// Package javadoc extracts API symbol tables and per-class API chunks from a
// Javadoc HTML bundle (JDK 17+ doclet), read directly from the zip published
// to Maven, so no site is crawled.
//
// For every type listed in type-search-index.js it parses the class page:
// the type signature, description, deprecation, and each member detail section
// (signature, first-sentence summary, deprecation). Symbols are exact facts
// for frc_api; the class chunk makes the API searchable.
package javadoc

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

const (
	maxSummary      = 320 // characters of member summary kept in symbols/chunks
	maxChunkTokens  = 700
	maxClassSummary = 900
)

// Stats summarizes a parse.
type Stats struct {
	Types, Members, Chunks, Deprecated int
}

type typeEntry struct {
	P string `json:"p"` // package
	L string `json:"l"` // (nested) type name, e.g. "Outer.Inner"
}

// Parse reads the Javadoc zip and emits symbols and chunks.
func Parse(zipPath string, src sources.Source, rev string, retrieved time.Time,
	emitSymbol func(index.Symbol) error, emitChunk func(index.Chunk) error) (Stats, error) {
	var st Stats
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return st, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	types, err := readIndex(files["type-search-index.js"], "typeSearchIndex")
	if err != nil {
		return st, fmt.Errorf("javadoc: %w", err)
	}
	sort.Slice(types, func(i, j int) bool {
		if types[i].P != types[j].P {
			return types[i].P < types[j].P
		}
		return types[i].L < types[j].L
	})
	for _, t := range types {
		if t.P == "" || t.P == "<Unnamed>" {
			continue
		}
		rel := strings.ReplaceAll(t.P, ".", "/") + "/" + t.L + ".html"
		f := files[rel]
		if f == nil {
			continue
		}
		b, err := readAll(f)
		if err != nil {
			return st, err
		}
		doc, err := html.Parse(bytes.NewReader(b))
		if err != nil {
			return st, fmt.Errorf("javadoc: %s: %w", rel, err)
		}
		ty := parseType(doc, t)
		if ty == nil {
			continue
		}
		st.Types++
		url := src.BaseURL + rel
		mk := func(fqn, kind, sig, summary string, dep *deprecation, anchor string) index.Symbol {
			s := index.Symbol{FQN: fqn, Library: src.Library, Version: src.Version, Season: src.Season,
				Language: "java", Kind: kind, Signature: sig, Summary: summary, SourceURL: url + anchor,
				UpstreamRev: rev, RetrievedAt: retrieved, License: src.License, Trust: src.Trust,
				ChunkID: chunkDocID(src, t) + "#0"}
			if dep != nil {
				s.DeprecatedIn = src.Version
				if m := sinceRe.FindStringSubmatch(sig); m != nil {
					s.DeprecatedIn = m[1] // @Deprecated(since="2025") is the true origin
				}
				s.Replacement = dep.replacement
				if dep.forRemoval {
					s.Summary = strings.TrimSpace("Deprecated for removal. " + dep.text + " " + s.Summary)
				} else if dep.text != "" {
					s.Summary = strings.TrimSpace("Deprecated. " + dep.text + " " + s.Summary)
				}
				st.Deprecated++
			}
			return s
		}
		if err := emitSymbol(mk(ty.fqn, ty.kind, ty.signature, firstSentence(ty.description), ty.dep, "")); err != nil {
			return st, err
		}
		for _, m := range ty.members {
			if err := emitSymbol(mk(ty.fqn+"#"+m.name, m.kind, m.signature, m.summary, m.dep, "#"+escapeAnchor(m.anchor))); err != nil {
				return st, err
			}
			st.Members++
		}
		for _, c := range typeChunks(ty, t, src, url, rev, retrieved) {
			if err := emitChunk(c); err != nil {
				return st, err
			}
			st.Chunks++
		}
	}
	return st, nil
}

func chunkDocID(src sources.Source, t typeEntry) string {
	return src.Library + "-" + src.Language + "/" + src.Season + "/" + t.P + "." + t.L
}

type deprecation struct {
	text        string
	replacement string
	forRemoval  bool
}

type member struct {
	name, kind, signature, summary, anchor string
	dep                                    *deprecation
}

type typeInfo struct {
	fqn, simple, kind, signature, description string
	dep                                       *deprecation
	members                                   []member
}

func parseType(doc *html.Node, t typeEntry) *typeInfo {
	sig := find(doc, byClass("type-signature"))
	if sig == nil {
		return nil
	}
	ty := &typeInfo{fqn: t.P + "." + t.L, simple: t.L, signature: collapse(textOf(sig))}
	mods := ""
	if m := find(sig, byClass("modifiers")); m != nil {
		mods = textOf(m)
	}
	switch {
	case strings.Contains(mods, "@interface"):
		ty.kind = "annotation"
	case strings.Contains(mods, "interface"):
		ty.kind = "interface"
	case strings.Contains(mods, "enum"):
		ty.kind = "enum"
	case strings.Contains(mods, "record"):
		ty.kind = "record"
	default:
		ty.kind = "class"
	}
	if desc := find(doc, func(n *html.Node) bool { return attr(n, "id") == "class-description" }); desc != nil {
		ty.dep = parseDeprecation(desc)
		if blk := directBlock(desc); blk != nil {
			ty.description = collapse(textOf(blk))
		}
	}
	// Member details: every section.detail, classified by the enclosing
	// "*-detail" section (constructor-detail, method-detail, field-detail, …).
	var walk func(n *html.Node, group string)
	walk = func(n *html.Node, group string) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Section {
			id := attr(n, "id")
			if strings.HasSuffix(id, "-detail") && !hasClass(attr(n, "class"), "detail") {
				group = strings.TrimSuffix(id, "-detail")
			} else if hasClass(attr(n, "class"), "detail") && group != "" {
				if m := parseMember(n, group); m != nil {
					ty.members = append(ty.members, *m)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, group)
		}
	}
	walk(doc, "")
	return ty
}

func parseMember(n *html.Node, group string) *member {
	h := find(n, func(x *html.Node) bool { return x.DataAtom == atom.H3 })
	sig := find(n, byClass("member-signature"))
	if h == nil || sig == nil {
		return nil
	}
	m := &member{name: collapse(textOf(h)), signature: collapse(textOf(sig)), anchor: attr(n, "id")}
	switch group {
	case "constructor":
		m.kind = "constructor"
	case "field", "enum-constant":
		m.kind = "field"
	case "annotation-interface-element", "annotation-type-element":
		m.kind = "element"
	default:
		m.kind = "method"
	}
	m.dep = parseDeprecation(n)
	if blk := directBlock(n); blk != nil {
		m.summary = firstSentence(collapse(textOf(blk)))
	}
	return m
}

func parseDeprecation(n *html.Node) *deprecation {
	blk := findShallow(n, byClass("deprecation-block"))
	if blk == nil {
		return nil
	}
	d := &deprecation{}
	if lbl := find(blk, byClass("deprecated-label")); lbl != nil {
		d.forRemoval = strings.Contains(strings.ToLower(textOf(lbl)), "removal")
	}
	if c := find(blk, byClass("deprecation-comment")); c != nil {
		d.text = firstSentence(collapse(textOf(c)))
		if code := find(c, func(x *html.Node) bool { return x.DataAtom == atom.Code }); code != nil {
			d.replacement = collapse(textOf(code))
		}
	}
	return d
}

// directBlock returns the first div.block that is a direct child of n (not a
// nested member's description).
func directBlock(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Div && hasClass(attr(c, "class"), "block") {
			return c
		}
	}
	return nil
}

// typeChunks renders one or more API chunks for a type: signature,
// description and the member list (signature + summary), split by size.
func typeChunks(ty *typeInfo, t typeEntry, src sources.Source, url, rev string, retrieved time.Time) []index.Chunk {
	var head strings.Builder
	fmt.Fprintf(&head, "```java\n%s\n```\n", ty.signature)
	if ty.dep != nil {
		fmt.Fprintf(&head, "\n> **Deprecated** in %s.", src.Version)
		if ty.dep.text != "" {
			fmt.Fprintf(&head, " %s", ty.dep.text)
		}
		head.WriteString("\n")
	}
	if ty.description != "" {
		d := ty.description
		if len(d) > maxClassSummary {
			d = firstSentences(d, maxClassSummary)
		}
		fmt.Fprintf(&head, "\n%s\n", d)
	}
	lines := make([]string, 0, len(ty.members))
	for _, m := range ty.members {
		l := "- `" + m.signature + "`"
		if m.dep != nil {
			l += " **(deprecated)**"
		}
		if m.summary != "" {
			l += " — " + m.summary
		}
		lines = append(lines, l)
	}
	mkChunk := func(ord int, body string) index.Chunk {
		return index.Chunk{DocID: chunkDocID(src, t), Ord: ord, Library: src.Library, VersionLo: src.Version,
			Season: src.Season, Channel: src.Channel, Language: "java", Kind: "api",
			Title: ty.simple + " (" + ty.kind + ")", HeadingPath: "Java API › " + t.P, Symbol: ty.fqn,
			Body: body, SourceURL: url, UpstreamRev: rev, RetrievedAt: retrieved, License: src.License,
			Trust: src.Trust, Authority: 3}
	}
	var out []index.Chunk
	cur := head.String()
	if len(lines) > 0 {
		cur += "\nMembers:\n"
	}
	for _, l := range lines {
		if textutil.EstimateTokens(cur+l) > maxChunkTokens && strings.Contains(cur, "\n- ") {
			out = append(out, mkChunk(len(out), strings.TrimSpace(cur)))
			cur = "```java\n" + ty.signature + "\n```\n\nMembers (continued):\n"
		}
		cur += l + "\n"
	}
	return append(out, mkChunk(len(out), strings.TrimSpace(cur)))
}

var sinceRe = regexp.MustCompile(`@Deprecated\([^)]*since\s*=\s*"([^"]+)"`)

func firstSentence(s string) string {
	// Javadoc inherits docs with a "Description copied from …: X" preamble.
	if rest, ok := strings.CutPrefix(s, "Description copied from "); ok {
		if i := strings.Index(rest, ": "); i >= 0 {
			rest = rest[i+2:]
			if j := strings.IndexByte(rest, ' '); j >= 0 {
				s = strings.TrimSpace(rest[j:]) // drop the "Interface" name token
			} else {
				s = ""
			}
		}
	}
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

func firstSentences(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := strings.LastIndex(s[:limit], ". ")
	if cut < 0 {
		return firstSentence(s)
	}
	return s[:cut+1]
}

// escapeAnchor keeps Javadoc anchors URL-safe (they contain parentheses,
// commas and angle brackets).
func escapeAnchor(a string) string {
	return strings.NewReplacer(" ", "%20", "<", "%3C", ">", "%3E").Replace(a)
}

func readIndex(f *zip.File, varName string) ([]typeEntry, error) {
	if f == nil {
		return nil, fmt.Errorf("%s not found", varName)
	}
	b, err := readAll(f)
	if err != nil {
		return nil, err
	}
	i := bytes.IndexByte(b, '[')
	j := bytes.LastIndexByte(b, ']')
	if i < 0 || j < i {
		return nil, fmt.Errorf("%s: unexpected format", varName)
	}
	var out []typeEntry
	if err := json.Unmarshal(b[i:j+1], &out); err != nil {
		return nil, fmt.Errorf("%s: %w", varName, err)
	}
	return out, nil
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 16<<20))
}

// ---- DOM helpers ----

func byClass(c string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(attr(n, "class"), c) }
}

func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, pred); f != nil {
			return f
		}
	}
	return nil
}

// findShallow searches n's subtree but does not descend into nested member
// detail sections (so a class-level search doesn't pick up a member's block).
func findShallow(n *html.Node, pred func(*html.Node) bool) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if pred(c) {
			return c
		}
		if c.DataAtom == atom.Section && hasClass(attr(c, "class"), "detail") {
			continue
		}
		if f := findShallow(c, pred); f != nil {
			return f
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapse(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(cls, c string) bool {
	for _, x := range strings.Fields(cls) {
		if x == c {
			return true
		}
	}
	return false
}
