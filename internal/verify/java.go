// Package verify statically checks robot code against the pinned season's
// API symbol tables (docs/mcp-surface.md frc_verify_code).
//
// It is deliberately conservative: false positives cost more than misses.
//
//   - error:   the symbol is absent from the pinned season but present in
//     another season (wrong_season) — the core "wrong season's API" signal;
//   - warning: the symbol exists but is deprecated in the pinned season;
//   - info:    the symbol could not be resolved (team code, inherited member,
//     or a library the index does not cover yet).
//
// MVP scope (Java): imports (single, wildcard, static), fully-qualified
// references, and member calls on receivers whose type is declared in the same
// file or on imported types (static calls). Inherited members are not
// resolved, so a missing member is never an error unless another season
// declares it on the same type.
package verify

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// Resolver answers exact symbol questions (retrieve.Engine in production).
type Resolver interface {
	Symbols(ctx context.Context, q index.SymbolQuery) ([]index.Symbol, error)
	PackageExists(ctx context.Context, pkg, season, language string) bool
	SymbolsReplacedBy(ctx context.Context, fqn, season string) []index.Symbol
}

// Finding is one verifier result.
type Finding struct {
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Symbol    string `json:"symbol"`
	Severity  string `json:"severity"` // error | warning | info
	Kind      string `json:"kind"`     // wrong_season | deprecated | unknown | not_covered
	Message   string `json:"message"`
	Fix       string `json:"fix,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
}

// Result is a verification report.
type Result struct {
	Findings []Finding         `json:"findings"`
	Coverage map[string]string `json:"coverage" jsonschema:"what was checked per library: full | partial | none"`
	Checked  int               `json:"checked" jsonschema:"number of API references checked"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
}

// covered are the package roots the index has symbol tables for.
var covered = []string{"edu.wpi.first.", "org.wpilib."}

// vendorRoots map vendor packages to library names for coverage reporting.
var vendorRoots = map[string]string{
	"com.ctre.": "phoenix6", "com.revrobotics.": "revlib", "org.photonvision.": "photonvision",
	"com.pathplanner.": "pathplannerlib", "choreo.": "choreolib", "org.littletonrobotics.": "advantagekit",
	"swervelib.": "yagsl", "com.studica.": "studica", "com.reduxrobotics.": "reduxlib", "au.grapplerobotics.": "grapple",
	"com.thethriftybot.": "thriftylib", "org.ironmaple.": "maple-sim", "yams.": "yams", "dev.doglog.": "doglog",
	"frc.robot.": "", "limelight.": "limelight",
}

var (
	importRe  = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+(static[ \t]+)?([A-Za-z_][\w.]*?)(\.\*)?[ \t]*;`)
	fqnRe     = regexp.MustCompile(`\b((?:edu\.wpi\.first|org\.wpilib)(?:\.[a-z_]\w*)*(?:\.[A-Z]\w*)+)`)
	declRe    = regexp.MustCompile(`\b([A-Z]\w*)(?:\.[A-Z]\w*)*(?:<[^;(){}=]*>)?(?:\[\])*[ \t]+([a-zA-Z_]\w*)[ \t]*(?:=|;|,|\)|:)`)
	callRe    = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\.\s*([a-z_]\w*)\s*\(`)
	packageRe = regexp.MustCompile(`(?m)^[ \t]*package[ \t]+([\w.]+)[ \t]*;`)
)

// Java verifies Java source against the pinned season.
func Java(ctx context.Context, r Resolver, code, season string) Result {
	src := stripJava(code)
	res := Result{Coverage: map[string]string{"wpilib": "partial (imports, types, members on declared/imported receivers)"}}
	lines := lineIndex(src)
	seen := map[string]bool{}
	imported := map[string]string{} // simple name → FQN (covered types only)
	pkgOwn := ""
	if m := packageRe.FindStringSubmatch(src); m != nil {
		pkgOwn = m[1]
	}

	add := func(off int, f Finding) {
		f.Line, f.Col = pos(lines, off)
		res.Findings = append(res.Findings, f)
	}

	for _, m := range importRe.FindAllStringSubmatchIndex(src, -1) {
		static := m[2] >= 0
		name := src[m[4]:m[5]]
		wildcard := m[6] >= 0
		if !isCovered(name) {
			if lib := vendorLib(name); lib != "" && res.Coverage[lib] == "" {
				res.Coverage[lib] = "none (not indexed yet)"
			}
			continue
		}
		res.Checked++
		switch {
		case wildcard && !static:
			checkPackage(ctx, r, name, season, func(f Finding) { add(m[4], f) })
		case static:
			owner, member := name, ""
			if !wildcard {
				if i := strings.LastIndexByte(name, '.'); i > 0 {
					owner, member = name[:i], name[i+1:]
				}
			}
			ok := checkType(ctx, r, owner, season, func(f Finding) { add(m[4], f) })
			if ok && member != "" {
				checkMember(ctx, r, owner, member, season, func(f Finding) { add(m[4], f) })
			}
		default:
			if checkType(ctx, r, name, season, func(f Finding) { add(m[4], f) }) {
				imported[simple(name)] = name
			}
			seen[name] = true
		}
	}

	// Fully-qualified references in the body (not already imported).
	for _, m := range fqnRe.FindAllStringSubmatchIndex(src, -1) {
		name := trimToType(src[m[2]:m[3]])
		if seen[name] || strings.HasPrefix(name, pkgOwn+".") && pkgOwn != "" {
			continue
		}
		if lineHasImport(src, m[2]) || lineHasPackage(src, m[2]) {
			continue
		}
		seen[name] = true
		res.Checked++
		checkType(ctx, r, name, season, func(f Finding) { add(m[2], f) })
	}

	// Receivers: variables declared with an imported type; static calls on
	// imported types.
	vars := map[string]string{}
	for _, m := range declRe.FindAllStringSubmatch(src, -1) {
		if fqn, ok := imported[m[1]]; ok {
			vars[m[2]] = fqn
		}
	}
	checkedCalls := map[string]bool{}
	for _, m := range callRe.FindAllStringSubmatchIndex(src, -1) {
		recv, member := src[m[2]:m[3]], src[m[4]:m[5]]
		fqn := vars[recv]
		if fqn == "" {
			fqn = imported[recv]
		}
		if fqn == "" || checkedCalls[fqn+"#"+member] {
			continue
		}
		checkedCalls[fqn+"#"+member] = true
		res.Checked++
		checkMember(ctx, r, fqn, member, season, func(f Finding) { add(m[4], f) })
	}

	sort.SliceStable(res.Findings, func(i, j int) bool {
		if res.Findings[i].Line != res.Findings[j].Line {
			return res.Findings[i].Line < res.Findings[j].Line
		}
		return res.Findings[i].Col < res.Findings[j].Col
	})
	for _, f := range res.Findings {
		switch f.Severity {
		case "error":
			res.Errors++
		case "warning":
			res.Warnings++
		}
	}
	if res.Findings == nil {
		res.Findings = []Finding{}
	}
	return res
}

func exact(ctx context.Context, r Resolver, fqn, season string) []index.Symbol {
	s, _ := r.Symbols(ctx, index.SymbolQuery{Name: fqn, Season: season, Language: "java", Limit: 5, Exact: true})
	return s
}

// checkType reports on a covered type reference; it returns true when the
// type exists in the pinned season.
func checkType(ctx context.Context, r Resolver, fqn, season string, emit func(Finding)) bool {
	if pinned := exact(ctx, r, fqn, season); len(pinned) > 0 {
		s := pinned[0]
		if s.DeprecatedIn != "" {
			emit(deprecated(s))
		}
		return true
	}
	other := exact(ctx, r, fqn, "")
	if len(other) > 0 {
		s := other[0]
		f := Finding{Symbol: fqn, Kind: "wrong_season", SourceURL: s.SourceURL,
			Message: fmt.Sprintf("%s does not exist in the %s API; it is from the %s API (WPILib %s)", fqn, season, s.Season, s.Version)}
		// Only a known move (a pinned-season counterpart) is certain enough
		// for an error. Without one, the class may legitimately come from
		// another library sharing the namespace (e.g. SleipnirJava ships
		// org.wpilib.math.* for 2026 while WPILib 2027 absorbed it).
		if repl := replacementFor(ctx, r, s, fqn, season); repl != "" {
			f.Severity, f.Fix = "error", "use "+repl
			f.Message += fmt.Sprintf("; in %s use %s", season, repl)
		} else {
			f.Severity = "warning"
			f.Message += fmt.Sprintf("; there is no %s equivalent in WPILib (ignore if it comes from another library)", season)
		}
		emit(f)
		return false
	}
	// Unknown everywhere: likely a nested/generated type or a typo.
	if pkg := packageOf(fqn); pkg != "" && !r.PackageExists(ctx, pkg, season, "java") && r.PackageExists(ctx, pkg, "", "java") {
		emit(Finding{Symbol: fqn, Severity: packageSeverity(pkg, season), Kind: "wrong_season",
			Message: fmt.Sprintf("package %s does not exist in the %s API (it exists in another season)", pkg, season)})
		return false
	}
	emit(Finding{Symbol: fqn, Severity: "info", Kind: "unknown",
		Message: fmt.Sprintf("%s was not found in the indexed WPILib %s API (typo, generated class, or not indexed)", fqn, season)})
	return false
}

func checkPackage(ctx context.Context, r Resolver, pkg, season string, emit func(Finding)) {
	if r.PackageExists(ctx, pkg, season, "java") {
		return
	}
	if r.PackageExists(ctx, pkg, "", "java") {
		f := Finding{Symbol: pkg + ".*", Severity: packageSeverity(pkg, season), Kind: "wrong_season",
			Message: fmt.Sprintf("package %s does not exist in the %s API (it exists in another season)", pkg, season)}
		if strings.HasPrefix(pkg, "edu.wpi.first.") && season >= "2027" {
			f.Fix = "2027 moved edu.wpi.first.* to org.wpilib.*; import the specific classes (see frc_api)"
		}
		emit(f)
		return
	}
	emit(Finding{Symbol: pkg + ".*", Severity: "info", Kind: "unknown", Message: fmt.Sprintf("package %s is not in the indexed API", pkg)})
}

func checkMember(ctx context.Context, r Resolver, typeFQN, member, season string, emit func(Finding)) {
	fqn := typeFQN + "#" + member
	if pinned := exact(ctx, r, fqn, season); len(pinned) > 0 {
		// Overloads: warn only if every overload is deprecated.
		for _, s := range pinned {
			if s.DeprecatedIn == "" {
				return
			}
		}
		emit(deprecated(pinned[0]))
		return
	}
	// Not declared on the type itself: walk the pinned season's supertypes.
	// Inherited members are fine; if any ancestor cannot be resolved we cannot
	// prove absence, so we stay silent (false positives cost more than misses).
	found, complete := inHierarchy(ctx, r, typeFQN, member, season, 0, map[string]bool{})
	if found || !complete {
		return
	}
	// Absent from the whole resolved hierarchy: if another season declares it
	// on the counterpart type, it was removed or renamed.
	others := exact(ctx, r, fqn, "")
	for _, cp := range counterparts(ctx, r, typeFQN, season) {
		others = append(others, exact(ctx, r, cp+"#"+member, "")...)
	}
	for _, s := range others {
		if s.Season == season {
			continue
		}
		emit(Finding{Symbol: typeFQN + "." + member + "()", Severity: "error", Kind: "wrong_season", SourceURL: s.SourceURL,
			Message: fmt.Sprintf("%s.%s is declared in the %s API but not in %s (removed or renamed)", simple(typeFQN), member, s.Season, season),
			Fix:     "look up " + simple(typeFQN) + " with frc_api for the " + season + " method"})
		return
	}
}

var superRe = regexp.MustCompile(`\b(?:extends|implements)\s+([^{]+)`)

// inHierarchy reports whether member is declared on typeFQN or any resolved
// supertype in season. complete=false means some supertype (outside the
// index, e.g. java.lang or a vendor class) could not be checked.
func inHierarchy(ctx context.Context, r Resolver, typeFQN, member, season string, depth int, seen map[string]bool) (found, complete bool) {
	if depth > 8 || seen[typeFQN] {
		return false, true
	}
	seen[typeFQN] = true
	if len(exact(ctx, r, typeFQN+"#"+member, season)) > 0 {
		return true, true
	}
	ts := exact(ctx, r, typeFQN, season)
	if len(ts) == 0 {
		return false, false
	}
	complete = true
	for _, sup := range supertypes(ts[0].Signature) {
		if sup == "Object" || sup == "Enum" || sup == "Record" {
			continue // java.lang members (equals, hashCode, name, …): never flagged by callers
		}
		supFQN := resolveType(ctx, r, sup, packageOf(typeFQN), season)
		if supFQN == "" {
			complete = false
			continue
		}
		f, c := inHierarchy(ctx, r, supFQN, member, season, depth+1, seen)
		if f {
			return true, true
		}
		complete = complete && c
	}
	return false, complete
}

// supertypes extracts simple names from "... extends A<T> implements B, C<X>".
func supertypes(sig string) []string {
	var out []string
	for _, m := range superRe.FindAllStringSubmatch(sig, -1) {
		depth := 0
		var cur strings.Builder
		flush := func() {
			if n := strings.TrimSpace(cur.String()); n != "" {
				if i := strings.LastIndexByte(n, '.'); i >= 0 {
					n = n[i+1:]
				}
				out = append(out, strings.Fields(n)[0])
			}
			cur.Reset()
		}
		for _, c := range m[1] {
			switch {
			case c == '<':
				depth++
			case c == '>':
				depth--
			case depth > 0:
			case c == ',':
				flush()
			default:
				if strings.HasPrefix(strings.TrimSpace(cur.String())+string(c), "implements") && depth == 0 {
					cur.Reset()
					continue
				}
				cur.WriteRune(c)
			}
		}
		flush()
	}
	// "extends A implements B" is matched once by the regex; split words.
	var res []string
	for _, n := range out {
		for _, w := range strings.Fields(n) {
			if w != "implements" && w != "extends" {
				res = append(res, w)
			}
		}
	}
	return res
}

// resolveType finds a supertype's FQN: same package first, then a unique
// simple-name match in the season.
func resolveType(ctx context.Context, r Resolver, simpleName, pkg, season string) string {
	if pkg != "" {
		if s := exact(ctx, r, pkg+"."+simpleName, season); len(s) > 0 {
			return s[0].FQN
		}
	}
	cands, _ := r.Symbols(ctx, index.SymbolQuery{Name: simpleName, Season: season, Language: "java", Limit: 10})
	var types []string
	for _, c := range cands {
		if !strings.Contains(c.FQN, "#") && simple(c.FQN) == simpleName {
			types = append(types, c.FQN)
		}
	}
	if len(types) == 1 {
		return types[0]
	}
	return ""
}

// counterparts maps a pinned-season type to the same type in other seasons
// via the generated migration map (both directions).
func counterparts(ctx context.Context, r Resolver, typeFQN, season string) []string {
	var out []string
	for _, s := range r.SymbolsReplacedBy(ctx, typeFQN, "") { // older FQN → typeFQN
		out = append(out, s.FQN)
	}
	for _, s := range exact(ctx, r, typeFQN, season) {
		if s.Replacement != "" && !strings.Contains(s.Replacement, "(") {
			out = append(out, s.Replacement)
		}
	}
	return out
}

// replacementFor finds the pinned-season counterpart of a symbol from another
// season: forward via its Replacement, or backward via symbols replaced by it.
func replacementFor(ctx context.Context, r Resolver, s index.Symbol, fqn, season string) string {
	if s.Replacement != "" && len(exact(ctx, r, s.Replacement, season)) > 0 {
		return s.Replacement
	}
	for _, old := range r.SymbolsReplacedBy(ctx, fqn, season) {
		return old.FQN
	}
	return ""
}

// packageSeverity: edu.wpi.first.* is WPILib's own namespace, so using it in
// a 2027+ project is certainly wrong. org.wpilib.* in an older project may be
// a third-party library (SleipnirJava), so it is only a warning.
func packageSeverity(pkg, season string) string {
	if strings.HasPrefix(pkg+".", "edu.wpi.first.") && season >= "2027" {
		return "error"
	}
	return "warning"
}

func deprecated(s index.Symbol) Finding {
	msg := fmt.Sprintf("%s is deprecated (since %s)", s.FQN, s.DeprecatedIn)
	if strings.HasPrefix(s.Summary, "Deprecated for removal") {
		msg = fmt.Sprintf("%s is deprecated for removal (since %s)", s.FQN, s.DeprecatedIn)
	}
	f := Finding{Symbol: s.FQN, Severity: "warning", Kind: "deprecated", Message: msg, SourceURL: s.SourceURL}
	if s.Replacement != "" {
		f.Fix = "use " + s.Replacement
	}
	return f
}

// ---- lexical helpers ----

// stripJava blanks comments, string/char literals and text blocks while
// preserving offsets and newlines, so positions map back to the source.
func stripJava(s string) string {
	b := []byte(s)
	blank := func(i, j int) {
		for k := i; k < j && k < len(b); k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for i := 0; i < len(b); {
		switch {
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := i + 2
			for j+1 < len(b) && (b[j] != '*' || b[j+1] != '/') {
				j++
			}
			blank(i, j+2)
			i = j + 2
		case b[i] == '"' && i+2 < len(b) && b[i+1] == '"' && b[i+2] == '"':
			j := i + 3
			for j+2 < len(b) && (b[j] != '"' || b[j+1] != '"' || b[j+2] != '"') {
				j++
			}
			blank(i+1, j+2)
			i = j + 3
		case b[i] == '"' || b[i] == '\'':
			q := b[i]
			j := i + 1
			for j < len(b) && b[j] != q && b[j] != '\n' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			blank(i+1, j)
			i = j + 1
		default:
			i++
		}
	}
	return string(b)
}

func lineIndex(s string) []int {
	idx := []int{0}
	for i, c := range s {
		if c == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

func pos(lines []int, off int) (int, int) {
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > off }) - 1
	return i + 1, off - lines[i] + 1
}

func lineHasImport(src string, off int) bool {
	start := strings.LastIndexByte(src[:off], '\n') + 1
	return strings.HasPrefix(strings.TrimSpace(src[start:off]), "import")
}

func lineHasPackage(src string, off int) bool {
	start := strings.LastIndexByte(src[:off], '\n') + 1
	return strings.HasPrefix(strings.TrimSpace(src[start:off]), "package")
}

func isCovered(name string) bool {
	for _, p := range covered {
		if strings.HasPrefix(name+".", p) {
			return true
		}
	}
	return false
}

func vendorLib(name string) string {
	for p, lib := range vendorRoots {
		if strings.HasPrefix(name+".", p) {
			return lib
		}
	}
	return ""
}

// trimToType drops trailing lower-case segments (members/fields) from a
// qualified reference: a.b.C.D.member → a.b.C.D.
func trimToType(name string) string {
	parts := strings.Split(name, ".")
	for len(parts) > 0 {
		last := parts[len(parts)-1]
		if last != "" && last[0] >= 'A' && last[0] <= 'Z' {
			break
		}
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}

func packageOf(fqn string) string {
	parts := strings.Split(fqn, ".")
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			return strings.Join(parts[:i], ".")
		}
	}
	return ""
}

func simple(fqn string) string {
	if i := strings.LastIndexByte(fqn, '.'); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}
