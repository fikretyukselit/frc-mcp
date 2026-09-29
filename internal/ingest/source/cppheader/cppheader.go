// Package cppheader extracts C++ API symbol tables and per-type API chunks
// from the public headers a vendor ships on its Maven repository
// (<artifact>-<version>-headers.zip, the "headerClassifier" artifact every
// C++ vendordep lists). CTRE, REV and PhotonVision publish their C++ API
// references only as crawled Doxygen sites that track one release, while the
// headers zip is one small, version-pinned download per release, so the
// headers are the source of truth and the Doxygen sites are used for links.
//
// The scanner (parse.go) records namespaces, classes/structs/unions with
// their bases, enums and enumerators, aliases, and public/protected members;
// the brief of each declaration comes from its Doxygen comment
// (\brief / first sentence) and deprecations from [[deprecated("…")]] or a
// \deprecated paragraph. FQNs follow the doxygen-zip form: types
// "ns::Type", members "ns::Type#Member", nested types "ns::Outer::Inner".
//
// Only namespaces listed in src.Include (comma-separated roots such as
// "ctre::") are kept; detail/impl/internal namespaces and names starting
// with '_' are dropped. src.Skip lists header path prefixes inside the zip
// to leave out (headers the vendor excludes from its own API reference).
package cppheader

import (
	"archive/zip"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

const (
	maxHeader      = 4 << 20
	maxSummary     = 320
	maxChunkTokens = 700
)

// Stats summarizes a parse.
type Stats struct{ Files, Namespaces, Types, Members, Functions, Chunks, Deprecated int }

var skipSeg = map[string]bool{"detail": true, "impl": true, "internal": true, "sig": true}

// Parse reads the headers zip and emits symbols and chunks.
func Parse(zipPath string, src sources.Source, rev string, retrieved time.Time,
	emitSymbol func(index.Symbol) error, emitChunk func(index.Chunk) error) (Stats, error) {
	var st Stats
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return st, err
	}
	defer zr.Close()
	var roots []string
	for _, r := range strings.Split(src.Include, ",") {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, r)
		}
	}
	want := func(p []string) (emit, descend bool) {
		for _, s := range p {
			if skipSeg[s] || strings.HasPrefix(s, "_") {
				return false, false
			}
		}
		if len(roots) == 0 {
			return true, true
		}
		q := strings.Join(p, "::") + "::"
		for _, r := range roots {
			if strings.HasPrefix(q, r) {
				return true, true
			}
			if strings.HasPrefix(r, q) || len(p) == 0 {
				descend = true
			}
		}
		return false, descend
	}
	var names []string
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		n := f.Name
		if f.FileInfo().IsDir() || strings.HasSuffix(n, ".pb.h") {
			continue // protobuf-generated headers are not the library's API
		}
		if slices.ContainsFunc(src.Skip, func(p string) bool { return strings.HasPrefix(n, p) }) {
			continue // headers the vendor leaves out of its documented API
		}
		if ext := path.Ext(n); ext == ".h" || ext == ".hpp" || ext == ".hh" || ext == ".hxx" {
			names = append(names, n)
			files[n] = f
		}
	}
	sort.Strings(names)
	var types []*typeDecl
	var funcs []nsDecl
	nsSeen := map[string]bool{}
	for _, n := range names {
		b, err := readAll(files[n])
		if err != nil {
			return st, fmt.Errorf("cppheader: %s: %w", n, err)
		}
		p := &parser{t: lex(string(b)), file: n, want: want}
		p.parseScope(scope{public: true})
		types = append(types, p.types...)
		funcs = append(funcs, p.funcs...)
		for _, ns := range p.namespaces {
			nsSeen[ns] = true
		}
		st.Files++
	}

	// One definition per type (first header wins); forward declarations were
	// never recorded.
	byFQN := map[string]*typeDecl{}
	var ordered []*typeDecl
	for _, t := range types {
		if byFQN[t.fqn] != nil {
			continue
		}
		byFQN[t.fqn] = t
		ordered = append(ordered, t)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].fqn < ordered[j].fqn })

	seen := map[string]bool{} // fqn + "\x00" + signature: the symbol primary key
	emit := func(s index.Symbol) error {
		k := s.FQN + "\x00" + s.Signature
		if seen[k] {
			return nil
		}
		seen[k] = true
		if s.DeprecatedIn != "" {
			st.Deprecated++
		}
		return emitSymbol(s)
	}
	mk := func(fqn, kind, sig, doc, dep, url, chunkID string) index.Symbol {
		summary, docDep := docText(doc)
		if dep == "" || dep == "deprecated" && docDep != "" {
			dep = docDep // the \deprecated text is more useful than a bare [[deprecated]]
		}
		s := index.Symbol{FQN: fqn, Library: src.Library, Version: src.Version, Season: src.Season, Language: "cpp",
			Kind: kind, Signature: sig, Summary: summary, SourceURL: url, UpstreamRev: rev, RetrievedAt: retrieved,
			License: src.License, Trust: src.Trust, ChunkID: chunkID}
		if dep != "" {
			s.DeprecatedIn = src.Version
			parts := []string{"Deprecated."}
			if dep != "deprecated" {
				parts = append(parts, dep)
			}
			if summary != "" {
				parts = append(parts, summary)
			}
			s.Summary = strings.Join(parts, " ")
		}
		return s
	}
	for _, t := range ordered {
		url := src.BaseURL + pageFor(t, byFQN)
		chunkID := ""
		if t.kind != "type" {
			chunkID = src.Library + "-cpp/" + src.Season + "/" + t.fqn + "#0"
		}
		sig := t.sig
		if t.kind == "class" || t.kind == "struct" || t.kind == "union" {
			t.bases = resolveBases(t, byFQN)
			sig = t.kind + " " + t.fqn
			if len(t.bases) > 0 {
				sig += " : public " + strings.Join(t.bases, ", public ")
			}
		}
		if err := emit(mk(t.fqn, t.kind, sig, t.doc, t.dep, url, chunkID)); err != nil {
			return st, err
		}
		st.Types++
		for _, m := range t.members {
			if err := emit(mk(t.fqn+"#"+m.name, m.kind, m.sig, m.doc, m.dep, url, chunkID)); err != nil {
				return st, err
			}
			st.Members++
		}
		if chunkID == "" {
			continue
		}
		for _, ch := range chunks(t, sig, src, url, rev, retrieved) {
			if err := emitChunk(ch); err != nil {
				return st, err
			}
			st.Chunks++
		}
	}
	sort.SliceStable(funcs, func(i, j int) bool { return funcs[i].fqn < funcs[j].fqn })
	for _, f := range funcs {
		ns := f.fqn[:strings.LastIndex(f.fqn, "::")]
		if err := emit(mk(f.fqn, f.kind, f.sig, f.doc, f.dep, src.BaseURL+"namespace"+doxyEscape(ns)+".html", "")); err != nil {
			return st, err
		}
		st.Functions++
	}
	// Namespaces are symbols too: some are named like types
	// (photon::PhotonTargetSortMode::Largest), and frc_api can list them.
	nss := slices.Sorted(maps.Keys(nsSeen))
	for _, ns := range nss {
		if byFQN[ns] != nil {
			continue
		}
		if err := emit(mk(ns, "namespace", "namespace "+ns, "", "", src.BaseURL+"namespace"+doxyEscape(ns)+".html", "")); err != nil {
			return st, err
		}
		st.Namespaces++
	}
	return st, nil
}

// resolveBases maps each base as written to the FQN of a type in this
// table, using C++ unqualified lookup order (innermost enclosing scope
// first). Bases from other libraries (wpi::Sendable) or other artifacts stay
// as written; the verifier resolves those by name.
func resolveBases(t *typeDecl, byFQN map[string]*typeDecl) []string {
	out := make([]string, 0, len(t.bases))
	self := append(append([]string(nil), t.scope...), lastSeg(t.fqn))
	for _, b := range t.bases {
		r := b
		for k := len(self); k >= 0; k-- {
			if c := join(self[:k], b); byFQN[c] != nil && c != t.fqn {
				r = c
				break
			}
		}
		out = append(out, r)
	}
	return out
}

// pageFor returns the Doxygen page that documents a type: its own
// class/struct/union page, the enclosing class page for a nested enum, or the
// namespace page for a namespace-level enum or alias. Doxygen derives page
// names from the FQN (doxyEscape), so links need no crawl.
func pageFor(t *typeDecl, byFQN map[string]*typeDecl) string {
	switch t.kind {
	case "class", "struct", "union":
		return t.kind + doxyEscape(t.fqn) + ".html"
	}
	if t.pageFQN != "" {
		if o := byFQN[t.pageFQN]; o != nil {
			return pageFor(o, byFQN)
		}
	}
	return "namespace" + doxyEscape(strings.Join(t.scope, "::")) + ".html"
}

// doxyEscape reproduces Doxygen's file-name escaping with
// CASE_SENSE_NAMES=NO (the setting of the CTRE, REV and PhotonVision
// sites): "::" → "_1_1", '_' → "__", an upper-case letter → '_' + lower.
func doxyEscape(fqn string) string {
	var b strings.Builder
	for i := 0; i < len(fqn); i++ {
		c := fqn[i]
		switch {
		case c == ':' && i+1 < len(fqn) && fqn[i+1] == ':':
			b.WriteString("_1_1")
			i++
		case c == '_':
			b.WriteString("__")
		case c >= 'A' && c <= 'Z':
			b.WriteByte('_')
			b.WriteByte(c + 'a' - 'A')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

var (
	cmdLineRe = regexp.MustCompile(`^[\\@](\w+)`)
	inlineRe  = regexp.MustCompile(`[\\@](c|p|a|b|e|em|ref|link|endlink)\b\s*`)
	tagRe     = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
)

// docText returns the one-sentence summary of a Doxygen comment (the \brief
// paragraph, else the first paragraph) and its \deprecated text, if any.
func docText(doc string) (summary, deprecated string) {
	if doc == "" {
		return "", ""
	}
	var brief, dep []string
	isDep := false
	sec := "main" // main | brief | dep | skip | done
	for _, l := range strings.Split(doc, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			switch {
			case (sec == "main" || sec == "brief") && len(brief) > 0:
				sec = "done"
			case sec == "dep" && len(brief) == 0:
				sec = "main"
			case sec == "dep":
				sec = "done"
			}
			continue
		}
		if m := cmdLineRe.FindStringSubmatch(l); m != nil {
			switch m[1] {
			case "brief", "short":
				sec = "brief"
				if len(brief) > 0 {
					sec = "skip"
				}
			case "deprecated":
				sec, isDep = "dep", true
			default: // \param, \returns, \details, \note, …
				sec = "skip"
			}
			if l = strings.TrimSpace(l[len(m[0]):]); l == "" {
				continue
			}
		}
		switch sec {
		case "main", "brief":
			brief = append(brief, l)
		case "dep":
			dep = append(dep, l)
		}
	}
	clean := func(ss []string) string {
		s := strings.Join(ss, " ")
		s = inlineRe.ReplaceAllString(s, "")
		s = tagRe.ReplaceAllString(s, "")
		return collapse(s)
	}
	summary = firstSentence(clean(brief))
	if isDep {
		if deprecated = clean(dep); deprecated == "" {
			deprecated = "deprecated"
		}
	}
	return summary, deprecated
}

func chunks(t *typeDecl, sig string, src sources.Source, url, rev string, retrieved time.Time) []index.Chunk {
	summary, dep := docText(t.doc)
	if t.dep != "" && dep == "" {
		dep = t.dep
	}
	var head strings.Builder
	fmt.Fprintf(&head, "```cpp\n%s\n```\n", sig)
	if dep != "" {
		head.WriteString("\n**Deprecated.**")
		if dep != "deprecated" {
			head.WriteString(" " + dep)
		}
		head.WriteString("\n")
	}
	if summary != "" {
		fmt.Fprintf(&head, "\n%s\n", summary)
	}
	ns := strings.Join(t.scope, "::")
	docID := src.Library + "-cpp/" + src.Season + "/" + t.fqn
	title := lastSeg(t.fqn) + " (" + t.kind + ")"
	mk := func(ord int, body string) index.Chunk {
		return index.Chunk{DocID: docID, Ord: ord, Library: src.Library, VersionLo: src.Version, Season: src.Season,
			Channel: src.Channel, Language: "cpp", Kind: "api", Title: title, HeadingPath: "C++ API › " + ns,
			Symbol: t.fqn, Body: body, SourceURL: url, UpstreamRev: rev, RetrievedAt: retrieved, License: src.License,
			Trust: src.Trust, Authority: 3}
	}
	cur := head.String()
	label := "Members"
	if t.kind == "enum" {
		label = "Values"
	}
	if len(t.members) > 0 {
		cur += "\n" + label + ":\n"
	}
	var out []index.Chunk
	for _, m := range t.members {
		msum, mdep := docText(m.doc)
		if m.dep != "" {
			mdep = m.dep
		}
		l := "- `" + m.sig + "`"
		if t.kind == "enum" {
			l = "- `" + m.name + "`"
		}
		if mdep != "" {
			l += " **(deprecated)**"
		}
		if msum != "" {
			l += " — " + msum
		}
		if textutil.EstimateTokens(cur+l) > maxChunkTokens && strings.Contains(cur, "\n- ") {
			out = append(out, mk(len(out), strings.TrimSpace(cur)))
			cur = "```cpp\n" + sig + "\n```\n\n" + label + " (continued):\n"
		}
		cur += l + "\n"
	}
	return append(out, mk(len(out), strings.TrimSpace(cur)))
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 && i < maxSummary {
		return s[:i+1]
	}
	if len(s) > maxSummary {
		cut := maxSummary
		for cut > 0 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		return strings.TrimSpace(s[:cut]) + " …"
	}
	return s
}

func collapse(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxHeader))
}
