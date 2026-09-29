// Package markdown turns vendor documentation written in Markdown dialects
// into structure-aware chunks, with the same rules as the Sphinx adapter
// (docs/retrieval.md §3): one chunk per heading section with its full heading
// path, per-language variants for code tabs, fences never split.
//
// Vendor sites use five dialects. Normalize rewrites each into plain
// CommonMark before sectioning:
//
//   - MyST (PhotonVision): :::{note} fences, ```{directive} blocks, {role}`x`
//     roles and ```{eval-rst} islands with tab-set-code / code-block;
//   - Docusaurus (AdvantageKit): front matter, MDX import/export lines,
//     <Tabs>/<TabItem>, :::tip admonitions, {#id} heading anchors;
//   - Writerside (PathPlanner): <tabs>/<tab title=…>, {style="note"} lines;
//   - MkDocs Material (Choreo): === "Java" tabs and !!! note admonitions whose
//     bodies are indented four spaces;
//   - GitBook (REV, YAGSL): {% hint %}, {% tabs %}/{% tab %}, {% embed %},
//     {% content-ref %}, <figure>, <details>.
package markdown

import (
	"regexp"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/chunking"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// Page is one Markdown document to chunk.
type Page struct {
	Rel   string // path used in the doc id, without extension (e.g. "docs/apriltag-pipelines/multitag")
	URL   string // canonical page URL (no anchor)
	Title string // fallback title (front matter, link text) when the page has no H1
	Text  string // raw Markdown
}

// Chunks normalizes and sections p into chunks with provenance from src.
func Chunks(p Page, src sources.Source, rev string, retrieved time.Time) []index.Chunk {
	fm, body := FrontMatter(p.Text)
	if t := fm["title"]; t != "" && p.Title == "" {
		p.Title = t
	}
	secs := sections(Normalize(body))
	if len(secs) == 0 {
		return nil
	}
	// Title: the H1 when the page has one, else front matter / link text, else
	// the file name (a leading H2 is a section, not the page title).
	title := p.Title
	for _, s := range secs {
		if len(s.path) > 0 {
			if s.level == 1 {
				title = s.path[0]
			}
			break
		}
	}
	if title == "" {
		title = humanize(p.Rel)
	}
	kind := "prose"
	if isRelease(p.Rel) {
		kind = "release"
	}
	docID := src.Library + "-docs/" + src.Season + "/" + strings.Trim(p.Rel, "/")
	var out []index.Chunk
	ord := 0
	for _, s := range secs {
		path := s.path
		if len(path) == 0 && title != "" {
			path = []string{title}
		}
		for _, v := range chunking.Variants(s.blocks) {
			for _, part := range chunking.Split(v.Text) {
				if textutil.EstimateTokens(part) < chunking.MinTokens {
					continue
				}
				out = append(out, index.Chunk{
					DocID: docID, Ord: ord, Library: src.Library, VersionLo: src.Version, Season: src.Season,
					Channel: src.Channel, Language: v.Lang, Kind: kind, Title: title,
					HeadingPath: strings.Join(path, " › "), Body: part, SourceURL: p.URL,
					Anchor: s.anchor, UpstreamRev: rev, RetrievedAt: retrieved, License: src.License,
					Trust: src.Trust, Authority: 2,
				})
				ord++
			}
		}
	}
	return out
}

// humanize turns "usage/controls-shortcuts" into "Controls shortcuts".
func humanize(rel string) string {
	b := rel[strings.LastIndexByte(rel, '/')+1:]
	b = strings.NewReplacer("-", " ", "_", " ").Replace(b)
	if b == "" {
		return rel
	}
	return strings.ToUpper(b[:1]) + b[1:]
}

func isRelease(rel string) bool {
	l := strings.ToLower(rel)
	for _, k := range []string{"changelog", "release-notes", "yearly-changes", "whats-new", "what-s-new", "migration"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

// FrontMatter splits a leading YAML front matter block (flat key: value only).
func FrontMatter(s string) (map[string]string, string) {
	s = strings.TrimPrefix(s, "\ufeff")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return nil, s
	}
	rest := s[strings.IndexByte(s, '\n')+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, s
	}
	fm := map[string]string{}
	for _, line := range strings.Split(rest[:end], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") {
			fm[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	rest = rest[end+4:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		rest = ""
	}
	return fm, rest
}

// ---- sectioning ----

type section struct {
	level  int
	path   []string
	anchor string
	blocks []chunking.Block
}

var (
	atxHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	anchorAttr = regexp.MustCompile(`\s*\{#([A-Za-z0-9_-]+)\}\s*$`)
)

// sections splits normalized Markdown on ATX headings (outside fences) and
// cuts each section body into blocks: paragraphs are shared, fenced code in
// java/cpp/python is language-tagged.
func sections(md string) []section {
	var out []section
	var stack []string // heading texts by level-1
	cur := section{}
	var para []string
	flush := func() {
		t := strings.TrimSpace(strings.Join(para, "\n"))
		para = para[:0]
		if t != "" {
			cur.blocks = append(cur.blocks, chunking.Block{Text: t})
		}
	}
	emit := func() {
		flush()
		if len(cur.blocks) > 0 {
			out = append(out, cur)
		}
	}
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if fence, info, ok := openFence(line); ok {
			flush()
			var code []string
			j := i + 1
			for ; j < len(lines); j++ {
				if closesFence(lines[j], fence) {
					break
				}
				code = append(code, lines[j])
			}
			i = j
			lang := chunking.CodeLang(firstWord(info))
			tag := lang
			if tag == "" {
				tag = normalizeInfo(info)
			}
			text := "```" + tag + "\n" + strings.Trim(dedent(strings.Join(code, "\n")), "\n") + "\n```"
			if strings.TrimSpace(strings.Join(code, "")) != "" {
				cur.blocks = append(cur.blocks, chunking.Block{Lang: lang, Text: text})
			}
			continue
		}
		if m := atxHeading.FindStringSubmatch(line); m != nil {
			emit()
			level := len(m[1])
			text := m[2]
			anchor := ""
			if a := idAttr.FindStringSubmatch(text); a != nil { // GitBook: ## Title <a href="#x" id="x"></a>
				anchor = a[1]
			}
			if a := anchorAttr.FindStringSubmatch(text); a != nil {
				anchor = a[1]
				text = strings.TrimSpace(text[:len(text)-len(a[0])])
			}
			text = inline(text)
			if anchor == "" {
				anchor = slug(text)
			}
			if level > len(stack)+1 {
				level = len(stack) + 1 // tolerate skipped levels
			}
			stack = append(stack[:level-1], text)
			cur = section{level: level, path: append([]string(nil), stack...), anchor: anchor}
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		para = append(para, inline(line))
	}
	emit()
	return out
}

func openFence(line string) (fence, info string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return "", "", false
	}
	for _, ch := range []string{"`", "~"} {
		n := 0
		for n < len(t) && t[n:n+1] == ch {
			n++
		}
		if n >= 3 {
			info = strings.TrimSpace(t[n:])
			if ch == "`" && strings.Contains(info, "`") {
				return "", "", false
			}
			return t[:n], info, true
		}
	}
	return "", "", false
}

func closesFence(line, fence string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == ""
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return strings.Trim(f[0], "{}.")
	}
	return ""
}

// normalizeInfo keeps a plain language tag for non-robot-code fences
// (json, bash, …) and drops attributes such as title="Drive.java".
func normalizeInfo(info string) string {
	w := strings.ToLower(firstWord(info))
	for _, r := range w {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '+' {
			return ""
		}
	}
	return w
}

func dedent(s string) string {
	lines := strings.Split(s, "\n")
	min := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return s
	}
	for i, l := range lines {
		if len(l) >= min {
			lines[i] = l[min:]
		} else {
			lines[i] = strings.TrimLeft(l, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

var (
	mystRole   = regexp.MustCompile("\\{[a-z:-]+\\}`([^`<]*?)\\s*(?:<[^`>]*>)?`")
	htmlInline = regexp.MustCompile(`</?(?:kbd|span|sup|sub|b|i|em|strong|u|br|code)\b[^>]*>`)
	imgMD      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	emptyA     = regexp.MustCompile(`<a\b[^>]*>\s*</a>`)
	aTag       = regexp.MustCompile(`</?a\b[^>]*>`)
	idAttr     = regexp.MustCompile(`<a\b[^>]*\bid="([^"]+)"`)
)

// inline rewrites inline dialect syntax: MyST roles keep their text, images
// are dropped (agents cannot see them), inline HTML tags are unwrapped.
func inline(s string) string {
	s = mystRole.ReplaceAllString(s, "`$1`")
	s = imgMD.ReplaceAllString(s, "")
	s = emptyA.ReplaceAllString(s, "")
	s = aTag.ReplaceAllString(s, "")
	s = htmlInline.ReplaceAllString(s, "")
	return strings.TrimRight(s, " ")
}

// slug is the GitHub/Docusaurus/MyST-style heading anchor.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_':
			b.WriteRune(r)
			dash = false
		case r == ' ' || r == '-':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
