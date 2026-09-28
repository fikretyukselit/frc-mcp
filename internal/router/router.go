// Package router is the deterministic query decision model (docs/architecture.md
// §4.1). Given a free-text query it infers intent, identifiers, libraries,
// season and language from cheap lexical signals. It never calls a model and
// runs in microseconds; every rule is covered by a table test.
package router

import (
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// Intent classifies what the caller is trying to do.
type Intent string

const (
	IntentGeneral      Intent = "general"
	IntentSymbol       Intent = "symbol"
	IntentHowTo        Intent = "howto"
	IntentTroubleshoot Intent = "troubleshoot"
)

// Decision is the router's output. Zero-valued fields mean "no signal".
type Decision struct {
	Intent      Intent   `json:"intent"`
	Identifiers []string `json:"identifiers,omitempty"` // symbol-shaped tokens, most specific first
	Libraries   []string `json:"libraries,omitempty"`   // inferred library ids
	Season      string   `json:"season,omitempty"`      // season named in the query
	Language    string   `json:"language,omitempty"`    // java | cpp | python
	NonEnglish  bool     `json:"non_english,omitempty"` // lexical-first routing advised
}

// libraryPrefixes maps package/namespace prefixes to library ids. Longest
// prefix wins; order is irrelevant.
var libraryPrefixes = map[string]string{
	"edu.wpi.first":         "wpilib",
	"org.wpilib":            "wpilib",
	"frc::":                 "wpilib",
	"wpi::":                 "wpilib",
	"wpilib.":               "wpilib",
	"wpimath.":              "wpilib",
	"commands2":             "wpilib",
	"com.ctre.phoenix6":     "phoenix6",
	"ctre::phoenix6":        "phoenix6",
	"phoenix6.":             "phoenix6",
	"com.ctre.phoenix.":     "phoenix5",
	"ctre::phoenix::":       "phoenix5",
	"com.revrobotics":       "revlib",
	"rev::":                 "revlib",
	"org.photonvision":      "photonvision",
	"photon::":              "photonvision",
	"photonlibpy":           "photonvision",
	"com.pathplanner":       "pathplannerlib",
	"pathplannerlib":        "pathplannerlib",
	"choreo":                "choreolib",
	"org.littletonrobotics": "advantagekit",
	"swervelib":             "yagsl",
	"com.reduxrobotics":     "reduxlib",
	"com.studica":           "studica",
	"limelighthelpers":      "limelight",
	"com.limelightvision":   "limelight",
	"au.grapplerobotics":    "grapple",
	"org.ironmaple":         "maple-sim",
}

// libraryWords maps plain-language mentions to library ids.
var libraryWords = map[string]string{
	"wpilib": "wpilib", "robotpy": "wpilib",
	"phoenix6": "phoenix6", "ctre": "phoenix6", "talonfx": "phoenix6", "kraken": "phoenix6", "falcon": "phoenix6",
	"cancoder": "phoenix6", "pigeon2": "phoenix6", "canivore": "phoenix6",
	"phoenix5": "phoenix5", "talonsrx": "phoenix5", "victorspx": "phoenix5",
	"revlib": "revlib", "sparkmax": "revlib", "sparkflex": "revlib", "cansparkmax": "revlib", "neo": "revlib", "vortex": "revlib",
	"photonvision": "photonvision", "photonlib": "photonvision",
	"pathplanner": "pathplannerlib", "pathplannerlib": "pathplannerlib", "autobuilder": "pathplannerlib",
	"choreo": "choreolib", "choreolib": "choreolib",
	"advantagekit": "advantagekit", "akit": "advantagekit",
	"yagsl": "yagsl", "limelight": "limelight", "reduxlib": "reduxlib", "canandmag": "reduxlib",
	"navx": "studica", "lasercan": "grapple", "maplesim": "maple-sim",
}

// Lexical signal sets, matched against lower-cased tokens. Token-set lookups
// are ~20× faster than equivalent alternation regexps.
var (
	troubleWords = set(`error errors exception exceptions crash crashes crashing fail fails failing failed
		broken bug bugs stacktrace nullpointerexception npe unresolved brownout brownouts timeout timeouts
		hata çalışmıyor`)
	troublePhrases = []string{"doesn't work", "doesnt work", "not working", "stack trace", "cannot find symbol",
		"known issue", "can't find", "no such"}
	howtoLead   = set(`how example examples tutorial guide setup configure implement write create show`)
	howtoPhrase = []string{"how do", "how to", "how can", "how should", "what is", "what are", "what does",
		"best way", "set up"}
	cppMarkers    = []string{"#include", "::", "std::", "->", " c++", "cpp"}
	pythonMarkers = []string{"def ", "self.", "import wpilib", "from wpilib", "python", "robotpy", "commands2", "photonlibpy"}
	javaMarkers   = []string{"import edu.", "import org.", "import com.", "public class", "public void", " java", "gradle", "new "}
)

func set(words string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, w := range strings.Fields(words) {
		m[w] = struct{}{}
	}
	return m
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// Decide routes a query. It is pure and allocation-light.
func Decide(q string) Decision {
	d := Decision{Intent: IntentGeneral}
	lower := strings.ToLower(q)

	// Identifiers: qualified names and CamelCase / camelCase tokens.
	for _, f := range strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '(' || r == ')' || r == '`' || r == '"' || r == '\'' || r == ';' || r == '?'
	}) {
		f = strings.Trim(f, ".:")
		if isIdentifier(f) {
			d.Identifiers = appendUnique(d.Identifiers, f)
		}
		if lib := libraryForQualified(strings.ToLower(f)); lib != "" {
			d.Libraries = appendUnique(d.Libraries, lib)
		}
	}
	toks := textutil.Tokens(lower)
	phoenix := ""
	for i, tok := range toks {
		if lib, ok := libraryWords[tok]; ok {
			d.Libraries = appendUnique(d.Libraries, lib)
		}
		if d.Season == "" && isSeason(tok) {
			d.Season = tok
		}
		// "phoenix 5", "phoenix v6", "Phoenix6": an explicit major version wins.
		if tok == "phoenix" && phoenix == "" && i+1 < len(toks) {
			if v := strings.TrimPrefix(toks[i+1], "v"); v == "5" || v == "6" {
				phoenix = v
			}
		}
	}
	if phoenix != "" {
		d.Libraries = removeValue(d.Libraries, "phoenix5", "phoenix6")
		d.Libraries = appendUnique(d.Libraries, "phoenix"+phoenix)
	}

	padded := " " + lower
	isJava := containsAny(padded, javaMarkers) && !strings.Contains(lower, "new to")
	switch {
	case containsAny(lower, cppMarkers) && !isJava:
		d.Language = "cpp"
	case containsAny(padded, pythonMarkers):
		d.Language = "python"
	case isJava:
		d.Language = "java"
	}
	if d.Language == "" {
		for _, id := range d.Identifiers {
			if strings.Contains(id, "::") {
				d.Language = "cpp"
				break
			}
			if strings.HasPrefix(id, "edu.wpi.") || strings.HasPrefix(id, "org.wpilib.") || strings.HasPrefix(id, "com.") {
				d.Language = "java"
				break
			}
		}
	}

	switch {
	case isTrouble(lower, toks):
		d.Intent = IntentTroubleshoot
	case len(d.Identifiers) > 0 && identifierDominant(q, d.Identifiers):
		d.Intent = IntentSymbol
	case (len(toks) > 0 && inSet(howtoLead, toks[0])) || containsAny(lower, howtoPhrase):
		d.Intent = IntentHowTo
	}
	d.NonEnglish = nonEnglish(q)
	return d
}

// isSeason accepts 2020–2039.
func isSeason(t string) bool {
	return len(t) == 4 && t[0] == '2' && t[1] == '0' && (t[2] == '2' || t[2] == '3') && t[3] >= '0' && t[3] <= '9'
}

func isTrouble(lower string, toks []string) bool {
	for _, t := range toks {
		if inSet(troubleWords, t) {
			return true
		}
	}
	return containsAny(lower, troublePhrases)
}

func inSet(m map[string]struct{}, k string) bool { _, ok := m[k]; return ok }

// isIdentifier accepts qualified names (a.b.C, ns::T, T#m) and tokens with an
// internal case change or underscore (TalonFX, toSwerveModuleStates, k_max).
func isIdentifier(s string) bool {
	if len(s) < 3 || len(s) > 200 {
		return false
	}
	if strings.Contains(s, "::") || strings.Contains(s, "#") {
		return true
	}
	if strings.Count(s, ".") >= 2 && !strings.Contains(s, "..") {
		for _, part := range strings.Split(s, ".") {
			if part == "" || !isWord(part) {
				return false
			}
		}
		return true
	}
	if !isWord(s) {
		return false
	}
	upper, lower := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'A' && c <= 'Z':
			if i > 0 {
				upper++
			}
		case c >= 'a' && c <= 'z':
			lower++
		}
	}
	return (upper > 0 && lower > 0) || (strings.Contains(s, "_") && len(s) > 3)
}

func isWord(s string) bool {
	for _, r := range s {
		if !textutil.IsIdentRune(r) {
			return false
		}
	}
	return s != ""
}

// identifierDominant: the query is mostly symbols (e.g. "TalonFX#setControl",
// "SparkMaxConfig smartCurrentLimit"), not prose that mentions one.
func identifierDominant(q string, ids []string) bool {
	words := 0
	for _, f := range strings.Fields(q) {
		if !textutil.IsStopword(strings.ToLower(strings.Trim(f, "?.,!"))) {
			words++
		}
	}
	return words <= 2*len(ids)+1
}

func libraryForQualified(s string) string {
	best, bestLen := "", 0
	for p, lib := range libraryPrefixes {
		if strings.HasPrefix(s, p) && len(p) > bestLen {
			best, bestLen = lib, len(p)
		}
	}
	return best
}

// nonEnglish flags queries with a substantial share of non-ASCII letters or
// common Turkish function words, so the retriever can favor lexical/symbol
// matching over the English-centric dense model.
func nonEnglish(q string) bool {
	var letters, nonASCII int
	for _, r := range q {
		if r > 127 {
			nonASCII++
			letters++
		} else if (r|0x20) >= 'a' && (r|0x20) <= 'z' {
			letters++
		}
	}
	if letters > 0 && nonASCII*10 >= letters {
		return true
	}
	padded := " " + strings.ToLower(q) + " "
	for _, w := range turkishMarkers {
		if strings.Contains(padded, w) {
			return true
		}
	}
	return false
}

var turkishMarkers = []string{" nasıl ", " nedir", " için ", " ile ", " neden ", " değil", " yapılır"}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func removeValue(xs []string, vs ...string) []string {
	out := xs[:0]
	for _, x := range xs {
		keep := true
		for _, v := range vs {
			if x == v {
				keep = false
			}
		}
		if keep {
			out = append(out, x)
		}
	}
	return out
}
