package cppheader

import (
	"regexp"
	"strings"
)

// This is a declaration scanner, not a C++ compiler front end. It walks the
// token stream of one header and records the public API a user can name:
// namespaces, classes/structs/unions (with their bases), enums and their
// enumerators, type aliases, functions and variables at namespace scope, and
// the public and protected members of classes. Function bodies, initializers
// and template specializations are skipped by bracket matching. When a
// construct is not understood the scanner skips to the end of the statement,
// so one odd declaration costs that declaration only.

// typeDecl is a class, struct, union or enum (or an alias at namespace scope).
type typeDecl struct {
	fqn     string
	kind    string   // class | struct | union | enum | type
	scope   []string // enclosing scope segments, for base-name lookup
	bases   []string // as written, template arguments stripped
	sig     string   // alias / enum signature ("" for classes: built from bases)
	doc     string
	dep     string
	file    string
	members []memberDecl
	pageFQN string // type whose Doxygen page documents this one ("" = itself)
}

type memberDecl struct {
	name, kind, sig, doc, dep string
}

// nsDecl is a function or variable at namespace scope.
type nsDecl struct {
	fqn, kind, sig, doc, dep, file string
}

type parser struct {
	t     []tok
	i     int
	file  string
	want  func(path []string) (emit, descend bool)
	types []*typeDecl
	funcs []nsDecl
	// namespaces lists every wanted namespace opened in the header (a
	// "namespace a::b {" opens a and a::b).
	namespaces []string
	// tmplDoc carries the documentation of a "template <…>" prefix to the
	// declaration it introduces.
	tmplDoc string
}

type scope struct {
	path   []string  // namespace + enclosing class segments
	cls    *typeDecl // nil at namespace scope
	public bool      // current access is public or protected
	hidden bool      // inside a private/excluded region: parse, never emit
}

func (p *parser) peek() tok {
	if p.i < len(p.t) {
		return p.t[p.i]
	}
	return tok{k: 'e'}
}

func (p *parser) next() tok {
	if p.i+1 < len(p.t) {
		return p.t[p.i+1]
	}
	return tok{k: 'e'}
}

func (p *parser) eof() bool { return p.i >= len(p.t) }

// parseScope consumes declarations until the closing brace of the scope (which
// it consumes) or the end of input.
func (p *parser) parseScope(sc scope) {
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is("}"):
			p.i++
			return
		case t.is(";"):
			p.i++
		case t.is("inline") && p.next().is("namespace"):
			p.i++
			p.namespace(sc, true)
		case t.is("namespace"):
			p.namespace(sc, false)
		case t.is("extern") && p.next().k == 's':
			p.i += 2
			if p.peek().is("{") {
				p.i++
				p.parseScope(sc) // extern "C" { … } is transparent
			}
		case t.is("extern") && p.next().is("template"):
			p.skipStatement()
		case t.is("template"):
			p.template(sc)
		case sc.cls != nil && (t.is("public") || t.is("protected") || t.is("private")) && p.next().is(":"):
			sc.public = !t.is("private")
			p.i += 2
		case t.is("using"):
			p.using(sc)
		case t.is("typedef"):
			p.typedef(sc)
		case t.is("friend") || t.is("static_assert") || t.is("concept"):
			p.skipStatement()
		case t.is("enum"):
			p.enum(sc)
		case t.is("class") || t.is("struct") || t.is("union"):
			p.class(sc)
		case t.k == 'i' && p.next().is("(") && p.statementMacro():
			// A function-like macro used as a statement (CTRE_PID_FF_UNIT_ADD(…)),
			// often without a trailing semicolon.
			p.i++
			p.skipGroup()
			if p.peek().is(";") {
				p.i++
			}
		default:
			p.declaration(sc)
		}
		p.tmplDoc = ""
	}
}

var macroRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)

// statementMacro reports whether the current token starts a function-like
// macro used as a statement: an all-caps identifier that is not an
// attribute-like marker (export, deprecation, nodiscard), whose argument
// list is followed by a new line that does not continue a declaration. An
// all-caps constructor ("PID(double p);") is followed by ';' and is kept.
func (p *parser) statementMacro() bool {
	s := p.peek().s
	if !macroRe.MatchString(s) {
		return false
	}
	for _, w := range []string{"DEPRECATED", "EXPORT", "ATTR", "NODISCARD", "API", "INLINE", "CONSTEXPR", "PACKED", "DLL"} {
		if strings.Contains(s, w) {
			return false
		}
	}
	save := p.i
	p.i++
	p.skipGroup()
	closeLine := p.t[p.i-1].line
	next := p.peek()
	p.i = save
	if next.k == 'e' || next.is("}") {
		return true
	}
	return next.line > closeLine && !next.is(";") && !next.is("{") && !next.is(":") && !next.is("=") && !qualifierWords[next.s]
}

func (p *parser) namespace(sc scope, inline bool) {
	p.i++ // namespace
	var name []string
	for {
		t := p.peek()
		if t.is("inline") {
			p.i++
			continue
		}
		if t.k == 'i' {
			name = append(name, t.s)
			p.i++
			if p.peek().is("::") {
				p.i++
				continue
			}
		}
		break
	}
	p.skipAttributes()
	if p.peek().is("=") { // namespace alias
		p.skipStatement()
		return
	}
	if !p.peek().is("{") {
		p.skipStatement()
		return
	}
	p.i++
	if len(name) == 0 { // anonymous namespace: internal linkage, not API
		p.skipScope()
		return
	}
	child := sc
	if !inline {
		child.path = append(append([]string(nil), sc.path...), name...)
	}
	child.cls = nil
	child.public = true
	if _, descend := p.want(child.path); !descend {
		p.skipScope()
		return
	}
	for k := len(sc.path) + 1; k <= len(child.path); k++ {
		if emit, _ := p.want(child.path[:k]); emit {
			p.namespaces = append(p.namespaces, strings.Join(child.path[:k], "::"))
		}
	}
	p.parseScope(child)
}

func (p *parser) template(sc scope) {
	doc := p.peek().doc
	p.i++                  // template
	if !p.peek().is("<") { // explicit instantiation
		p.skipStatement()
		return
	}
	p.skipAngles()
	p.skipRequires()
	if p.peek().is("template") { // member template of a class template
		p.template(sc)
		return
	}
	if doc != "" {
		p.tmplDoc = doc
	}
	// Specializations ("template <> struct wpi::Struct<X>") are skipped by
	// class(), which sees the '<' after the name.
	t := p.peek()
	switch {
	case t.is("class") || t.is("struct") || t.is("union"):
		p.class(sc)
	case t.is("using"):
		p.using(sc)
	case t.is("concept") || t.is("friend"):
		p.skipStatement()
	default:
		p.declaration(sc)
	}
}

// skipRequires skips a requires-clause after a template parameter list.
func (p *parser) skipRequires() {
	if !p.peek().is("requires") {
		return
	}
	p.i++
	for !p.eof() {
		// one primary: "!"? ( "(…)" | qualified-name ["<…>"] ["::" name …] )
		for p.peek().is("!") {
			p.i++
		}
		switch t := p.peek(); {
		case t.is("("):
			p.skipGroup()
		case t.k == 'i' || t.is("::"):
			for {
				if p.peek().is("::") {
					p.i++
				}
				if p.peek().k != 'i' {
					break
				}
				p.i++
				if p.peek().is("<") {
					p.skipAngles()
				}
				if !p.peek().is("::") {
					break
				}
			}
		default:
			return
		}
		if (p.peek().is("&") && p.next().is("&")) || (p.peek().is("|") && p.next().is("|")) {
			p.i += 2
			continue
		}
		return
	}
}

func (p *parser) using(sc scope) {
	start := p.i
	doc := p.docAt(start)
	p.i++ // using
	if p.peek().is("namespace") || p.peek().is("enum") {
		p.skipStatement()
		return
	}
	name := p.peek()
	if name.k != 'i' || !p.next().is("=") && !p.next().is("[") {
		p.skipStatement() // using Base::member;
		return
	}
	p.i++
	dep := p.skipAttributes()
	if !p.peek().is("=") {
		p.skipStatement()
		return
	}
	p.i++
	valStart := p.i
	p.skipToSemicolon()
	valEnd := p.i - 1 // the ';'
	if valEnd < valStart {
		valEnd = valStart
	}
	sig := "using " + name.s + " = " + render(p.t[valStart:valEnd])
	p.addAlias(sc, name.s, sig, doc, dep)
}

func (p *parser) typedef(sc scope) {
	start := p.i
	doc := p.docAt(start)
	p.i++
	body := p.i
	p.skipToSemicolon()
	end := p.i - 1
	if end <= body {
		return
	}
	ts := p.t[body:end]
	name := ""
	for j := 0; j+2 < len(ts); j++ { // function pointer: ( * Name )
		if ts[j].is("(") && ts[j+1].is("*") && ts[j+2].k == 'i' {
			name = ts[j+2].s
			break
		}
	}
	if name == "" {
		depth := 0
		for j := len(ts) - 1; j >= 0; j-- {
			switch {
			case ts[j].is("]"):
				depth++
			case ts[j].is("["):
				depth--
			case depth == 0 && ts[j].k == 'i':
				name = ts[j].s
			}
			if name != "" {
				break
			}
		}
	}
	if name == "" {
		return
	}
	p.addAlias(sc, name, "typedef "+render(ts), doc, "")
}

func (p *parser) addAlias(sc scope, name, sig, doc, dep string) {
	if sc.hidden || !sc.public || strings.HasPrefix(name, "_") {
		return
	}
	if sc.cls != nil {
		sc.cls.members = append(sc.cls.members, memberDecl{name: name, kind: "type", sig: sig, doc: doc, dep: dep})
		return
	}
	if emit, _ := p.want(sc.path); !emit {
		return
	}
	p.types = append(p.types, &typeDecl{fqn: join(sc.path, name), kind: "type", scope: sc.path, sig: sig, doc: doc, dep: dep, file: p.file})
}

func (p *parser) enum(sc scope) {
	start := p.i
	doc := p.docAt(start)
	p.i++ // enum
	scoped := false
	if p.peek().is("class") || p.peek().is("struct") {
		scoped = true
		p.i++
	}
	dep := p.skipAttributes()
	name := ""
	if p.peek().k == 'i' {
		name = p.peek().s
		p.i++
	}
	under := ""
	if p.peek().is(":") {
		p.i++
		us := p.i
		for !p.eof() && !p.peek().is("{") && !p.peek().is(";") {
			p.i++
		}
		under = render(p.t[us:p.i])
	}
	if !p.peek().is("{") {
		p.skipStatement() // opaque declaration, or "enum E e;" variable
		return
	}
	p.i++
	type enumerator struct{ name, doc string }
	var vals []enumerator
	for !p.eof() && !p.peek().is("}") {
		t := p.peek()
		vdoc := t.doc
		p.skipAttributes()
		t = p.peek()
		if t.k == 'i' {
			vals = append(vals, enumerator{t.s, vdoc})
		}
		// skip to the next top-level ',' or the closing '}'
		for !p.eof() && !p.peek().is(",") && !p.peek().is("}") {
			if p.peek().is("(") || p.peek().is("{") || p.peek().is("[") {
				p.skipGroup()
				continue
			}
			p.i++
		}
		if p.peek().is(",") {
			p.i++
		}
	}
	if p.peek().is("}") {
		p.i++
	}
	p.skipStatement() // "};" or "} var;"
	if sc.hidden || !sc.public {
		return
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i] = v.name
	}
	kw := "enum "
	if scoped {
		kw = "enum class "
	}
	sig := kw + name
	if under != "" {
		sig += " : " + under
	}
	sig += " { " + strings.Join(names, ", ") + " }"
	if len(sig) > 240 {
		sig = kw + name + " { " + strings.Join(names[:min(len(names), 12)], ", ") + ", … }"
	}
	if name == "" { // anonymous enum: enumerators belong to the enclosing scope
		if sc.cls != nil {
			for _, v := range vals {
				sc.cls.members = append(sc.cls.members, memberDecl{name: v.name, kind: "field", sig: v.name, doc: v.doc})
			}
		}
		return
	}
	if emit, _ := p.want(sc.path); !emit && sc.cls == nil {
		return
	}
	e := &typeDecl{fqn: join(sc.path, name), kind: "enum", scope: sc.path, sig: sig, doc: doc, dep: dep, file: p.file}
	for _, v := range vals {
		e.members = append(e.members, memberDecl{name: v.name, kind: "field", sig: name + "::" + v.name, doc: v.doc})
	}
	if sc.cls != nil {
		e.pageFQN = sc.cls.fqn
		sc.cls.members = append(sc.cls.members, memberDecl{name: name, kind: "type", sig: sig, doc: doc, dep: dep})
		if !scoped { // unscoped enumerators are also Outer::kValue
			for _, v := range vals {
				sc.cls.members = append(sc.cls.members, memberDecl{name: v.name, kind: "field", sig: name + " " + v.name, doc: v.doc})
			}
		}
	}
	p.types = append(p.types, e)
}

func (p *parser) class(sc scope) {
	start := p.i
	doc := p.docAt(start)
	kw := p.peek().s
	p.i++
	dep := ""
	var name string
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is("[") && p.next().is("["):
			if d := p.skipAttributes(); d != "" {
				dep = d
			}
			continue
		case t.is("alignas") || t.is("__declspec") || t.is("__attribute__"):
			p.i++
			p.skipGroup()
			continue
		case t.is("final") && (p.next().is(":") || p.next().is("{")):
			p.i++
			continue
		case t.k == 'i':
			if strings.Contains(t.s, "DEPRECATED") {
				dep = "deprecated"
			}
			name = t.s
			p.i++
			if p.peek().is("::") { // "struct ns::Name" — rare in headers; not ours to emit
				p.i = start
				p.skipStatement()
				return
			}
			if p.peek().is("(") && macroRe.MatchString(t.s) { // attribute macro with arguments
				p.skipGroup()
				name = ""
			}
			continue
		}
		break
	}
	next := p.peek()
	switch {
	case next.is(";"):
		p.i++ // forward declaration
		return
	case next.is("<"): // specialization: not a new type
		p.skipStatement()
		return
	case name == "" && next.is("{"): // anonymous struct/union
		p.skipStatement()
		return
	case !next.is("{") && !next.is(":"):
		p.i = start // elaborated type in a declaration ("struct HAL_Value* v;")
		p.declaration(sc)
		return
	}
	var bases []string
	if next.is(":") {
		p.i++
		var cur []tok
		depth := 0
		flush := func() {
			var parts []string
			for _, t := range cur {
				if t.is("public") || t.is("protected") || t.is("private") || t.is("virtual") || t.is("typename") {
					continue
				}
				parts = append(parts, t.s)
			}
			if b := stripTemplateArgs(strings.Join(parts, "")); b != "" {
				bases = append(bases, strings.TrimPrefix(b, "::"))
			}
			cur = nil
		}
		for !p.eof() {
			t := p.peek()
			if depth == 0 && t.is("{") {
				break
			}
			switch {
			case t.is("<"), t.is("("):
				depth++
			case t.is(">"), t.is(")"):
				depth--
			case depth == 0 && t.is(","):
				flush()
				p.i++
				continue
			}
			cur = append(cur, t)
			p.i++
		}
		flush()
	}
	if !p.peek().is("{") {
		p.skipStatement()
		return
	}
	p.i++
	path := append(append([]string(nil), sc.path...), name)
	emit, _ := p.want(sc.path)
	hidden := sc.hidden || !sc.public || !emit || strings.HasPrefix(name, "_")
	td := &typeDecl{fqn: join(sc.path, name), kind: kw, scope: sc.path, bases: bases, doc: doc, dep: dep, file: p.file}
	p.parseScope(scope{path: path, cls: td, public: kw != "class", hidden: hidden})
	// trailing declarators: "} name;" / "};"
	p.skipStatement()
	if !hidden {
		p.types = append(p.types, td)
		// A nested type is also a member of its enclosing class, so the
		// verifier sees "Outer::Inner" disappear when a season moves it out
		// (Phoenix 6 CANBus::CANBusStatus → CANBusStatus in 2027).
		if sc.cls != nil {
			sc.cls.members = append(sc.cls.members, memberDecl{name: name, kind: "type", sig: kw + " " + name, doc: doc, dep: dep})
		}
	}
}

// declaration handles functions, variables and anything else that ends with
// ';' or a body.
func (p *parser) declaration(sc scope) {
	start := p.i
	doc := p.docAt(start)
	dep := ""
	angle := 0
	var head []tok
	for !p.eof() {
		t := p.peek()
		if t.is("[") && p.next().is("[") {
			if d := p.skipAttributes(); d != "" {
				dep = d
			}
			continue
		}
		if t.k == 'i' {
			switch {
			case t.s == "operator" || t.s == "friend" || t.s == "concept":
				p.skipStatement()
				return
			case strings.Contains(t.s, "DEPRECATED") && macroRe.MatchString(t.s):
				dep = "deprecated"
				p.i++
				if p.peek().is("(") {
					g := p.i
					p.skipGroup()
					if m := joinStrings(p.t[g:p.i]); m != "" {
						dep = m
					}
				}
				continue
			}
		}
		if angle == 0 {
			switch {
			case t.is(";"), t.is("="), t.is("{"), t.is("["), t.is(":") && len(head) > 0:
				p.field(sc, head, doc, dep)
				return
			case t.is("}"):
				return // unbalanced input: let the scope close
			case t.is("("):
				prev := tok{}
				if len(head) > 0 {
					prev = head[len(head)-1]
				}
				g := p.i
				p.skipGroup()
				after := p.peek()
				if headAttached(prev, after) {
					head = append(head, p.t[g:p.i]...)
					continue
				}
				p.function(sc, head, p.t[g:p.i], doc, dep)
				return
			}
		}
		switch {
		case t.is("<"):
			angle++
		case t.is(">") && angle > 0:
			angle--
		case t.is("(") && angle > 0:
			g := p.i
			p.skipGroup()
			head = append(head, p.t[g:p.i]...)
			continue
		case t.is(";") || t.is("{") || t.is("}"):
			// '<' was a comparison inside something we do not model
			p.skipStatement()
			return
		}
		head = append(head, t)
		p.i++
	}
}

var qualifierWords = map[string]bool{"const": true, "volatile": true, "override": true, "final": true, "noexcept": true,
	"throw": true, "requires": true, "try": true, "mutable": true}

// headAttached reports whether a parenthesized group belongs to the
// declaration's type/specifiers (decltype(…), alignas(…), a macro with
// arguments) rather than being the parameter list.
func headAttached(prev, after tok) bool {
	switch prev.s {
	case "decltype", "alignas", "__attribute__", "__declspec", "noexcept", "sizeof", "alignof", "explicit":
		return true
	}
	if after.is("::") || after.is("~") || (after.k == 'i' && !qualifierWords[after.s]) {
		return true
	}
	return false
}

func (p *parser) field(sc scope, head []tok, doc, dep string) {
	isBitfield := p.peek().is(":")
	init := ""
	if p.peek().is("=") && !isBitfield {
		g := p.i + 1
		p.skipToSemicolon()
		if e := p.i - 1; e > g {
			if v := render(p.t[g:e]); len(v) <= 40 && !strings.Contains(v, "{") {
				init = " = " + v
			}
		}
	} else {
		p.skipToSemicolon()
	}
	name := ""
	for j := len(head) - 1; j >= 0; j-- {
		if head[j].k == 'i' && !cvWords[head[j].s] {
			name = head[j].s
			break
		}
	}
	if name == "" || sc.hidden || !sc.public || strings.HasPrefix(name, "_") || len(head) < 2 {
		return // "int;" or a lone identifier (macro use) is not a declaration we can name
	}
	for _, t := range head {
		if t.is("typedef") || t.is("return") {
			return
		}
	}
	sig := render(head) + init
	if sc.cls != nil {
		sc.cls.members = append(sc.cls.members, memberDecl{name: name, kind: "field", sig: sig, doc: doc, dep: dep})
		return
	}
	if emit, _ := p.want(sc.path); emit {
		p.funcs = append(p.funcs, nsDecl{fqn: join(sc.path, name), kind: "field", sig: sig, doc: doc, dep: dep, file: p.file})
	}
}

var cvWords = map[string]bool{"const": true, "volatile": true, "mutable": true}

func (p *parser) function(sc scope, head, params []tok, doc, dep string) {
	// qualifiers, trailing return type, requires-clause
	qs := p.i
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is("const") || t.is("volatile") || t.is("override") || t.is("final") || t.is("&") || t.is("mutable"):
			p.i++
			continue
		case t.is("noexcept") || t.is("throw"):
			p.i++
			if p.peek().is("(") {
				p.skipGroup()
			}
			continue
		case t.is("[") && p.next().is("["):
			p.skipAttributes()
			continue
		case t.is("->"):
			p.i++
			angle := 0
			for !p.eof() {
				u := p.peek()
				if angle == 0 && (u.is(";") || u.is("{") || u.is("=") || u.is("requires") || u.is("override") || u.is("final")) {
					break
				}
				switch {
				case u.is("<"):
					angle++
				case u.is(">") && angle > 0:
					angle--
				case u.is("("):
					p.skipGroup()
					continue
				}
				p.i++
			}
			continue
		case t.is("requires"):
			p.skipRequires()
			continue
		}
		break
	}
	quals := render(p.t[qs:p.i])
	deleted := false
	switch t := p.peek(); {
	case t.is(";"):
		p.i++
	case t.is("="):
		deleted = p.next().is("delete")
		p.skipToSemicolon()
	case t.is("{"):
		p.skipGroup()
		if p.peek().is(";") {
			p.i++
		}
	case t.is(":"): // constructor initializer list, then the body
		for !p.eof() && !p.peek().is("{") && !p.peek().is(";") && !p.peek().is("}") {
			if p.peek().is("(") || p.peek().is("[") {
				p.skipGroup()
				continue
			}
			p.i++
			if p.peek().is("{") && p.i > 0 && p.t[p.i-1].k == 'i' { // member{init}
				p.skipGroup()
			}
		}
		switch {
		case p.peek().is("{"):
			p.skipGroup()
		case p.peek().is(";"):
			p.i++
		}
	case t.is("try"):
		p.skipStatement()
	default:
		p.skipStatement()
	}
	if len(head) == 0 || deleted || sc.hidden || !sc.public {
		return
	}
	nameTok := head[len(head)-1]
	if nameTok.k != 'i' {
		return
	}
	if len(head) >= 2 {
		switch before := head[len(head)-2]; {
		case before.is("~"):
			return // destructor
		case before.is("::"):
			return // out-of-line definition of something declared elsewhere
		}
	}
	name := nameTok.s
	if strings.HasPrefix(name, "_") {
		return
	}
	for _, t := range head[:len(head)-1] {
		if t.is("return") || t.is("typedef") {
			return
		}
	}
	sig := render(head) + " " + render(params)
	if quals != "" {
		sig += " " + quals
	}
	if sc.cls != nil {
		kind := "method"
		if name == lastSeg(sc.cls.fqn) {
			kind = "constructor"
		}
		sc.cls.members = append(sc.cls.members, memberDecl{name: name, kind: kind, sig: sig, doc: doc, dep: dep})
		return
	}
	if len(head) < 2 {
		return // "FOO(x)" at namespace scope: a macro, not a function
	}
	if emit, _ := p.want(sc.path); emit {
		p.funcs = append(p.funcs, nsDecl{fqn: join(sc.path, name), kind: "function", sig: sig, doc: doc, dep: dep, file: p.file})
	}
}

// docAt returns the documentation attached to the token at i, or to the
// template prefix that introduced it.
func (p *parser) docAt(i int) string {
	if i < len(p.t) && p.t[i].doc != "" {
		return p.t[i].doc
	}
	return p.tmplDoc
}

// skipAttributes skips [[…]] groups and returns a deprecation message found
// in them ("deprecated" when the attribute has no message).
func (p *parser) skipAttributes() string {
	dep := ""
	for p.peek().is("[") && p.next().is("[") {
		g := p.i
		p.skipGroup()
		ts := p.t[g:p.i]
		for j, t := range ts {
			if t.is("deprecated") {
				dep = "deprecated"
				if j+1 < len(ts) && ts[j+1].is("(") {
					if m := joinStrings(ts[j+1:]); m != "" {
						dep = m
					}
				}
			}
		}
	}
	return dep
}

// joinStrings concatenates the string literals up to the first ')' (adjacent
// literals are one string in C++).
func joinStrings(ts []tok) string {
	var b strings.Builder
	for _, t := range ts {
		if t.is(")") {
			break
		}
		if t.k == 's' && strings.HasPrefix(t.s, `"`) {
			b.WriteString(strings.TrimSuffix(strings.TrimPrefix(t.s, `"`), `"`))
		}
	}
	return strings.TrimSpace(b.String())
}

// skipGroup skips a balanced (…), […] or {…} group starting at the current
// token (which must be the opener).
func (p *parser) skipGroup() {
	depth := 0
	for !p.eof() {
		t := p.peek()
		p.i++
		switch {
		case t.is("(") || t.is("[") || t.is("{"):
			depth++
		case t.is(")") || t.is("]") || t.is("}"):
			depth--
			if depth <= 0 {
				return
			}
		}
		if depth == 0 {
			return
		}
	}
}

// skipAngles skips a balanced <…> group; parentheses inside are skipped
// whole, so comparisons in default arguments do not confuse the count.
func (p *parser) skipAngles() {
	depth := 0
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is("(") || t.is("["):
			p.skipGroup()
			continue
		case t.is("<"):
			depth++
		case t.is(">"):
			depth--
		case t.is(";") || t.is("{") || t.is("}"):
			return // malformed: never cross a statement boundary
		}
		p.i++
		if depth <= 0 {
			return
		}
	}
}

// skipStatement advances past the next ';' at bracket depth 0, or past a
// top-level {…} block (function body, class body) and an optional ';'. It
// never consumes the '}' that closes the enclosing scope.
func (p *parser) skipStatement() {
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is(";"):
			p.i++
			return
		case t.is("}"):
			return
		case t.is("(") || t.is("["):
			p.skipGroup()
		case t.is("{"):
			p.skipGroup()
			if p.peek().is(";") {
				p.i++
			}
			return
		default:
			p.i++
		}
	}
}

// skipToSemicolon advances past the next ';' at bracket depth 0, crossing
// {…} groups (brace initializers, lambdas, "typedef struct {…} Name;"). It
// never consumes the '}' that closes the enclosing scope.
func (p *parser) skipToSemicolon() {
	for !p.eof() {
		t := p.peek()
		switch {
		case t.is(";"):
			p.i++
			return
		case t.is("}"):
			return
		case t.is("(") || t.is("[") || t.is("{"):
			p.skipGroup()
		default:
			p.i++
		}
	}
}

// skipScope consumes tokens through the matching close of an already-opened
// scope (the opener was consumed by the caller).
func (p *parser) skipScope() {
	depth := 1
	for !p.eof() {
		t := p.peek()
		p.i++
		switch {
		case t.is("{"):
			depth++
		case t.is("}"):
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

// render joins tokens into readable C++ ("const Foo &x", "std::vector<int>").
func render(ts []tok) string {
	var b strings.Builder
	for i, t := range ts {
		if i > 0 {
			prev := ts[i-1]
			space := true
			switch {
			case t.is(",") || t.is(")") || t.is("]") || t.is(">") || t.is("::") || t.is(";") || t.is("..."):
				space = false
			case prev.is("(") || prev.is("[") || prev.is("<") || prev.is("::") || prev.is("~") || prev.is("*") || prev.is("&") || prev.is("!"):
				space = false
			case t.is("(") && (prev.k == 'i' || prev.is(">") || prev.is(")")):
				space = false
			case t.is("<") && prev.k == 'i':
				space = false
			case t.is("[") && prev.k == 'i':
				space = false
			case t.is("&") && prev.is("&"):
				space = false
			}
			if space {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.s)
	}
	return b.String()
}

// stripTemplateArgs removes <…> groups ("wpi::SendableHelper<TalonFX>" →
// "wpi::SendableHelper").
func stripTemplateArgs(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func join(path []string, name string) string {
	if len(path) == 0 {
		return name
	}
	return strings.Join(path, "::") + "::" + name
}

func lastSeg(fqn string) string {
	if i := strings.LastIndex(fqn, "::"); i >= 0 {
		return fqn[i+2:]
	}
	return fqn
}
