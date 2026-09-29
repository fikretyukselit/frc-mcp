// Package pystub extracts Python API symbol tables from the type stubs (.pyi)
// or typed sources (.py) inside a PyPI wheel: RobotPy (wpilib, wpimath,
// commands2, …) and the vendors' Python packages.
//
// RobotPy defines symbols in private modules and re-exports them
// ("wpilib/_wpilib/__init__.pyi" is imported as "wpilib"), so the public
// module path drops every segment that starts with "_". A module with both a
// .pyi and a .py uses the .pyi. The parser is line-based and handles what
// stub generators emit: nested classes, multi-line signatures, @overload,
// deprecation decorators, docstrings and annotated class attributes. Private
// names are skipped; __init__ is the constructor.
package pystub

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

const (
	maxFile        = 4 << 20
	maxSummary     = 320
	maxChunkTokens = 700
)

// Stats summarizes a parse.
type Stats struct{ Modules, Types, Members, Chunks, Deprecated int }

// Parse reads the wheel at whlPath and emits symbols and per-class chunks.
func Parse(whlPath string, src sources.Source, rev string, retrieved time.Time,
	emitSymbol func(index.Symbol) error, emitChunk func(index.Chunk) error) (Stats, error) {
	var st Stats
	zr, err := zip.OpenReader(whlPath)
	if err != nil {
		return st, err
	}
	defer zr.Close()
	// Choose one file per module: .pyi wins over .py.
	chosen := map[string]*zip.File{}
	for _, f := range zr.File {
		name := f.Name
		if strings.Contains(name, ".dist-info/") || strings.Contains(name, "/tests/") || strings.Contains(name, "/test/") ||
			f.UncompressedSize64 > maxFile {
			continue
		}
		ext := path.Ext(name)
		if ext != ".pyi" && ext != ".py" {
			continue
		}
		mod := strings.TrimSuffix(name, ext)
		if prev, ok := chosen[mod]; ok && path.Ext(prev.Name) == ".pyi" {
			continue
		}
		chosen[mod] = f
	}
	mods := make([]string, 0, len(chosen))
	for m := range chosen {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	aliases := reexports(chosen)
	seen := map[string]bool{} // FQN+signature: the same class can be re-declared in several private modules
	for _, m := range mods {
		pub := PublicModule(m)
		if pub == "" || (src.Include != "" && !strings.HasPrefix(pub+".", src.Include)) {
			continue
		}
		rc, err := chosen[m].Open()
		if err != nil {
			return st, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxFile))
		rc.Close()
		if err != nil {
			return st, err
		}
		decls := ParseModule(string(b))
		if len(decls) == 0 {
			continue
		}
		st.Modules++
		url := src.BaseURL
		for _, d := range decls {
			if d.Class == nil {
				continue
			}
			fqn := pub + "." + d.Class.Name
			if top, _, _ := strings.Cut(d.Class.Name, "."); true {
				if pkg := aliases[pub+"."+top]; pkg != "" { // re-exported by a package __init__
					fqn = pkg + "." + d.Class.Name
				}
			}
			if seen[fqn] {
				continue
			}
			seen[fqn] = true
			st.Types++
			chunkID := src.Library + "-python/" + src.Season + "/" + fqn
			mk := func(fqn, kind, sig, doc string, dep string) index.Symbol {
				s := index.Symbol{FQN: fqn, Library: src.Library, Version: src.Version, Season: src.Season, Language: "python",
					Kind: kind, Signature: sig, Summary: firstSentence(doc), SourceURL: url, UpstreamRev: rev,
					RetrievedAt: retrieved, License: src.License, Trust: src.Trust, ChunkID: chunkID + "#0"}
				if dep != "" {
					s.DeprecatedIn = src.Version
					s.Summary = strings.TrimSpace("Deprecated. " + dep + " " + s.Summary)
					st.Deprecated++
				}
				return s
			}
			if err := emitSymbol(mk(fqn, "class", d.Class.Signature, d.Class.Doc, d.Class.Deprecated)); err != nil {
				return st, err
			}
			names := map[string]bool{}
			for _, mem := range d.Class.Members {
				names[mem.Name] = true
			}
			for _, mem := range d.Class.Members {
				sym := mk(fqn+"#"+mem.Name, mem.Kind, mem.Signature, mem.Doc, mem.Deprecated)
				if mem.Use != "" && mem.Use != mem.Name && names[mem.Use] {
					sym.Replacement, sym.ReplacementSrc = fqn+"#"+mem.Use, "upstream"
				}
				if err := emitSymbol(sym); err != nil {
					return st, err
				}
				st.Members++
			}
			for _, c := range classChunks(d.Class, pub, fqn, chunkID, src, url, rev, retrieved) {
				if err := emitChunk(c); err != nil {
					return st, err
				}
				st.Chunks++
			}
		}
		// Module-level functions become symbols of the module.
		for _, d := range decls {
			if d.Func == nil {
				continue
			}
			fqn := pub + "#" + d.Func.Name
			if seen[fqn+d.Func.Signature] {
				continue
			}
			seen[fqn+d.Func.Signature] = true
			s := index.Symbol{FQN: fqn, Library: src.Library, Version: src.Version, Season: src.Season, Language: "python",
				Kind: "function", Signature: d.Func.Signature, Summary: firstSentence(d.Func.Doc), SourceURL: url,
				UpstreamRev: rev, RetrievedAt: retrieved, License: src.License, Trust: src.Trust}
			if err := emitSymbol(s); err != nil {
				return st, err
			}
			st.Members++
		}
	}
	return st, nil
}

// PublicModule maps a wheel path to its import path: "wpilib/_wpilib/__init__"
// → "wpilib", "wpimath/_controls/_controls/trajectory" → "wpimath.trajectory".
// A wheel's "<dist>.data/purelib|platlib/" prefix is dropped. Paths with no
// public segment return "".
func PublicModule(p string) string {
	if i := strings.Index(p, ".data/"); i >= 0 {
		rest := p[i+len(".data/"):]
		if r, ok := strings.CutPrefix(rest, "purelib/"); ok {
			p = r
		} else if r, ok := strings.CutPrefix(rest, "platlib/"); ok {
			p = r
		}
	}
	var out []string
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || strings.HasPrefix(seg, "_") || strings.Contains(seg, "-") {
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, ".")
}

var fromImportRe = regexp.MustCompile(`(?s)from\s+(\.+[\w.]*|[A-Za-z_][\w.]*)\s+import\s+(\([^)]*\)|[^\n]+)`)

// reexports reads every package __init__ and maps "module.Name" to the
// shortest package that re-exports it ("phoenix6.hardware.talon_fx.TalonFX"
// → "phoenix6.hardware"), so symbols carry the import path users write.
func reexports(files map[string]*zip.File) map[string]string {
	out := map[string]string{}
	for mod, f := range files {
		if path.Base(mod) != "__init__" {
			continue
		}
		pkg := PublicModule(mod)
		if pkg == "" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, maxFile))
		rc.Close()
		for _, m := range fromImportRe.FindAllStringSubmatch(string(b), -1) {
			from := m[1]
			switch {
			case strings.HasPrefix(from, "."):
				rel := strings.TrimLeft(from, ".")
				up := len(from) - len(rel) - 1 // "." = this package, ".." = parent
				base := strings.Split(pkg, ".")
				if up > len(base)-1 {
					continue
				}
				base = base[:len(base)-up]
				from = strings.Join(base, ".")
				if rel != "" {
					from += "." + rel
				}
			case !strings.HasPrefix(from+".", strings.SplitN(pkg, ".", 2)[0]+"."):
				continue // a different distribution
			}
			src := PublicModule(strings.ReplaceAll(from, ".", "/"))
			for _, name := range strings.Split(strings.Trim(m[2], "() \t\n"), ",") {
				name = strings.TrimSpace(strings.SplitN(strings.TrimSpace(name), " as ", 2)[0])
				if name == "" || name == "*" || strings.HasPrefix(name, "_") || strings.ContainsAny(name, " #") {
					continue
				}
				key := src + "." + name
				if prev := out[key]; (prev == "" || len(pkg) < len(prev)) && src != pkg {
					out[key] = pkg
				}
			}
		}
	}
	return out
}

// Decl is a top-level class or function.
type Decl struct {
	Class *Class
	Func  *Member
}

// Class is a parsed class with its members and nested classes flattened
// ("Outer.Inner" names).
type Class struct {
	Name, Signature, Doc, Deprecated string
	Members                          []Member
}

// Member is a method, constructor, property or attribute.
type Member struct {
	Name, Kind, Signature, Doc, Deprecated string
	Use                                    string // recommended replacement member (from the docstring)
}

var (
	classRe  = regexp.MustCompile(`^class\s+([A-Za-z_]\w*)\s*(\([^)]*\))?\s*:`)
	defRe    = regexp.MustCompile(`^(async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)
	attrRe   = regexp.MustCompile(`^([A-Za-z]\w*)\s*:\s*(.+?)(\s*=.*)?$`)
	deprRe   = regexp.MustCompile(`^@(?:typing_extensions\.|warnings\.|typing\.)?deprecated\((.*)\)\s*$`)
	quotedRe = regexp.MustCompile(`["']([^"']*)["']`)
	// RobotPy docstrings: ":deprecated: Use GetLeftBumperButton instead. …"
	docDeprRe = regexp.MustCompile(`(?s):deprecated:\s*(.*?)(\n\s*\n|\n\s*:[a-z]+|$)`)
	useRe     = regexp.MustCompile(`\bUse\s+([A-Za-z_]\w*)(?:\(\))?\s+(?:instead|in favor)`)
)

// docDeprecation returns the deprecation text of a docstring and the name it
// recommends, lower-camel-cased as RobotPy exposes it (GetX → getX).
func docDeprecation(doc string) (text, use string) {
	m := docDeprRe.FindStringSubmatch(doc)
	if m == nil {
		return "", ""
	}
	text = strings.Join(strings.Fields(m[1]), " ")
	if text == "" {
		text = "deprecated"
	}
	if u := useRe.FindStringSubmatch(text); u != nil {
		use = strings.ToLower(u[1][:1]) + u[1][1:]
	}
	return text, use
}

type frame struct {
	indent int
	class  *Class
}

// ParseModule parses one .pyi/.py source.
func ParseModule(src string) []Decl {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []Decl
	var stack []frame
	var pendingDep string
	var property bool
	top := func() *frame {
		if len(stack) == 0 {
			return nil
		}
		return &stack[len(stack)-1]
	}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		for len(stack) > 0 && indent <= top().indent {
			stack = stack[:len(stack)-1]
		}
		switch {
		case strings.HasPrefix(t, "@"):
			if m := deprRe.FindStringSubmatch(t); m != nil {
				pendingDep = "deprecated"
				if q := quotedRe.FindStringSubmatch(m[1]); q != nil {
					pendingDep = q[1]
				}
			}
			if t == "@property" {
				property = true
			}
			continue
		case classRe.MatchString(t):
			m := classRe.FindStringSubmatch(t)
			name := m[1]
			f := top()
			// A class outside any class frame is module-level even when it is
			// indented under try/except or if/else (CTRE defines TalonFX in a
			// try block that adds Sendable when wpilib is importable); the
			// first definition wins. Bodies of defs are skipped, so classes
			// local to functions never reach here.
			if strings.HasPrefix(name, "_") {
				i = skipBlock(lines, i, indent)
				pendingDep, property = "", false
				continue
			}
			prefix := ""
			if f != nil {
				prefix = f.class.Name + "." // already qualified ("Outer.Inner")
			}
			c := &Class{Name: prefix + name, Signature: "class " + name + m[2], Deprecated: pendingDep}
			c.Doc, i = docstring(lines, i+1, i)
			out = append(out, Decl{Class: c})
			stack = append(stack, frame{indent: indent, class: c})
			pendingDep, property = "", false
			continue
		case defRe.MatchString(t):
			sig, end := joinSignature(lines, i)
			m := defRe.FindStringSubmatch(t)
			name := m[2]
			doc, next := docstring(lines, end+1, end)
			f := top()
			switch {
			case f == nil:
				if !strings.HasPrefix(name, "_") {
					out = append(out, Decl{Func: &Member{Name: name, Kind: "function", Signature: cleanSig(sig), Doc: doc, Deprecated: pendingDep}})
				}
			case f != nil && indent > f.indent:
				kind := "method"
				switch {
				case name == "__init__":
					kind, name = "constructor", f.class.Name[strings.LastIndexByte(f.class.Name, '.')+1:]
				case property:
					kind = "property"
				case strings.HasPrefix(name, "_"):
					name = ""
				}
				if name != "" {
					mem := Member{Name: name, Kind: kind, Signature: cleanSig(sig), Doc: doc, Deprecated: pendingDep}
					if txt, use := docDeprecation(doc); txt != "" {
						mem.Use = use
						if mem.Deprecated == "" {
							mem.Deprecated = txt
						}
					}
					f.class.Members = append(f.class.Members, mem)
				}
			}
			i = skipBody(lines, next, indent)
			pendingDep, property = "", false
			continue
		}
		// Annotated class attribute: "kDefaultPeriod: typing.ClassVar[float] = 20.0"
		if f := top(); f != nil && indent > f.indent {
			if m := attrRe.FindStringSubmatch(t); m != nil && !strings.HasPrefix(m[1], "_") {
				f.class.Members = append(f.class.Members, Member{Name: m[1], Kind: "field", Signature: m[1] + ": " + m[2]})
			}
		}
		pendingDep, property = "", false
	}
	return out
}

// joinSignature returns the full "def …:" text starting at line i (it may
// span lines until the parentheses balance) and the index of its last line.
func joinSignature(lines []string, i int) (string, int) {
	var b strings.Builder
	depth := 0
	for j := i; j < len(lines) && j < i+60; j++ {
		t := strings.TrimSpace(lines[j])
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
		depth += strings.Count(t, "(") + strings.Count(t, "[") - strings.Count(t, ")") - strings.Count(t, "]")
		if depth <= 0 && (strings.HasSuffix(t, ":") || strings.Contains(t, ": ...") || strings.HasSuffix(t, "...")) {
			return b.String(), j
		}
	}
	return b.String(), i
}

func cleanSig(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "..."), " ")
	s = strings.TrimSuffix(s, ":")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	return strings.Join(strings.Fields(s), " ")
}

// docstring reads a triple-quoted docstring starting at line i (after a def
// or class header ending at hdr). It returns the text and the last line
// consumed (hdr when there is none).
func docstring(lines []string, i, hdr int) (string, int) {
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return "", hdr
	}
	t := strings.TrimSpace(lines[i])
	var q string
	switch {
	case strings.HasPrefix(t, `"""`), strings.HasPrefix(t, `r"""`):
		q = `"""`
	case strings.HasPrefix(t, `'''`):
		q = `'''`
	default:
		return "", hdr
	}
	t = strings.TrimPrefix(strings.TrimPrefix(t, "r"), q)
	if j := strings.Index(t, q); j >= 0 {
		return strings.TrimSpace(t[:j]), i
	}
	parts := []string{t}
	for j := i + 1; j < len(lines); j++ {
		l := strings.TrimSpace(lines[j])
		if k := strings.Index(l, q); k >= 0 {
			parts = append(parts, l[:k])
			return strings.TrimSpace(strings.Join(parts, "\n")), j
		}
		parts = append(parts, l)
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), len(lines) - 1
}

// skipBody advances past the indented body of a def (stubs have none or a
// docstring; .py sources have code).
func skipBody(lines []string, i, indent int) int {
	j := i + 1
	for ; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " \t")) <= indent {
			break
		}
	}
	return j - 1
}

func skipBlock(lines []string, i, indent int) int { return skipBody(lines, i, indent) }

func firstSentence(doc string) string {
	s := strings.Join(strings.Fields(doc), " ")
	for _, stop := range []string{":param", ":returns", ":return", ":type", "Members:"} {
		if k := strings.Index(s, stop); k >= 0 {
			s = strings.TrimSpace(s[:k])
		}
	}
	if k := strings.Index(s, ". "); k >= 0 && k < maxSummary {
		return s[:k+1]
	}
	if len(s) > maxSummary {
		cut := maxSummary
		for cut > 0 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		return strings.TrimSpace(s[:cut]) + " …"
	}
	return s
}

func classChunks(c *Class, pub, fqn, docID string, src sources.Source, url, rev string, retrieved time.Time) []index.Chunk {
	var head strings.Builder
	fmt.Fprintf(&head, "```python\nfrom %s import %s\n%s\n```\n", pub, strings.SplitN(c.Name, ".", 2)[0], c.Signature)
	if c.Deprecated != "" {
		fmt.Fprintf(&head, "\n> **Deprecated** in %s. %s\n", src.Version, c.Deprecated)
	}
	if d := firstSentence(c.Doc); d != "" {
		fmt.Fprintf(&head, "\n%s\n", d)
	}
	mk := func(ord int, body string) index.Chunk {
		return index.Chunk{DocID: docID, Ord: ord, Library: src.Library, VersionLo: src.Version, Season: src.Season,
			Channel: src.Channel, Language: "python", Kind: "api", Title: c.Name + " (class)",
			HeadingPath: "Python API › " + pub, Symbol: fqn, Body: body, SourceURL: url, UpstreamRev: rev,
			RetrievedAt: retrieved, License: src.License, Trust: src.Trust, Authority: 3}
	}
	cur := head.String()
	if len(c.Members) > 0 {
		cur += "\nMembers:\n"
	}
	var out []index.Chunk
	for _, m := range c.Members {
		l := "- `" + m.Signature + "`"
		if m.Deprecated != "" {
			l += " **(deprecated)**"
		}
		if s := firstSentence(m.Doc); s != "" {
			l += " — " + s
		}
		if textutil.EstimateTokens(cur+l) > maxChunkTokens && strings.Contains(cur, "\n- ") {
			out = append(out, mk(len(out), strings.TrimSpace(cur)))
			cur = "```python\n" + c.Signature + "\n```\n\nMembers (continued):\n"
		}
		cur += l + "\n"
	}
	return append(out, mk(len(out), strings.TrimSpace(cur)))
}
