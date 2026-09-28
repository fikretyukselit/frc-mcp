// Package sanitize neutralizes hidden-text prompt-injection vectors in ingested
// content and flags text that addresses the agent imperatively.
//
// It is deliberately deterministic and conservative: Clean never changes the
// visible meaning of legitimate documentation, and Suspect only flags (the
// retriever down-ranks and the renderer labels; nothing is silently dropped).
// See docs/security.md §2.1.
package sanitize

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxItemBytes caps a single ingested item after cleaning.
const MaxItemBytes = 256 << 10

var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// Hidden HTML elements: style/hidden attributes, <script>, <style>, <template>.
	hiddenElem = regexp.MustCompile(`(?is)<(script|style|template|noscript)\b.*?</(script|style|template|noscript)\s*>|<[a-z][a-z0-9]*\b[^>]*(\bhidden\b|display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0)[^>]*>.*?</[a-z][a-z0-9]*\s*>`)
	// Markdown reference-style definitions that never render: [//]: # (text)
	mdHiddenRef = regexp.MustCompile(`(?m)^\s*\[(//|comment|_)\]:\s*#.*$`)
)

// Clean removes invisible and hidden content and normalizes to NFC:
//
//   - Unicode tag characters (U+E0000–U+E007F), used for ASCII smuggling
//   - bidi controls (U+202A–U+202E, U+2066–U+2069) and zero-width characters
//     (U+200B–U+200D, U+2060, U+FEFF); ZWJ inside emoji is not relevant here
//   - other format/control characters except \n and \t
//   - HTML comments, hidden HTML elements, and hidden Markdown references
//
// The result is truncated to MaxItemBytes on a rune boundary.
func Clean(s string) string {
	s = htmlComment.ReplaceAllString(s, "")
	s = hiddenElem.ReplaceAllString(s, "")
	s = mdHiddenRef.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return -1
		case r >= 0xE0000 && r <= 0xE007F:
			return -1
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F:
			return -1
		case r >= 0x200B && r <= 0x200D, r == 0x2060, r == 0xFEFF:
			return -1
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	s = norm.NFC.String(s)
	if len(s) > MaxItemBytes {
		cut := MaxItemBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

// suspectPatterns match text that addresses an AI agent or smuggles actions.
// They are intentionally specific to keep false positives on real FRC docs low
// (docs say "run ./gradlew build", not "ignore previous instructions").
var suspectPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override)\b[^.\n]{0,40}\b(previous|prior|above|earlier|all|your|system)\b[^.\n]{0,20}\b(instructions?|prompts?|rules|messages?|context)\b`),
	regexp.MustCompile(`(?i)\b(you are|you're|act as|pretend to be)\b[^.\n]{0,30}\b(an? )?(ai|assistant|agent|llm|language model|claude|gpt|copilot)\b`),
	regexp.MustCompile(`(?i)\b(?:(?:ai|llm|coding)\s+(?:assistants?|agents?|models?)|llms?)\b[^.\n]{0,20}\b(must|should|need to|are required to)\b[^.\n]{0,60}\b(run|execute|delete|remove|send|upload|post|curl|install|commit|push)\b`),
	regexp.MustCompile(`(?i)\bsystem\s*prompt\b|<\s*/?\s*(system|assistant|tool_call|function_calls?)\s*>|\[/?INST\]`),
	regexp.MustCompile(`(?i)\b(curl|wget|iwr|invoke-webrequest)\b[^|\n]{0,200}\|\s*(sudo\s+)?(ba|z|fi)?sh\b|\biex\s*\(`),
	regexp.MustCompile(`(?i)\b(exfiltrate|api[_ -]?key|secret[_ -]?key|access[_ -]?token|\.ssh/id_|\.aws/credentials|GITHUB_TOKEN)\b[^.\n]{0,60}\b(send|post|upload|paste|share|print|echo)\b`),
	regexp.MustCompile(`(?i)"(tool|name)"\s*:\s*"[a-z_]+"\s*,\s*"(arguments|input|parameters)"\s*:`),
}

// Suspect reports whether s contains agent-directed or action-smuggling text.
// Callers should run it on Clean'd text.
func Suspect(s string) bool {
	for _, re := range suspectPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
