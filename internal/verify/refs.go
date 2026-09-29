package verify

import (
	"regexp"
	"sort"
	"strings"
)

// Ref is one API reference found in source code: a type ("pkg.Type",
// "ns::Type", "module.Type") or a member ("<type>#member").
type Ref struct {
	Symbol string `json:"symbol"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	// Member is set for "<type>#member" references.
	Member bool `json:"member,omitempty"`
}

// Refs extracts the API references of one source file, deduplicated by
// symbol (first occurrence wins), in source order. It is lexical and
// conservative: only fully qualified names, imported names and receivers
// whose type is visible in the same file are resolved.
func Refs(code, language string) []Ref {
	switch language {
	case "java":
		return javaRefs(code)
	case "python":
		return pythonRefs(code)
	case "cpp":
		return cppRefs(code)
	}
	return nil
}

// DetectLanguage guesses java, cpp or python from source text ("" if unsure).
func DetectLanguage(code string) string {
	switch {
	case javaHint.MatchString(code):
		return "java"
	case cppHint.MatchString(code):
		return "cpp"
	case pyHint.MatchString(code):
		return "python"
	}
	return ""
}

var (
	javaHint = regexp.MustCompile(`(?m)^[ \t]*(?:import[ \t]+[\w.]+(?:\.\*)?[ \t]*;|package[ \t]+[\w.]+;|public[ \t]+(?:final[ \t]+)?class\b)`)
	cppHint  = regexp.MustCompile(`(?m)^[ \t]*#include\b|\b(?:frc2?|wpi|ctre|rev|units)::\w`)
	pyHint   = regexp.MustCompile(`(?m)^[ \t]*(?:from[ \t]+[\w.]+[ \t]+import\b|import[ \t]+[\w.]+[ \t]*$|def[ \t]+\w+\(|class[ \t]+\w+(?:\([\w., ]*\))?:)`)
)

type refSet struct {
	lines []int
	seen  map[string]bool
	out   []Ref
}

func newRefSet(src string) *refSet { return &refSet{lines: lineIndex(src), seen: map[string]bool{}} }

func (s *refSet) add(sym string, off int, member bool) {
	if sym == "" || s.seen[sym] {
		return
	}
	s.seen[sym] = true
	l, c := pos(s.lines, off)
	s.out = append(s.out, Ref{Symbol: sym, Line: l, Col: c, Member: member})
}

func (s *refSet) sorted() []Ref {
	sort.SliceStable(s.out, func(i, j int) bool {
		if s.out[i].Line != s.out[j].Line {
			return s.out[i].Line < s.out[j].Line
		}
		return s.out[i].Col < s.out[j].Col
	})
	return s.out
}

// ---- Java ----

// javaQualRe matches qualified references into indexed Java roots.
var javaQualRe = regexp.MustCompile(`\b((?:edu\.wpi\.first|org\.wpilib|com\.ctre\.phoenix6|com\.revrobotics|org\.photonvision|com\.pathplanner\.lib|choreo|org\.littletonrobotics\.junction|swervelib)(?:\.[a-z_]\w*)*(?:\.[A-Z]\w*)+)`)

func javaRefs(code string) []Ref {
	src := stripJava(code)
	rs := newRefSet(src)
	imported := map[string]string{} // simple → FQN
	for _, m := range importRe.FindAllStringSubmatchIndex(src, -1) {
		static, name, wildcard := m[2] >= 0, src[m[4]:m[5]], m[6] >= 0
		if _, lib := vendorRoot(name); lib == "" && !isCovered(name) {
			continue
		}
		switch {
		case wildcard:
			// Package or static-wildcard imports name no single symbol.
		case static:
			if i := strings.LastIndexByte(name, '.'); i > 0 {
				rs.add(name[:i]+"#"+name[i+1:], m[4], true)
			}
		default:
			imported[simple(name)] = name
			rs.add(name, m[4], false)
		}
	}
	for _, m := range javaQualRe.FindAllStringSubmatchIndex(src, -1) {
		if lineHasImport(src, m[2]) || lineHasPackage(src, m[2]) {
			continue
		}
		rs.add(trimToType(src[m[2]:m[3]]), m[2], false)
	}
	vars := declaredVars(src, imported)
	for _, m := range callRe.FindAllStringSubmatchIndex(src, -1) {
		recv, member := src[m[2]:m[3]], src[m[4]:m[5]]
		fqn := vars[recv]
		if fqn == "" {
			fqn = imported[recv]
		}
		if fqn != "" {
			rs.add(fqn+"#"+member, m[4], true)
		}
	}
	return rs.sorted()
}

// ---- Python ----

var (
	pyFromRe   = regexp.MustCompile(`(?m)^[ \t]*from[ \t]+([A-Za-z_][\w.]*)[ \t]+import[ \t]+(\([^)]*\)|[^\n#]+)`)
	pyImportRe = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+([^\n#]+)`)
	pyAssignRe = regexp.MustCompile(`(?m)^[ \t]*((?:self\.)?[A-Za-z_]\w*)[ \t]*(?::[^=\n]+)?=[ \t]*([A-Za-z_][\w.]*)[ \t]*\(`)
	pyCallRe   = regexp.MustCompile(`((?:self\.)?[A-Za-z_]\w*)\.([A-Za-z_]\w*)[ \t]*\(`)
	pyAttrRe   = regexp.MustCompile(`\b([A-Za-z_]\w*)((?:\.[A-Za-z_]\w*)+)`)
)

// pyRoots are the indexed Python distributions' top-level modules.
var pyRoots = map[string]bool{"wpilib": true, "wpimath": true, "wpiutil": true, "ntcore": true, "commands2": true,
	"hal": true, "cscore": true, "robotpy_apriltag": true, "wpinet": true, "phoenix6": true, "rev": true,
	"photonlibpy": true, "pathplannerlib": true, "choreo": true, "wpilib_units": true, "romi": true, "xrp": true}

func pyIndexed(module string) bool {
	root, _, _ := strings.Cut(module, ".")
	return pyRoots[root]
}

func pythonRefs(code string) []Ref {
	src := stripPython(code)
	rs := newRefSet(src)
	names := map[string]string{}   // local name → qualified symbol (from-imports)
	modules := map[string]string{} // alias → module (import x.y as z)
	for _, m := range pyFromRe.FindAllStringSubmatchIndex(src, -1) {
		mod := src[m[2]:m[3]]
		if !pyIndexed(mod) {
			continue
		}
		list := strings.Trim(src[m[4]:m[5]], "() \t")
		for _, item := range strings.Split(list, ",") {
			f := strings.Fields(item)
			if len(f) == 0 || f[0] == "*" {
				continue
			}
			local := f[0]
			if len(f) == 3 && f[1] == "as" {
				local = f[2]
			}
			names[local] = mod + "." + f[0]
			rs.add(mod+"."+f[0], m[2], false)
		}
	}
	for _, m := range pyImportRe.FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			f := strings.Fields(item)
			if len(f) == 0 || !pyIndexed(f[0]) {
				continue
			}
			alias := f[0]
			if len(f) == 3 && f[1] == "as" {
				alias = f[2]
			} else if i := strings.IndexByte(alias, '.'); i > 0 {
				alias = alias[:i] // "import wpimath.geometry" binds "wpimath"
				modules[alias] = alias
				continue
			}
			modules[alias] = f[0]
		}
	}
	// module.attr chains: keep up to the first capitalized segment (the type)
	// and one member after it.
	for _, m := range pyAttrRe.FindAllStringSubmatchIndex(src, -1) {
		head := src[m[2]:m[3]]
		mod, ok := modules[head]
		if !ok || (m[2] > 0 && src[m[2]-1] == '.') {
			continue
		}
		segs := strings.Split(strings.TrimPrefix(src[m[4]:m[5]], "."), ".")
		path := mod
		for i, sg := range segs {
			path += "." + sg
			if sg[0] >= 'A' && sg[0] <= 'Z' {
				rs.add(path, m[2], false)
				if i+1 < len(segs) {
					rs.add(path+"#"+segs[i+1], m[2], true)
				}
				break
			}
		}
	}
	vars, bad := map[string]string{}, map[string]bool{}
	for _, m := range pyAssignRe.FindAllStringSubmatch(src, -1) {
		t := ""
		if q, ok := names[m[2]]; ok {
			t = q
		} else if head, rest, ok := strings.Cut(m[2], "."); ok && modules[head] != "" {
			t = modules[head] + "." + rest
		}
		if prev, seen := vars[m[1]]; seen && prev != t {
			bad[m[1]] = true // reassigned (or self.x in two classes): ambiguous
		}
		vars[m[1]] = t
	}
	for n := range bad {
		delete(vars, n)
	}
	for _, m := range pyCallRe.FindAllStringSubmatchIndex(src, -1) {
		recv, member := src[m[2]:m[3]], src[m[4]:m[5]]
		t := vars[recv]
		if t == "" {
			t = names[recv] // static / class method on an imported class
		}
		if t != "" && !strings.HasPrefix(member, "_") {
			rs.add(t+"#"+member, m[4], true)
		}
	}
	return rs.sorted()
}

// stripPython blanks comments and string literals, preserving offsets.
func stripPython(s string) string {
	b := []byte(s)
	blank := func(i, j int) {
		for k := i; k < j && k < len(b); k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for i := 0; i < len(b); {
		switch c := b[i]; {
		case c == '#':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case (c == '"' || c == '\'') && i+2 < len(b) && b[i+1] == c && b[i+2] == c:
			q := string([]byte{c, c, c})
			end := strings.Index(string(b[i+3:]), q)
			j := len(b)
			if end >= 0 {
				j = i + 3 + end
			}
			blank(i+3, j)
			i = j + 3
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(b) && b[j] != c && b[j] != '\n' {
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

// ---- C++ ----

var cppQualRe = regexp.MustCompile(`\b((?:frc2?|wpi|ctre|rev|photon|pathplanner|choreo|units)(?:::[A-Za-z_]\w*)+)`)

func cppRefs(code string) []Ref {
	src := stripJava(code) // same comment and literal syntax
	rs := newRefSet(src)
	for _, m := range cppQualRe.FindAllStringSubmatchIndex(src, -1) {
		if m[2] > 0 && src[m[2]-1] == ':' {
			continue // tail of a longer qualified name
		}
		segs := strings.Split(src[m[2]:m[3]], "::")
		for i, sg := range segs {
			if sg[0] < 'A' || sg[0] > 'Z' {
				continue
			}
			typ := strings.Join(segs[:i+1], "::")
			rs.add(typ, m[2], false)
			if i+1 < len(segs) {
				rs.add(typ+"#"+segs[i+1], m[2], true)
			}
			break
		}
	}
	return rs.sorted()
}
