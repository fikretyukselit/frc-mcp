// Package doxygen extracts C++ API symbol tables and per-class API chunks
// from a Doxygen HTML bundle read directly from the zip WPILib publishes to
// Maven (…/wpilibc/documentation/<ver>/documentation-<ver>.zip). No XML or
// tag file is published, so the class pages are parsed:
//
//   - the page title gives the FQN ("frc::TimedRobot Class Reference");
//   - "memberdecls" tables give every member's declaration and brief;
//   - "inherited from" headers give the base classes (for member lookups
//     through the hierarchy);
//   - deprecated.html gives deprecation text per member anchor.
//
// Only namespaces listed in src.Include (comma-separated, e.g. "frc::,wpi::")
// are kept: the bundle also documents vendored third-party code (fmt, Eigen,
// LLVM, Sleipnir) that robot code does not call through WPILib's API.
package doxygen

import (
	"archive/zip"
	"bytes"
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
	maxPage        = 8 << 20
	maxSummary     = 320
	maxChunkTokens = 700
)

// Stats summarizes a parse.
type Stats struct{ Types, Members, Chunks, Deprecated int }

var (
	pageRe     = regexp.MustCompile(`^(class|struct)[^/]+\.html$`)
	titleRe    = regexp.MustCompile(`^(.+?)\s+(Class|Struct|Union)( Template)? Reference`) // labels may follow unspaced ("Referencefinal")
	templateRe = regexp.MustCompile(`<[^<>]*(<[^<>]*>[^<>]*)*>`)
	skipSeg    = map[string]bool{"detail": true, "impl": true, "internal": true, "sig": true}
)

// Parse reads the Doxygen zip and emits symbols and chunks.
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
	files := map[string]*zip.File{}
	var pages []string
	for _, f := range zr.File {
		files[f.Name] = f
		if pageRe.MatchString(f.Name) && !strings.HasSuffix(f.Name, "-members.html") {
			pages = append(pages, f.Name)
		}
	}
	sort.Strings(pages)
	deprecated := map[string]string{}
	if f := files["deprecated.html"]; f != nil {
		if b, err := readAll(f); err == nil {
			deprecated = parseDeprecated(b)
		}
	}
	seen := map[string]bool{}
	for _, name := range pages {
		b, err := readAll(files[name])
		if err != nil {
			return st, err
		}
		doc, err := html.Parse(bytes.NewReader(b))
		if err != nil {
			return st, fmt.Errorf("doxygen: %s: %w", name, err)
		}
		c := parseClass(doc)
		if c == nil || !wanted(c.fqn, roots) || seen[c.fqn] {
			continue
		}
		seen[c.fqn] = true
		st.Types++
		url := src.BaseURL + name
		chunkID := src.Library + "-cpp/" + src.Season + "/" + c.fqn
		mk := func(fqn, kind, sig, summary, dep, anchor string) index.Symbol {
			s := index.Symbol{FQN: fqn, Library: src.Library, Version: src.Version, Season: src.Season, Language: "cpp",
				Kind: kind, Signature: sig, Summary: summary, SourceURL: url + anchor, UpstreamRev: rev,
				RetrievedAt: retrieved, License: src.License, Trust: src.Trust, ChunkID: chunkID + "#0"}
			if dep != "" {
				s.DeprecatedIn = src.Version
				s.Summary = strings.TrimSpace("Deprecated. " + dep + " " + s.Summary)
				st.Deprecated++
			}
			return s
		}
		if err := emitSymbol(mk(c.fqn, c.kind, c.signature(), c.summary, deprecated[name], "")); err != nil {
			return st, err
		}
		for i := range c.members {
			m := &c.members[i]
			m.dep = deprecated[name+"#"+m.anchor]
			if err := emitSymbol(mk(c.fqn+"#"+m.name, m.kind, m.sig, m.brief, m.dep, "#"+m.anchor)); err != nil {
				return st, err
			}
			st.Members++
		}
		for _, ch := range c.chunks(src, chunkID, url, rev, retrieved) {
			if err := emitChunk(ch); err != nil {
				return st, err
			}
			st.Chunks++
		}
	}
	return st, nil
}

func wanted(fqn string, roots []string) bool {
	for _, seg := range strings.Split(fqn, "::") {
		if skipSeg[seg] {
			return false
		}
	}
	if len(roots) == 0 {
		return true
	}
	for _, r := range roots {
		if strings.HasPrefix(fqn, r) {
			return true
		}
	}
	return false
}

type member struct {
	name, kind, sig, brief, anchor, dep string
}

type class struct {
	fqn, kind, summary string
	bases              []string
	members            []member
}

func (c *class) simple() string {
	if i := strings.LastIndex(c.fqn, "::"); i >= 0 {
		return c.fqn[i+2:]
	}
	return c.fqn
}

func (c *class) signature() string {
	s := c.kind + " " + c.fqn
	if len(c.bases) > 0 {
		s += " : public " + strings.Join(c.bases, ", public ")
	}
	return s
}

func parseClass(doc *html.Node) *class {
	title := find(doc, func(n *html.Node) bool { return hasClass(attr(n, "class"), "title") })
	if title == nil {
		return nil
	}
	m := titleRe.FindStringSubmatch(collapse(textOf(title)))
	if m == nil {
		return nil
	}
	fqn := strings.TrimSpace(templateRe.ReplaceAllString(m[1], ""))
	if fqn == "" || strings.ContainsAny(fqn, " <>") {
		return nil
	}
	c := &class{fqn: fqn, kind: strings.ToLower(m[2])}
	if tb := find(doc, func(n *html.Node) bool { return hasClass(attr(n, "class"), "textblock") }); tb != nil {
		if p := find(tb, func(n *html.Node) bool { return n.DataAtom == atom.P }); p != nil {
			c.summary = firstSentence(collapse(textOf(p)))
		}
	}
	seenBase := map[string]bool{}
	for _, tbl := range findAll(doc, func(n *html.Node) bool {
		return n.DataAtom == atom.Table && hasClass(attr(n, "class"), "memberdecls")
	}) {
		group := ""
		var last *member
		for tr := range rows(tbl) {
			cls := attr(tr, "class")
			switch {
			case cls == "heading":
				group = collapse(textOf(tr))
			case strings.HasPrefix(cls, "inherit_header"):
				if a := find(tr, func(n *html.Node) bool { return n.DataAtom == atom.A && hasClass(attr(n, "class"), "el") }); a != nil {
					if b := strings.TrimSpace(templateRe.ReplaceAllString(collapse(textOf(a)), "")); b != "" && !seenBase[b] {
						seenBase[b] = true
						c.bases = append(c.bases, b)
					}
				}
			case strings.Contains(cls, "inherit"):
				last = nil // inherited rows (and their descriptions) belong to the base class
			case strings.HasPrefix(cls, "memitem:"):
				last = nil
				kind := groupKind(group)
				if kind == "" {
					continue
				}
				left := cell(tr, "memItemLeft")
				right := cell(tr, "memItemRight")
				if right == nil {
					continue
				}
				name := ""
				if a := find(right, func(n *html.Node) bool { return n.DataAtom == atom.A && hasClass(attr(n, "class"), "el") }); a != nil {
					name = collapse(textOf(a))
				}
				if name == "" || strings.HasPrefix(name, "~") || strings.HasPrefix(name, "operator") {
					continue
				}
				if kind == "method" && name == c.simple() {
					kind = "constructor"
				}
				sig := collapse(strings.TrimSpace(textOrEmpty(left) + " " + textOf(right)))
				anchor := strings.TrimPrefix(strings.TrimPrefix(cls, "memitem:"), "r_")
				c.members = append(c.members, member{name: name, kind: kind, sig: sig, anchor: anchor})
				last = &c.members[len(c.members)-1]
			case strings.HasPrefix(cls, "memdesc:") && last != nil:
				if d := cell(tr, "mdescRight"); d != nil {
					last.brief = firstSentence(collapse(textOf(d)))
				}
			}
		}
	}
	return c
}

// groupKind maps a Doxygen member group heading to a symbol kind; private
// and friend members are not API.
func groupKind(h string) string {
	l := strings.ToLower(h)
	switch {
	case strings.Contains(l, "private"), strings.Contains(l, "friend"), strings.Contains(l, "related"):
		return ""
	case strings.Contains(l, "function"):
		return "method"
	case strings.Contains(l, "attribute"), strings.Contains(l, "data field"), strings.Contains(l, "variable"):
		return "field"
	case strings.Contains(l, "enum"), strings.Contains(l, "typedef"), strings.Contains(l, "type"):
		return "type"
	}
	return ""
}

// parseDeprecated maps "page.html#anchor" (or "page.html" for a class) to
// the deprecation text listed on deprecated.html.
func parseDeprecated(b []byte) map[string]string {
	out := map[string]string{}
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return out
	}
	for _, dt := range findAll(doc, func(n *html.Node) bool { return n.DataAtom == atom.Dt }) {
		// The first link is the deprecated entity ("Member <a>ns::T::m</a>
		// (const <a>Rotation2d</a> &r)"); later links are parameter types
		// and must not mark those classes deprecated.
		var href string
		if links := findAll(dt, func(n *html.Node) bool { return n.DataAtom == atom.A && hasClass(attr(n, "class"), "el") }); len(links) > 0 {
			href = attr(links[0], "href")
		}
		if href == "" {
			continue
		}
		dd := dt.NextSibling
		for dd != nil && dd.Type != html.ElementNode {
			dd = dd.NextSibling
		}
		text := ""
		if dd != nil && dd.DataAtom == atom.Dd {
			text = collapse(textOf(dd))
		}
		if text == "" {
			text = "deprecated"
		}
		out[href] = text
	}
	return out
}

func (c *class) chunks(src sources.Source, docID, url, rev string, retrieved time.Time) []index.Chunk {
	var head strings.Builder
	fmt.Fprintf(&head, "```cpp\n%s\n```\n", c.signature())
	if c.summary != "" {
		fmt.Fprintf(&head, "\n%s\n", c.summary)
	}
	mk := func(ord int, body string) index.Chunk {
		ns := ""
		if i := strings.LastIndex(c.fqn, "::"); i >= 0 {
			ns = c.fqn[:i]
		}
		return index.Chunk{DocID: docID, Ord: ord, Library: src.Library, VersionLo: src.Version, Season: src.Season,
			Channel: src.Channel, Language: "cpp", Kind: "api", Title: c.simple() + " (" + c.kind + ")",
			HeadingPath: "C++ API › " + ns, Symbol: c.fqn, Body: body, SourceURL: url, UpstreamRev: rev,
			RetrievedAt: retrieved, License: src.License, Trust: src.Trust, Authority: 3}
	}
	cur := head.String()
	if len(c.members) > 0 {
		cur += "\nMembers:\n"
	}
	var out []index.Chunk
	for _, m := range c.members {
		l := "- `" + m.sig + "`"
		if m.dep != "" {
			l += " **(deprecated)**"
		}
		if m.brief != "" {
			l += " — " + m.brief
		}
		if textutil.EstimateTokens(cur+l) > maxChunkTokens && strings.Contains(cur, "\n- ") {
			out = append(out, mk(len(out), strings.TrimSpace(cur)))
			cur = "```cpp\n" + c.signature() + "\n```\n\nMembers (continued):\n"
		}
		cur += l + "\n"
	}
	return append(out, mk(len(out), strings.TrimSpace(cur)))
}

// ---- HTML helpers ----

func rows(tbl *html.Node) func(func(*html.Node) bool) {
	return func(yield func(*html.Node) bool) {
		var walk func(n *html.Node) bool
		walk = func(n *html.Node) bool {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.DataAtom == atom.Tr {
					if !yield(c) {
						return false
					}
					continue
				}
				if c.DataAtom == atom.Table { // nested tables are not member rows
					continue
				}
				if !walk(c) {
					return false
				}
			}
			return true
		}
		walk(tbl)
	}
}

func cell(tr *html.Node, cls string) *html.Node {
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.DataAtom == atom.Td && hasClass(attr(c, "class"), cls) {
			return c
		}
	}
	return nil
}

func textOrEmpty(n *html.Node) string {
	if n == nil {
		return ""
	}
	return textOf(n)
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

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxPage))
}

func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := find(c, pred); r != nil {
			return r
		}
	}
	return nil
}

func findAll(n *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if pred(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
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
