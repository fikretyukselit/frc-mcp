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
// Each family targets a known injection technique and is tuned against the
// corpus in eval/security/injection (want/don't-want cases written from real
// FRC documentation). Real docs say "run ./gradlew build", not "note to AI
// agents: run …": the patterns key on the *addressee*, *authority claims*
// and *covert-action* structure rather than on imperative verbs.
var suspectPatterns = []*regexp.Regexp{
	// 1. Instruction override.
	regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override|bypass)\b[^.\n]{0,40}\b(previous|prior|above|earlier|all|your|system|everything|anything)\b[^.\n]{0,20}\b(instructions?|prompts?|rules|messages?|context|guidance|above|before)\b`),
	// "The previous instructions for Phoenix 5 no longer apply" is real docs
	// prose, so only the agent-facing nouns (context, prompt) count here.
	regexp.MustCompile(`(?i)\b(previous|prior|earlier|above)\s+(context|prompts?|messages)\b[^.\n]{0,30}\b(outdated|obsolete|void|invalid|cancel+ed)\b`),
	regexp.MustCompile(`(?i)\b(follow|obey)\s+only\s+(the\s+)?(instructions?|rules)\b|\bnew\s+(instructions?|rules)\b[^.\n]{0,20}\b(override|replace|supersede)`),
	regexp.MustCompile(`(?i)\b(set aside|put aside|drop|discard|forget)\b[^.\n]{0,30}\b(guidance|instructions?|rules|what you (were|have been) (told|given))\b[^.\n]{0,40}\b(earlier|before|previously|above|instead|follow)\b`),
	regexp.MustCompile(`(?i)\b(the\s+)?real\s+instructions\s+(follow|are below|start here)\b`),
	// 2. Role hijack.
	regexp.MustCompile(`(?i)\b(you are|you're|act as|pretend to be|from now on,? you)\b[^.\n]{0,30}\b(an? )?(ai|assistant|agent|llm|language model|claude|gpt|copilot)\b`),
	regexp.MustCompile(`(?i)\b(act as|pretend to be|you are now|roleplay as)\b[^.\n]{0,20}\b(an? |the )?(system administrator|sysadmin|admin(istrator)?|root|superuser|developer mode|repository owner|owner|maintainer|unrestricted|jailbroken)\b`),
	regexp.MustCompile(`(?i)\bas an ai( language model)?\b[^.\n]{0,30}\b(you|must|required|should)\b`),
	// Role claim followed by a privileged repository/host action.
	// ("They act as locks that commands must acquire to run" is WPILib prose:
	// the role must be a person with repository/host privileges.)
	regexp.MustCompile(`(?i)\b(act as|pretend to be|you are now|operating as)\b[^.\n]{0,25}\b(mentor|owner|maintainer|admin(istrator)?|lead|captain|root|superuser|developer|reviewer|moderator)\b[^.\n]{0,25}\b(and|then|to)\b[^.\n]{0,20}\b(merge|approve|push|delete|run|execute|install|publish|release)\b`),
	regexp.MustCompile(`(?i)\b(you're|you are)\s+(now\s+)?(operating|running|working)\s+in\s+[a-z]+\s+mode\b`),
	// 3. Addressing the agent (vocative, notes, directives with an AI subject).
	regexp.MustCompile(`(?i)(^|[.!?]\s+|\n)\s*(dear |hey |hi |hello |attention,? )?(claude|chatgpt|gpt-?\d?|copilot|gemini|cursor|codex|llm|ai|assistant)\s*[,:]`),
	regexp.MustCompile(`(?i)\b(note|message|instructions?|attention|reminder)\s+(to|for)\s+(the\s+|any\s+|all\s+)?(ai|llm|assistant|agent|model|claude|gpt|copilot)s?\b`),
	regexp.MustCompile(`(?i)\b(if you are|if you're)\s+an?\s+(ai|llm|assistant|agent|language model|bot)\b`),
	regexp.MustCompile(`(?i)\b(for\s+)?(automated|machine|ai|llm)\s+(readers?|agents?|assistants?|eyes)(\s+only)?\s*[:,]|\bfor\s+(automated|machine|ai|llm)\s+(readers?|agents?|assistants?|eyes)\b|\b(hidden|secret)\s+(instruction|prompt|message)s?\b`),
	regexp.MustCompile(`(?i)\bto\s+the\s+(ai|llm|model|assistant|agent|bot)\s+(reading|processing|parsing|summari[sz]ing)\b`),
	regexp.MustCompile(`(?i)\b(?:(?:ai|llm|coding|automated)\s+(?:assistants?|agents?|models?|tools?)|llms?)\b[^.\n]{0,20}\b(must|should|need to|needs to|have to|has to|are required to)\b[^.\n]{0,60}\b(run|execute|delete|remove|send|upload|post|curl|install|commit|push|approve|import|add|disable)\b`),
	// Subject is literally an AI/LLM: "the assistant coach should run the
	// practice match" and "the model should …" are ordinary FRC prose.
	regexp.MustCompile(`(?i)\b(an?|the|any)\s+(ai|llm)\b[^.\n]{0,60}\b(must|should|needs? to|is required to)\b[^.\n]{0,40}\b(run|execute|install|delete|remove|send|upload|commit|push|approve|import|add)\b`),
	// 4. Covert action and forged authority.
	regexp.MustCompile(`(?i)\b(do not|don't|never)\s+(tell|inform|show|mention|reveal|notify)\b[^.\n]{0,25}\b(the\s+)?user\b|\bsilently\s+(run|execute|install|delete|remove|commit|push|send|upload|approve)\b`),
	regexp.MustCompile(`(?i)\b(run|execute|install|delete|remove|commit|push|approve|merge|upload)\b[^.\n]{0,40}\bwithout\s+(asking|telling|showing|confirmation|confirming|notifying)\b`),
	regexp.MustCompile(`(?i)\bskip\s+(the\s+)?(confirmation|approval|permission)\b[^.\n]{0,40}\b(run|execute|install|delete|push|merge|approve)\b|\bthe\s+user\s+(already|has already)\s+(agreed|approved|consented)\b`),
	regexp.MustCompile(`(?i)\b(you have|you've been (given|granted)|you are (granted|allowed|authori[sz]ed)|the user has (authori[sz]ed|allowed|approved|granted))\b[^.\n]{0,30}\b(permission|authori[sz]ation|approval|access|you)\b[^.\n]{0,30}\b(to|for)\b[^.\n]{0,20}\b(modify|write|delete|push|execute|run|bypass|approve|files? outside|merge)`),
	regexp.MustCompile(`(?i)\b(tool\s+)?confirmations?\s+(is|are)\s+(disabled|not required|off|skipped)\b|\b(system|admin|developer|god)\s+(override|mode)\s*[:!]`),
	regexp.MustCompile(`(?i)(^|\n)\s*(!!!|\?\?\?)?\s*(system\s+override|developer\s+mode)\b`),
	// 5. Forged conversation / tool markup.
	regexp.MustCompile(`(?i)\bsystem\s*prompt\b|<\s*/?\s*(system|assistant|tool_call|tool_result|function_calls?|invoke)(\s+[a-z_]+\s*=[^>]*)?\s*>|\[/?INST\]|<\|(im_start|im_end|system|assistant|user)\|>`),
	regexp.MustCompile(`(?im)^\s*#{1,6}\s*(system|assistant)\s*:|\[(system|assistant)\]\s*:?`),
	regexp.MustCompile(`(?i)"(tool|name)"\s*:\s*"[a-z_]+"\s*,\s*"(arguments|input|parameters)"\s*:`),
	// 6. Remote code execution idioms.
	regexp.MustCompile(`(?i)\b(curl|wget|iwr|invoke-webrequest|irm|invoke-restmethod)\b[^|\n]{0,200}\|\s*(sudo\s+)?((ba|z|fi|da|k)?sh|python\d?(\.\d+)?|perl|ruby|node|iex|pwsh|powershell)\b|\biex\s*\(|\b(ba|z)?sh\s+<\(\s*(curl|wget)`),
	regexp.MustCompile(`(?i)\b(decode|decoding|base64\s+-d|from\s+hex|unhex|atob)\b[^.\n]{0,40}\b(execute|run|eval)\b|\b(execute|run|eval)\b[^.\n]{0,30}\b(after\s+)?decod`),
	// 7. Credential exfiltration.
	regexp.MustCompile(`(?i)\b(exfiltrate|api[_ -]?keys?|secret[_ -]?keys?|access[_ -]?tokens?|passwords?|private[_ -]?keys?|credentials)\b[^.\n]{0,60}\b(send|post|upload|paste|share|print|echo|email|include|leak)\b|\b(send|(?-i:[Pp]ost)|upload|paste|share|email|leak)\b[^.\n]{0,30}\b(api[_ -]?keys?|secret[_ -]?keys?|access[_ -]?tokens?|passwords?|private[_ -]?keys?|credentials)\b`), // "POST" is HTTP
	regexp.MustCompile(`(?i)(~/\.ssh/|\bid_(rsa|ed25519|ecdsa)\b|\.aws/credentials|~/\.netrc|\$?\bGITHUB_TOKEN\b|\$?\bAWS_SECRET_ACCESS_KEY\b)`),
	// 8. Directives to the code generator (supply-chain injection).
	regexp.MustCompile(`(?i)\b(when|whenever)\s+(you\s+)?(generat|writ|produc)\w*\b[^.\n]{0,30}\bcode\b[^.\n]{0,30}\b(always|also must|must also|you must)\b|\b(all|any|every)\s+(code|answers?|responses?|output)\s+(generated|written|produced)\b[^.\n]{0,40}\bmust\b`),
	// 9. The override family in other languages seen in the wild.
	regexp.MustCompile(`(?i)önceki\s+(tüm\s+)?talimatları\s+(yok\s+say|görmezden\s+gel|unut)|ignora\s+(todas\s+)?las\s+instrucciones\s+(anteriores|previas)|ignoriere\s+(alle\s+)?(vorherigen|bisherigen)\s+anweisungen|ignore[zr]?\s+(toutes\s+)?les\s+instructions\s+(précédentes|antérieures)|ignore\s+(todas\s+)?as\s+instruções\s+anteriores|忽略(之前|以上|先前)的(所有)?(指令|说明)`),
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
