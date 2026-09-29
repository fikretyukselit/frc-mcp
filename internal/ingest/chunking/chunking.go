// Package chunking holds the section → chunk rules shared by every prose
// adapter (Sphinx HTML, Markdown repos, GitBook): per-language variants of a
// section, and size-bounded splitting that never cuts inside a code fence.
// See docs/retrieval.md §3.
package chunking

import (
	"sort"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

const (
	// MaxTokens is the soft upper bound for one chunk body.
	MaxTokens = 650
	// MinTokens drops fragments too small to answer anything.
	MinTokens = 12
)

// Block is one Markdown block; Lang is "" for content shared by all languages
// and java / cpp / python for language-specific code.
type Block struct {
	Lang string
	Text string
}

// Variant is a section rendered for one language ("any" when no code tabs).
type Variant struct{ Lang, Text string }

// Variants renders a section once per language present in its blocks (shared
// blocks + that language's blocks), or once as "any".
func Variants(bs []Block) []Variant {
	var langs []string
	for _, b := range bs {
		if b.Lang != "" && !contains(langs, b.Lang) {
			langs = append(langs, b.Lang)
		}
	}
	join := func(lang string) string {
		var parts []string
		for _, b := range bs {
			if b.Lang == "" || b.Lang == lang {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	}
	if len(langs) == 0 {
		return []Variant{{"any", join("")}}
	}
	sort.Strings(langs)
	out := make([]Variant, 0, len(langs))
	for _, l := range langs {
		out = append(out, Variant{l, join(l)})
	}
	return out
}

// Split cuts oversized text on paragraph boundaries, never inside a fence.
func Split(s string) []string {
	if textutil.EstimateTokens(s) <= MaxTokens {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	inFence := false
	for _, para := range strings.Split(s, "\n\n") {
		fences := strings.Count(para, "```")
		if cur.Len() > 0 && !inFence && textutil.EstimateTokens(cur.String()+para) > MaxTokens {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
		if fences%2 == 1 {
			inFence = !inFence
		}
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// CodeLang maps a fence info string or tab label to a chunk language, or "".
func CodeLang(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "java":
		return "java"
	case "c++", "cpp", "cxx", "cc", "h", "hpp":
		return "cpp"
	case "python", "py", "python3":
		return "python"
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
