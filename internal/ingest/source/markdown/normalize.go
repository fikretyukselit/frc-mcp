package markdown

import (
	"regexp"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/ingest/chunking"
)

var (
	gitbookTag   = regexp.MustCompile(`^\s*\{%\s*(end)?([a-z-]+)\b(.*?)%\}\s*$`)
	labelAttr    = regexp.MustCompile(`\b(?:title|label)="([^"]*)"`)
	styleAttr    = regexp.MustCompile(`\b(?:style|value)="([^"]*)"`)
	anyTag       = regexp.MustCompile(`<[^>]+>`)
	htmlOnly     = regexp.MustCompile(`^\s*</?([A-Za-z][A-Za-z0-9-]*)\b[^>]*?/?>\s*$`)
	summaryLine  = regexp.MustCompile(`^\s*(?:<details[^>]*>)?\s*<summary>(.*?)</summary>\s*$`)
	figcaption   = regexp.MustCompile(`<figcaption>(.*?)</figcaption>`)
	figureLine   = regexp.MustCompile(`^\s*<figure\b.*</figure>\s*$`)
	writerAttr   = regexp.MustCompile(`^\s*\{[a-z-]+="[^"]*"(?:\s+[a-z-]+="[^"]*")*\}\s*$`)
	mdxLine      = regexp.MustCompile(`^(?:import\s.+\sfrom\s|import\s+['"]|export\s)`)
	colonFence   = regexp.MustCompile(`^(\s*)(:{3,})\s*(?:\{([a-z][a-z0-9-]*)\}|([a-z][a-z0-9-]*))?\s*(.*?)\s*$`)
	mkdocsTab    = regexp.MustCompile(`^(\s*)===\+?\s+"([^"]*)"\s*$`)
	mkdocsAdm    = regexp.MustCompile(`^(\s*)(?:!!!|\?\?\?\+?)\s+([a-z-]+)(?:\s+"([^"]*)")?\s*$`)
	mkdocsAdm1   = regexp.MustCompile(`^(\s*)(?:!!!|\?\?\?\+?)\s+([a-z-]+)\s+([^"\s].*)$`) // one-line form: !!! tip text
	mediaTag     = regexp.MustCompile(`(?i)^\s*<(iframe|video|audio|img|source|picture|svg|script|style|br|hr)\b`)
	directiveOpn = regexp.MustCompile("^(\\s*)(`{3,}|~{3,})\\{([a-z][a-z0-9-]*)\\}\\s*(.*?)\\s*$")
	rstDirective = regexp.MustCompile(`^(\s*)\.\.\s+([a-z][a-z0-9-]*)::\s*(.*?)\s*$`)
	optionLine   = regexp.MustCompile(`^\s*:[a-z][a-z0-9-]*:`)
	llmsHeader   = regexp.MustCompile(`(?i)^>\s*for the complete documentation index`)
)

// admonitions become a bold label so the warning survives as text.
var admonitions = map[string]string{
	"note": "Note", "tip": "Tip", "info": "Info", "important": "Important", "warning": "Warning",
	"caution": "Caution", "danger": "Danger", "attention": "Attention", "hint": "Hint", "success": "Tip",
	"error": "Error", "seealso": "See also", "example": "Example", "abstract": "Summary", "question": "Question",
	"quote": "Quote", "bug": "Bug", "failure": "Failure", "admonition": "Note", "todo": "Note",
}

// dropDirectives are blocks with no text an agent can use.
var dropDirectives = map[string]bool{
	"image": true, "figure": true, "toctree": true, "raw": true, "contents": true, "video": true,
	"literalinclude": true, "rli": true, "include": true, "only": true, "mermaid": true, "embed": true,
	"content-ref": true, "grid": true, "card": true,
}

// Normalize rewrites dialect syntax (MyST, Docusaurus/MDX, Writerside, MkDocs
// Material, GitBook) into CommonMark. Code fences pass through untouched.
func Normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	lines = dedentContainers(lines)
	var out []string
	emitLabel := func(label string) {
		if label = strings.TrimSpace(inline(label)); label != "" {
			out = append(out, "", "**"+strings.TrimSuffix(label, ":")+":**")
		}
	}
	tabLabel := func(label string) {
		if chunking.CodeLang(label) == "" && !codeTabLabel(label) {
			emitLabel(label)
		}
	}
	skipUntil := "" // gitbook block being dropped
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		// MyST backtick directives: ```{name} args
		if m := directiveOpn.FindStringSubmatch(line); m != nil {
			fence, name, arg := m[2], m[3], m[4]
			var body []string
			j := i + 1
			for ; j < len(lines) && !closesFence(lines[j], fence); j++ {
				body = append(body, lines[j])
			}
			i = j
			body = stripOptions(body)
			switch {
			case dropDirectives[name]:
			case name == "eval-rst":
				out = append(out, rst(body)...)
			case name == "code-block" || name == "code" || name == "sourcecode":
				out = append(out, "```"+firstWord(arg))
				out = append(out, body...)
				out = append(out, "```")
			case admonitions[name] != "":
				emitLabel(admonitionTitle(name, arg))
				out = append(out, body...)
				out = append(out, "")
			default:
				out = append(out, body...)
			}
			continue
		}
		if fence, _, ok := openFence(line); ok {
			out = append(out, line)
			for i++; i < len(lines); i++ {
				out = append(out, lines[i])
				if closesFence(lines[i], fence) {
					break
				}
			}
			continue
		}
		// GitBook liquid tags.
		if m := gitbookTag.FindStringSubmatch(line); m != nil {
			end, name, args := m[1] != "", m[2], m[3]
			switch {
			case skipUntil != "":
				if end && name == skipUntil {
					skipUntil = ""
				}
			case end:
			case name == "content-ref":
				skipUntil = name
			case name == "hint":
				style := "info"
				if a := styleAttr.FindStringSubmatch(args); a != nil {
					style = a[1]
				}
				emitLabel(admonitionTitle(style, ""))
			case name == "tab" || name == "step":
				if a := labelAttr.FindStringSubmatch(args); a != nil {
					tabLabel(a[1])
				}
			}
			continue
		}
		if skipUntil != "" {
			continue
		}
		switch {
		case llmsHeader.MatchString(line), writerAttr.MatchString(line), mdxLine.MatchString(line):
			continue
		case mediaTag.MatchString(line):
			continue
		case figureLine.MatchString(line):
			if c := figcaption.FindStringSubmatch(line); c != nil && strings.TrimSpace(anyTag.ReplaceAllString(c[1], "")) != "" {
				out = append(out, "", "*Figure: "+strings.TrimSpace(anyTag.ReplaceAllString(c[1], ""))+"*", "")
			}
			continue
		}
		if m := summaryLine.FindStringSubmatch(line); m != nil {
			emitLabel(m[1])
			continue
		}
		if m := htmlOnly.FindStringSubmatch(line); m != nil {
			tag := strings.ToLower(m[1])
			if (tag == "tab" || tag == "tabitem") && !strings.HasPrefix(strings.TrimSpace(line), "</") {
				if a := labelAttr.FindStringSubmatch(line); a != nil {
					tabLabel(a[1])
				} else if a := styleAttr.FindStringSubmatch(line); a != nil {
					tabLabel(a[1])
				}
			}
			continue
		}
		// Bare RST directives outside {eval-rst} (an upstream authoring slip that
		// Sphinx still renders): convert the directive and its indented body.
		if m := rstDirective.FindStringSubmatch(line); m != nil {
			j := i + 1
			for ; j < len(lines); j++ {
				l := lines[j]
				if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " \t")) <= len(m[1]) {
					break
				}
			}
			out = append(out, rst(lines[i:j])...)
			i = j - 1
			continue
		}
		if m := mkdocsAdm1.FindStringSubmatch(line); m != nil {
			out = append(out, "", "**"+admonitionTitle(m[2], "")+":** "+inline(m[3]))
			continue
		}
		// MyST / Docusaurus colon fences: :::{note} / :::tip Title / ::::{tab-set} / :::{tab-item} Java / :::
		if m := colonFence.FindStringSubmatch(line); m != nil {
			name := m[3] + m[4]
			switch {
			case name == "":
			case name == "tab-item":
				tabLabel(m[5])
			case admonitions[name] != "":
				emitLabel(admonitionTitle(name, m[5]))
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// codeTabLabel recognizes tab labels that only name a code language variant.
func codeTabLabel(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	switch l {
	case "java/c++", "java / c++", "kotlin", "c++ (header)", "c++ (source)", "c++ header", "c++ source":
		return true
	}
	return false
}

func admonitionTitle(name, arg string) string {
	if strings.TrimSpace(arg) != "" {
		return arg
	}
	if t := admonitions[strings.ToLower(name)]; t != "" {
		return t
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func stripOptions(body []string) []string {
	for len(body) > 0 && optionLine.MatchString(body[0]) {
		body = body[1:]
	}
	return body
}

// dedentContainers handles MkDocs Material containers whose bodies are
// indented four spaces under the marker (=== "Tab", !!! note). Markers become
// labels; bodies are shifted left so fences and headings parse normally.
// Nested containers work because rewritten body lines are scanned again.
func dedentContainers(lines []string) []string {
	out := append([]string(nil), lines...)
	fence := ""
	for i := 0; i < len(out); i++ {
		line := out[i]
		if fence != "" {
			if closesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if f, _, ok := openFence(line); ok {
			fence = f
			continue
		}
		indent, label := "", ""
		if m := mkdocsTab.FindStringSubmatch(line); m != nil {
			indent = m[1]
			if chunking.CodeLang(m[2]) == "" && !codeTabLabel(m[2]) {
				label = m[2]
			}
		} else if m := mkdocsAdm.FindStringSubmatch(line); m != nil {
			indent, label = m[1], admonitionTitle(m[2], m[3])
		} else {
			continue
		}
		if label != "" {
			out[i] = indent + "**" + inline(label) + ":**"
		} else {
			out[i] = ""
		}
		body := indent + "    "
		for j := i + 1; j < len(out); j++ {
			l := out[j]
			if strings.TrimSpace(l) == "" {
				continue
			}
			if !strings.HasPrefix(l, body) {
				break
			}
			out[j] = indent + l[len(body):]
		}
	}
	return out
}

// rst converts the reStructuredText islands MyST sites embed ({eval-rst}):
// code-block directives become fences, tab labels become labels, and
// admonitions become bold labels. Unknown directives are dropped with their
// indented bodies; plain text is kept.
func rst(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		m := rstDirective.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		indent, name, arg := len(m[1]), m[2], m[3]
		// Body: following blank or more-indented lines; options come first.
		j := i + 1
		for ; j < len(lines) && optionLine.MatchString(lines[j]); j++ {
		}
		var body []string
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " \t")) <= indent {
				break
			}
			body = append(body, l)
		}
		switch {
		case name == "code-block" || name == "code" || name == "sourcecode":
			out = append(out, "", "```"+firstWord(arg), strings.Trim(dedent(strings.Join(body, "\n")), "\n"), "```", "")
			i = j - 1
		case name == "tab-set" || name == "tab-set-code" || name == "tabs" || name == "group-tab" || name == "tab-item" || name == "tab":
			if (name == "tab-item" || name == "tab" || name == "group-tab") && chunking.CodeLang(arg) == "" && !codeTabLabel(arg) && arg != "" {
				out = append(out, "", "**"+arg+":**")
			}
			// Keep the body; its nested directives are handled on the next iterations.
			out = append(out, strings.Split(dedent(strings.Join(body, "\n")), "\n")...)
			i = j - 1
		case admonitions[name] != "":
			out = append(out, "", "**"+admonitionTitle(name, "")+":** "+arg)
			out = append(out, strings.Split(dedent(strings.Join(body, "\n")), "\n")...)
			i = j - 1
		default:
			i = j - 1 // drop (rli, image, include, …)
		}
	}
	// A second pass resolves directives that were nested inside tab bodies.
	for _, l := range out {
		if rstDirective.MatchString(l) {
			return rst(out)
		}
	}
	return out
}
