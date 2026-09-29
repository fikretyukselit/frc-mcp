package verify

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// Call-shape checks (M4): a constructor or method call whose arguments fit
// no overload of the pinned season. Only literal arguments are typed ("x",
// 1, 1.0, true, null, 'c'); anything else matches every parameter type, so
// a finding means the call cannot compile, not that it might not.
//
//   - error:   no pinned overload fits, and another season's overload of the
//     same (or counterpart) member does — a signature changed between seasons
//     (Phoenix 6 26.50 removed TalonFX(int, String));
//   - warning: no pinned overload takes that many arguments at all.
//
// Methods are checked only when the pinned type's hierarchy resolves
// completely, so an inherited overload can never be missed.

var newRe = regexp.MustCompile(`\bnew\s+([A-Z][\w.]*)\s*(?:<[^<>;()]*>)?\s*\(`)

// argKind is the static type class of a literal argument.
type argKind uint8

const (
	argOther argKind = iota
	argString
	argInt
	argFloat
	argBool
	argNull
	argChar
)

var (
	intLitRe   = regexp.MustCompile(`^[-+]?(?:0[xX][0-9a-fA-F_]+|0[bB][01_]+|\d[\d_]*)[lL]?$`)
	floatLitRe = regexp.MustCompile(`^[-+]?(?:\d[\d_]*\.\d*|\.\d+|\d[\d_]*)(?:[eE][-+]?\d+)?[fFdD]?$`)
)

func classify(arg string) argKind {
	a := strings.TrimSpace(arg)
	switch {
	case strings.HasPrefix(a, `"`) && strings.HasSuffix(a, `"`) && len(a) >= 2:
		return argString
	case strings.HasPrefix(a, "'") && strings.HasSuffix(a, "'") && len(a) >= 3:
		return argChar
	case a == "true" || a == "false":
		return argBool
	case a == "null":
		return argNull
	case intLitRe.MatchString(a):
		return argInt
	case floatLitRe.MatchString(a) && strings.ContainsAny(a, ".eEfFdD"):
		return argFloat
	}
	return argOther
}

func (k argKind) String() string {
	return [...]string{"expr", "String", "int", "double", "boolean", "null", "char"}[k]
}

// callArgs splits the argument list that starts right after "(" at off in
// src (comments and literal contents already blanked by stripJava). It
// returns nil, false when the parentheses do not balance.
func callArgs(src string, off int) ([]string, bool) {
	depth, generic, start := 0, 0, off
	var out []string
	for i := off; i < len(src); i++ {
		switch src[i] {
		case '<':
			// A type argument list ("HashMap<K, V>()", "c.<K, V>of()") opens
			// right after an identifier or a dot with a type (or ?/>) next;
			// "a < b" has spaces.
			if i > 0 && (isIdentByte(src[i-1]) || src[i-1] == '.') && i+1 < len(src) && (src[i+1] >= 'A' && src[i+1] <= 'Z' || src[i+1] == '?' || src[i+1] == '>') {
				generic++
			}
		case '>':
			if generic > 0 {
				generic--
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if src[i] != ')' {
					return nil, false
				}
				if a := strings.TrimSpace(src[start:i]); a != "" || len(out) > 0 {
					out = append(out, src[start:i])
				}
				return out, true
			}
			depth--
		case ',':
			if depth == 0 && generic == 0 {
				out = append(out, src[start:i])
				start = i + 1
			}
		case ';':
			if depth == 0 {
				return nil, false
			}
		}
	}
	return nil, false
}

// param is one declared parameter type.
type param struct {
	typ     string
	varargs bool
}

var sigParamsRe = regexp.MustCompile(`\(([^()]*)\)\s*(?:throws\b.*)?$`)

// params parses "... name(int a, CANBus b, Command... cmds)" (annotations
// and generics tolerated). ok=false when the signature has no parameter list.
func params(sig string) ([]param, bool) {
	m := sigParamsRe.FindStringSubmatch(strings.TrimSpace(sig))
	if m == nil {
		return nil, false
	}
	var out []param
	for _, p := range splitTop(m[1]) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f := strings.Fields(p)
		for len(f) > 0 && (strings.HasPrefix(f[0], "@") || f[0] == "final") {
			f = f[1:]
		}
		if len(f) == 0 {
			continue
		}
		t := strings.Join(f[:max(len(f)-1, 1)], " ")
		va := strings.HasSuffix(t, "...")
		t = strings.TrimSuffix(t, "...")
		if i := strings.IndexByte(t, '<'); i >= 0 {
			t = t[:i]
		}
		out = append(out, param{typ: strings.TrimSpace(t), varargs: va})
	}
	return out, true
}

// accepts reports whether a literal kind can be passed as type t.
func accepts(t string, k argKind) bool {
	t = strings.TrimPrefix(strings.TrimSuffix(t, "[]"), "java.lang.")
	if k == argOther || t == "Object" || isTypeVar(t) {
		return true
	}
	switch k {
	case argString:
		return t == "String" || t == "CharSequence" || t == "Comparable" || t == "Serializable"
	case argInt:
		switch t {
		case "int", "long", "double", "float", "short", "byte", "char", "Integer", "Long", "Double", "Float", "Short", "Number":
			return true
		}
	case argFloat:
		switch t {
		case "double", "float", "Double", "Float", "Number":
			return true
		}
	case argBool:
		return t == "boolean" || t == "Boolean"
	case argChar:
		switch t {
		case "char", "Character", "int", "long", "double", "float":
			return true
		}
	case argNull:
		switch t {
		case "int", "long", "double", "float", "short", "byte", "char", "boolean":
			return false
		}
		return true
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isTypeVar(t string) bool {
	return len(t) <= 2 && t != "" && t[0] >= 'A' && t[0] <= 'Z'
}

// fits reports whether args can call an overload with these parameters.
func fits(ps []param, kinds []argKind) bool {
	n := len(ps)
	va := n > 0 && ps[n-1].varargs
	if (!va && len(kinds) != n) || (va && len(kinds) < n-1) {
		return false
	}
	for i, k := range kinds {
		p := ps[min(i, n-1)]
		if i >= n-1 && va {
			// A single array argument or an element per slot.
			if !accepts(p.typ, k) && (i != n-1 || len(kinds) != n || k != argNull) {
				return false
			}
			continue
		}
		if !accepts(p.typ, k) {
			return false
		}
	}
	return true
}

// overloadsFit: whether any signature fits, and whether any takes len(kinds)
// arguments at all (or varargs).
func overloadsFit(syms []index.Symbol, kinds []argKind) (fit, arity bool, parsed int) {
	for _, s := range syms {
		ps, ok := params(s.Signature)
		if !ok {
			continue
		}
		parsed++
		if fits(ps, kinds) {
			return true, true, parsed
		}
		n := len(ps)
		if len(kinds) == n || (n > 0 && ps[n-1].varargs && len(kinds) >= n-1) {
			arity = true
		}
	}
	return false, arity, parsed
}

func describe(kinds []argKind) string {
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = k.String()
	}
	return strings.Join(parts, ", ")
}

// checkCall reports a call whose literal arguments fit no pinned overload of
// fqn ("Type#member", or "Type#Type" for a constructor). pinned are the
// pinned-season overloads (all of the hierarchy for methods).
func checkCall(ctx context.Context, r Resolver, typeFQN, member string, pinned []index.Symbol, kinds []argKind, season string, emit func(Finding)) {
	fit, arity, parsed := overloadsFit(pinned, kinds)
	if fit || parsed == 0 || parsed < len(pinned) {
		return // fits, or some overload's signature could not be read
	}
	call := simple(typeFQN) + "." + member
	if member == simple(typeFQN) {
		call = "new " + simple(typeFQN)
	}
	var sigs []string
	for i, s := range pinned {
		if i == 3 {
			sigs = append(sigs, "…")
			break
		}
		sigs = append(sigs, strings.TrimSpace(strings.TrimPrefix(s.Signature, "public ")))
	}
	// Did another season accept this call?
	var others []index.Symbol
	for _, s := range exact(ctx, r, typeFQN+"#"+member, "", "java") {
		if s.Season != season {
			others = append(others, s)
		}
	}
	for _, cp := range counterparts(ctx, r, typeFQN, season, "java") {
		cpMember := member
		if member == simple(typeFQN) {
			cpMember = simple(cp)
		}
		for _, s := range exact(ctx, r, cp+"#"+cpMember, "", "java") {
			if s.Season != season {
				others = append(others, s)
			}
		}
	}
	if ok, _, _ := overloadsFit(others, kinds); ok {
		f := Finding{Symbol: typeFQN + "#" + member, Severity: "error", Kind: "wrong_season", SourceURL: pinned[0].SourceURL,
			Message: fmt.Sprintf("%s(%s) does not compile against the %s API: no overload takes these arguments (another season's does)",
				call, describe(kinds), season),
			Fix: season + " overloads: " + strings.Join(sigs, " | ")}
		if s := pinned[0]; s.Replacement != "" && s.ReplacementSrc == "curated" {
			f.Fix += " (see frc_migrate)"
		}
		emit(f)
		return
	}
	if !arity {
		emit(Finding{Symbol: typeFQN + "#" + member, Severity: "warning", Kind: "signature", SourceURL: pinned[0].SourceURL,
			Message: fmt.Sprintf("%s: no %s overload takes %d argument(s)", call, season, len(kinds)),
			Fix:     season + " overloads: " + strings.Join(sigs, " | ")})
	}
}

// hierarchyOverloads collects every pinned-season overload of member on
// typeFQN and its resolved supertypes; complete=false when some supertype
// is outside the index (an overload there could fit).
func hierarchyOverloads(ctx context.Context, r Resolver, typeFQN, member, season string, depth int, seen map[string]bool) (out []index.Symbol, complete bool) {
	if depth > 8 || seen[typeFQN] {
		return nil, true
	}
	seen[typeFQN] = true
	out = exact(ctx, r, typeFQN+"#"+member, season, "java")
	ts := exact(ctx, r, typeFQN, season, "java")
	if len(ts) == 0 {
		return out, false
	}
	complete = true
	for _, sup := range supertypesOf(ts[0].Signature, "java") {
		if sup == "Object" || sup == "Enum" || sup == "Record" {
			continue
		}
		supFQN := resolveType(ctx, r, sup, packageOf(typeFQN), season, "java")
		if supFQN == "" {
			complete = false
			continue
		}
		o, c := hierarchyOverloads(ctx, r, supFQN, member, season, depth+1, seen)
		out = append(out, o...)
		complete = complete && c
	}
	return out, complete
}

func kindsOf(args []string) []argKind {
	out := make([]argKind, len(args))
	for i, a := range args {
		out[i] = classify(a)
	}
	return out
}
