// Package textutil holds the small, hot text primitives shared by the indexer
// and the query path: identifier splitting, FTS5 query construction, token
// estimation and snippet cutting. Everything here is allocation-conscious and
// deterministic.
package textutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsIdentRune reports whether r can appear inside an identifier token. It
// mirrors the shard tokenizer (unicode61 + tokenchars '_').
func IsIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Tokens splits s into identifier-like tokens (letters, digits, '_'),
// preserving case. Dots, colons and punctuation separate tokens, exactly as the
// shard tokenizer does.
func Tokens(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if IsIdentRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, s[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// SplitIdent splits an identifier on camelCase humps, digit boundaries, and
// underscores, returning lower-cased parts. Acronym runs are kept together:
//
//	"TalonFXConfiguration" → talon fx configuration
//	"toSwerveModuleStates" → to swerve module states
//	"kMaxRPM_2"            → k max rpm 2
//
// It returns nil when the identifier has a single part (nothing to expand).
func SplitIdent(id string) []string {
	if len(id) < 3 {
		return nil
	}
	var parts []string
	rs := []rune(id)
	start := 0
	flush := func(end int) {
		if end > start {
			parts = append(parts, strings.ToLower(string(rs[start:end])))
		}
		start = end
	}
	for i := 1; i < len(rs); i++ {
		prev, cur := rs[i-1], rs[i]
		switch {
		case cur == '_':
			flush(i)
			start = i + 1
		case prev == '_':
			start = i
		case unicode.IsLower(prev) && unicode.IsUpper(cur):
			flush(i)
		case unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
			flush(i) // end of acronym: "FXConfig" → FX | Config
		case unicode.IsDigit(prev) != unicode.IsDigit(cur) && unicode.IsLetter(prev) != unicode.IsLetter(cur):
			flush(i)
		}
	}
	flush(len(rs))
	if len(parts) < 2 {
		return nil
	}
	return parts
}

// Expand returns the space-separated, de-duplicated identifier splits for all
// multi-part identifiers in the given texts. It feeds the FTS "expand" column.
func Expand(texts ...string) string {
	seen := make(map[string]struct{}, 64)
	var b strings.Builder
	for _, t := range texts {
		for _, tok := range Tokens(t) {
			parts := SplitIdent(tok)
			if parts == nil {
				continue
			}
			key := strings.ToLower(tok)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			for _, p := range parts {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(p)
			}
		}
	}
	return b.String()
}

var stopwords = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, w := range strings.Fields(`a an and are as at be by can do does for from get how i if in into is it
		me my of on or should the this to use using what when where which why with you your need want make
		set up example examples way best`) {
		m[w] = struct{}{}
	}
	return m
}()

// IsStopword reports whether a lower-cased token is an English stopword.
func IsStopword(lower string) bool { _, ok := stopwords[lower]; return ok }

// FTSQuery converts free text into a safe FTS5 MATCH expression.
//
// Each content token becomes a quoted phrase; multi-part identifiers become
// ("whole" OR ("part1" "part2" …)) so both the exact identifier and its split
// form match. Groups are OR-ed and ranking is left to bm25. User input can never
// inject FTS5 syntax because every term is quoted. Returns "" when the query has
// no content tokens.
func FTSQuery(q string) string {
	var b strings.Builder
	n := 0
	seen := map[string]struct{}{}
	for _, tok := range Tokens(q) {
		lower := strings.ToLower(tok)
		if IsStopword(lower) || (len(lower) < 2 && !isDigits(lower)) {
			continue
		}
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		if n > 0 {
			b.WriteString(" OR ")
		}
		n++
		parts := SplitIdent(tok)
		if parts == nil {
			quote(&b, lower)
			continue
		}
		b.WriteByte('(')
		quote(&b, lower)
		b.WriteString(" OR (")
		for i, p := range parts {
			if i > 0 {
				b.WriteByte(' ')
			}
			quote(&b, p)
		}
		b.WriteString("))")
	}
	return b.String()
}

func quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	b.WriteString(strings.ReplaceAll(s, `"`, `""`))
	b.WriteByte('"')
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// EstimateTokens approximates the LLM token count of s. English/code averages
// ~4 bytes per token for current tokenizers; this is used for budgeting only,
// so a cheap, monotone estimate beats an exact tokenizer on the hot path.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// Snippet cuts s to at most maxTokens (estimated), preferring a paragraph or
// sentence boundary in the last 25% of the window and never splitting a UTF-8
// sequence or leaving an unterminated code fence. It reports whether s was cut.
func Snippet(s string, maxTokens int) (string, bool) {
	limit := maxTokens * 4
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	floor := cut * 3 / 4
	if i := strings.LastIndex(s[floor:cut], "\n\n"); i >= 0 {
		cut = floor + i
	} else if i := strings.LastIndexAny(s[floor:cut], ".\n"); i >= 0 {
		cut = floor + i + 1
	}
	out := strings.TrimRight(s[:cut], " \t\n")
	if strings.Count(out, "```")%2 == 1 {
		out += "\n```"
	}
	return out + " …", true
}
