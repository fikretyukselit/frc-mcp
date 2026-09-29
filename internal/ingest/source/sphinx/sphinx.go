// Package sphinx turns a Read the Docs "htmlzip" (a full Sphinx HTML build)
// into structure-aware chunks (docs/retrieval.md §3):
//
//   - one chunk per heading section, with the full heading path;
//   - sphinx-design code tab sets (data-sync-id java / c++ / python) split a
//     section into per-language chunks: shared prose + that language's code, so
//     language filtering is exact;
//   - code blocks are never split; oversized sections split on block
//     boundaries;
//   - HTML is converted to compact Markdown (admonitions, lists, tables, code).
package sphinx

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/chunking"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// skipDirs are sections of the site that do not help an agent write robot
// code (Java/C++/Python): docs-contribution guides, legal pages, generated
// indexes, and LabVIEW (a graphical language agents do not author).
var skipDirs = []string{"contributing/", "legal/", "labview/", "_", "genindex", "search", "404"}

// Stats summarizes a parse.
type Stats struct {
	Pages, Chunks, Skipped int
}

// Parse reads the htmlzip at zipPath and emits chunks.
func Parse(zipPath string, src sources.Source, rev string, retrieved time.Time, emit func(index.Chunk) error) (Stats, error) {
	var st Stats
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return st, err
	}
	defer zr.Close()
	files := make([]*zip.File, 0, len(zr.File))
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, src.ZipRoot)
		if !ok || !strings.HasSuffix(rel, ".html") || skip(rel) {
			continue
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name }) // deterministic build
	for _, f := range files {
		rel := strings.TrimPrefix(f.Name, src.ZipRoot)
		rc, err := f.Open()
		if err != nil {
			return st, err
		}
		doc, err := html.Parse(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return st, fmt.Errorf("sphinx: %s: %w", rel, err)
		}
		chunks := pageChunks(doc, rel, src, rev, retrieved)
		if len(chunks) == 0 {
			st.Skipped++
			continue
		}
		st.Pages++
		for _, c := range chunks {
			if err := emit(c); err != nil {
				return st, err
			}
			st.Chunks++
		}
	}
	return st, nil
}

// contentRoot finds the article body across Sphinx themes: sphinx_rtd_theme
// (div[itemprop=articleBody], 2026 docs) and Furo (article[role=main], 2027
// docs), with generic fallbacks. Upstream theme changes must not silently
// empty a shard: Parse reports pages without a root as skipped.
func contentRoot(doc *html.Node) *html.Node {
	for _, pred := range []func(*html.Node) bool{
		func(n *html.Node) bool { return attr(n, "itemprop") == "articleBody" },
		func(n *html.Node) bool { return n.DataAtom == atom.Article && attr(n, "role") == "main" },
		func(n *html.Node) bool { return attr(n, "role") == "main" },
		func(n *html.Node) bool { return n.DataAtom == atom.Main },
	} {
		if n := find(doc, pred); n != nil {
			return n
		}
	}
	return nil
}

func skip(rel string) bool {
	for _, d := range skipDirs {
		if strings.HasPrefix(rel, d) || strings.HasPrefix(path.Base(rel), d) {
			return true
		}
	}
	return rel == "index.html"
}

func toChunking(bs []block) []chunking.Block {
	out := make([]chunking.Block, len(bs))
	for i, b := range bs {
		out[i] = chunking.Block{Lang: b.lang, Text: b.text}
	}
	return out
}

// block is one Markdown block; lang is "" for content shared by all languages.
type block struct {
	lang string
	text string
}

type section struct {
	anchor  string
	heading string
	blocks  []block
}

func pageChunks(doc *html.Node, rel string, src sources.Source, rev string, retrieved time.Time) []index.Chunk {
	body := contentRoot(doc)
	if body == nil {
		return nil
	}
	var secs []struct {
		path []string
		s    section
	}
	var walk func(n *html.Node, path []string)
	walk = func(n *html.Node, path []string) {
		s := section{anchor: attr(n, "id")}
		var subs []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.DataAtom == atom.Section:
				subs = append(subs, c)
			case isHeading(c):
				if s.heading == "" {
					s.heading = headingText(c)
				}
			default:
				s.blocks = append(s.blocks, blocks(c)...)
			}
		}
		p := path
		if s.heading != "" {
			p = append(append([]string{}, path...), s.heading)
		}
		if len(s.blocks) > 0 {
			secs = append(secs, struct {
				path []string
				s    section
			}{p, s})
		}
		for _, sub := range subs {
			walk(sub, p)
		}
	}
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if c.DataAtom == atom.Section {
			walk(c, nil)
		}
	}
	if len(secs) == 0 {
		return nil
	}
	title := ""
	if len(secs[0].path) > 0 {
		title = secs[0].path[0]
	}
	docID := src.Library + "-docs/" + src.Season + "/" + strings.TrimSuffix(rel, ".html")
	kind := "prose"
	if strings.HasPrefix(rel, "yearly-overview/") || strings.Contains(rel, "yearly-changes/") || strings.Contains(rel, "migration/") {
		kind = "release"
	}
	var out []index.Chunk
	ord := 0
	for _, x := range secs {
		for _, v := range chunking.Variants(toChunking(x.s.blocks)) {
			for _, part := range chunking.Split(v.Text) {
				if textutil.EstimateTokens(part) < chunking.MinTokens {
					continue
				}
				out = append(out, index.Chunk{
					DocID: docID, Ord: ord, Library: src.Library, VersionLo: src.Version, Season: src.Season,
					Channel: src.Channel, Language: v.Lang, Kind: kind, Title: title,
					HeadingPath: strings.Join(x.path, " › "), Body: part, SourceURL: src.BaseURL + rel,
					Anchor: x.s.anchor, UpstreamRev: rev, RetrievedAt: retrieved, License: src.License,
					Trust: src.Trust, Authority: authority(src.Trust),
				})
				ord++
			}
		}
	}
	return out
}

// ---- HTML → Markdown ----

var syncLang = map[string]string{"java": "java", "c++": "cpp", "cpp": "cpp", "python": "python"}

func blocks(n *html.Node) []block {
	switch n.Type {
	case html.TextNode:
		if t := strings.TrimSpace(n.Data); t != "" {
			return []block{{text: t}}
		}
		return nil
	case html.ElementNode:
	default:
		return nil
	}
	cls := attr(n, "class")
	switch {
	case hasClass(cls, "sd-tab-set") || hasClass(cls, "tabbed-set"):
		return tabSet(n)
	case strings.Contains(cls, "highlight-"):
		return []block{{text: codeBlock(n, cls)}}
	case hasClass(cls, "admonition"):
		return []block{{text: admonition(n)}}
	case hasClass(cls, "toctree-wrapper") || hasClass(cls, "headerlink") || hasClass(cls, "sphinx-tabs"):
		return nil
	}
	switch n.DataAtom {
	case atom.P:
		if t := inline(n); t != "" {
			return []block{{text: t}}
		}
	case atom.Ul, atom.Ol:
		if t := list(n, 0); t != "" {
			return []block{{text: t}}
		}
	case atom.Pre:
		return []block{{text: "```\n" + strings.TrimRight(textOf(n), "\n") + "\n```"}}
	case atom.Table:
		return []block{{text: table(n)}}
	case atom.Dl:
		return []block{{text: defList(n)}}
	case atom.Blockquote:
		var parts []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			for _, b := range blocks(c) {
				parts = append(parts, b.text)
			}
		}
		return []block{{text: "> " + strings.ReplaceAll(strings.Join(parts, "\n\n"), "\n", "\n> ")}}
	case atom.Img, atom.Script, atom.Style, atom.Figure, atom.Input, atom.Label:
		if n.DataAtom == atom.Figure {
			if cap := find(n, func(x *html.Node) bool { return x.DataAtom == atom.Figcaption }); cap != nil {
				return []block{{text: "*Figure: " + inline(cap) + "*"}}
			}
		}
		return nil
	}
	var out []block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, blocks(c)...)
	}
	return out
}

// tabSet renders sphinx-design tabs. Code-language tab sets become
// language-tagged blocks; other tab sets (OS, IDE, …) are shared content.
func tabSet(n *html.Node) []block {
	var out []block
	var label, lang string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		cls := attr(c, "class")
		switch {
		case hasClass(cls, "sd-tab-label"):
			label = strings.TrimSpace(textOf(c))
			lang = syncLang[strings.ToLower(attr(c, "data-sync-id"))]
			if lang == "" {
				lang = syncLang[strings.ToLower(label)]
			}
		case hasClass(cls, "sd-tab-content"):
			var parts []string
			for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
				for _, b := range blocks(cc) {
					parts = append(parts, b.text)
				}
			}
			text := strings.Join(parts, "\n\n")
			if lang != "" {
				out = append(out, block{lang: lang, text: text})
			} else if text != "" {
				out = append(out, block{text: "**" + label + ":**\n\n" + text})
			}
			label, lang = "", ""
		}
	}
	return out
}

func codeBlock(n *html.Node, cls string) string {
	lang := ""
	for _, c := range strings.Fields(cls) {
		if l, ok := strings.CutPrefix(c, "highlight-"); ok {
			lang = l
		}
	}
	switch lang {
	case "default", "none", "text", "console", "bash", "shell", "powershell":
		if lang != "default" && lang != "none" {
			break
		}
		lang = ""
	case "c++":
		lang = "cpp"
	}
	pre := find(n, func(x *html.Node) bool { return x.DataAtom == atom.Pre })
	code := textOf(n)
	if pre != nil {
		code = textOf(pre)
	}
	return "```" + lang + "\n" + strings.TrimRight(code, "\n") + "\n```"
}

func admonition(n *html.Node) string {
	title := "Note"
	var parts []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasClass(attr(c, "class"), "admonition-title") {
			title = strings.TrimSpace(textOf(c))
			continue
		}
		for _, b := range blocks(c) {
			parts = append(parts, b.text)
		}
	}
	return "> **" + title + ":** " + strings.ReplaceAll(strings.Join(parts, "\n\n"), "\n", "\n> ")
}

func list(n *html.Node, depth int) string {
	var lines []string
	i := 1
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.DataAtom != atom.Li {
			continue
		}
		marker := "- "
		if n.DataAtom == atom.Ol {
			marker = fmt.Sprintf("%d. ", i)
			i++
		}
		var text []string
		var nested []string
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Ul || c.DataAtom == atom.Ol {
				nested = append(nested, list(c, depth+1))
				continue
			}
			if c.Type == html.ElementNode && (strings.Contains(attr(c, "class"), "highlight-") || c.DataAtom == atom.Pre) {
				for _, b := range blocks(c) {
					nested = append(nested, indentBlock(b.text, depth+1))
				}
				continue
			}
			if t := inlineNode(c); strings.TrimSpace(t) != "" {
				text = append(text, strings.TrimSpace(t))
			}
		}
		line := strings.Repeat("  ", depth) + marker + strings.Join(text, " ")
		lines = append(lines, line)
		lines = append(lines, nested...)
	}
	return strings.Join(lines, "\n")
}

func indentBlock(s string, depth int) string {
	pad := strings.Repeat("  ", depth)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func table(n *html.Node) string {
	var rows []string
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.DataAtom == atom.Tr {
			var cells []string
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
					cells = append(cells, strings.TrimSpace(inline(c)))
				}
			}
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(rows, "\n")
}

func defList(n *html.Node) string {
	var lines []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.DataAtom {
		case atom.Dt:
			lines = append(lines, "**"+strings.TrimSpace(inline(c))+"**")
		case atom.Dd:
			var parts []string
			for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
				for _, b := range blocks(cc) {
					parts = append(parts, b.text)
				}
			}
			lines = append(lines, ": "+strings.Join(parts, " "))
		}
	}
	return strings.Join(lines, "\n")
}

// inline renders an element's inline content as one Markdown line.
func inline(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(inlineNode(c))
	}
	return collapse(b.String())
}

func inlineNode(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return n.Data
	case html.ElementNode:
	default:
		return ""
	}
	if hasClass(attr(n, "class"), "headerlink") {
		return ""
	}
	switch n.DataAtom {
	case atom.Code, atom.Kbd, atom.Samp:
		return "`" + strings.TrimSpace(textOf(n)) + "`"
	case atom.Strong, atom.B:
		return "**" + strings.TrimSpace(inline(n)) + "**"
	case atom.Em, atom.I:
		if t := strings.TrimSpace(inline(n)); t != "" {
			return "*" + t + "*"
		}
		return ""
	case atom.Br:
		return "\n"
	case atom.Img, atom.Script, atom.Style:
		return ""
	}
	return inline(n)
}

func collapse(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	return strings.Join(strings.Fields(s), " ")
}

// ---- tiny DOM helpers ----

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

func headingText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasClass(attr(c, "class"), "headerlink") {
			continue
		}
		b.WriteString(textOf(c))
	}
	return collapse(b.String())
}

func isHeading(n *html.Node) bool {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return true
	}
	return false
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

// authority ranks first-party documentation above vendor documentation.
func authority(trust string) int {
	if trust == "official" {
		return 3
	}
	return 2
}
