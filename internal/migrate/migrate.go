package migrate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

// Resolver is the index view the mapper needs (retrieve.Engine).
type Resolver interface {
	Symbols(ctx context.Context, q index.SymbolQuery) ([]index.Symbol, error)
	SymbolsReplacedBy(ctx context.Context, fqn, season string) []index.Symbol
	MigrationsFrom(ctx context.Context, fqn, language string, members bool) []index.Migration
	MigrationsTo(ctx context.Context, fqn, language string) []index.Migration
	Migrations() []index.Migration
	SymbolSeasons() []string
	LibraryVersion(ctx context.Context, library, season, language string) string
	PackageExists(ctx context.Context, pkg, season, language string) bool
}

// Mapping sources and confidences.
const (
	SrcCurated   = "curated"
	SrcUpstream  = "upstream"
	SrcGenerated = "generated"
	SrcUnchanged = "unchanged"
)

// Mapping is one symbol's counterpart in the target season.
type Mapping struct {
	From       string `json:"from"`
	To         string `json:"to,omitempty"`
	FromSeason string `json:"from_season"`
	ToSeason   string `json:"to_season"`
	Language   string `json:"language"`
	Library    string `json:"library"`
	// Kind: rename | move | removed | signature | behavior | unchanged.
	Kind       string `json:"kind"`
	Notes      string `json:"notes,omitempty"`
	Confidence string `json:"confidence"` // high | medium | low
	Source     string `json:"source"`     // curated | upstream | generated | unchanged
	RuleID     string `json:"rule_id,omitempty"`
	Citation   string `json:"citation,omitempty"`
	Line       int    `json:"line,omitempty"` // code mode: first use
}

// Unresolved is a source symbol with no known counterpart.
type Unresolved struct {
	Symbol   string `json:"symbol"`
	Language string `json:"language"`
	Library  string `json:"library,omitempty"`
	Reason   string `json:"reason"`
	Line     int    `json:"line,omitempty"`
	// NoTable: the library has no target-season table indexed (coverage,
	// not a missing mapping).
	NoTable bool `json:"no_table,omitempty"`
}

// Result is a migration report.
type Result struct {
	From       string       `json:"from_season"`
	To         string       `json:"to_season"`
	Mappings   []Mapping    `json:"mappings"`
	Unresolved []Unresolved `json:"unresolved"`
	// NotFound lists inputs that are not symbols of the from season (team
	// code, a typo, or already migrated: see AlreadyIn).
	NotFound  []string `json:"not_found,omitempty"`
	AlreadyIn []string `json:"already_in_target,omitempty"`
	Checked   int      `json:"checked"`
	// Omitted counts code references left out by Query.MaxRefs.
	Omitted int `json:"omitted,omitempty"`
}

// Query selects what to map.
type Query struct {
	Symbol   string
	Code     string
	Language string // java | cpp | python; "" = any (symbol) or detected (code)
	From, To string // seasons; From < To maps forward, From > To backward
	// Members adds curated rules for the members of a mapped type.
	Members bool
	// MaxRefs caps code-mode references (default 200).
	MaxRefs int
}

// Map runs a symbol or code query.
func Map(ctx context.Context, r Resolver, q Query) Result {
	res := Result{From: q.From, To: q.To, Mappings: []Mapping{}, Unresolved: []Unresolved{}}
	if q.Code != "" {
		lang := q.Language
		if lang == "" {
			lang = verify.DetectLanguage(q.Code)
		}
		refs := verify.Refs(q.Code, lang)
		if q.MaxRefs <= 0 {
			q.MaxRefs = 200
		}
		if len(refs) > q.MaxRefs {
			res.Omitted = len(refs) - q.MaxRefs
			refs = refs[:q.MaxRefs]
		}
		seen := map[string]bool{}
		used := map[string]bool{} // references the file makes itself: reported at their own line
		for _, ref := range refs {
			used[ref.Symbol] = true
		}
		for _, ref := range refs {
			// Member rules of used types surface constructor and signature
			// changes (new TalonFX(id, "canivore")) that no ref names.
			m := mapper{r: r, lang: lang, from: q.From, to: q.To, line: ref.Line, members: !ref.Member, seen: seen, used: used}
			m.symbol(ctx, ref.Symbol, &res, true)
		}
		return res
	}
	m := mapper{r: r, lang: q.Language, from: q.From, to: q.To, members: q.Members, seen: map[string]bool{}}
	m.symbol(ctx, q.Symbol, &res, false)
	return res
}

type mapper struct {
	r              Resolver
	lang, from, to string
	line           int
	members        bool
	seen           map[string]bool // "lang fqn" already reported
	used           map[string]bool // code mode: symbols the file references directly
}

// symbol resolves a name in the from season and maps each match. In code
// mode (quiet), names that are not from-season symbols are skipped silently
// unless they already live in the target season.
func (m *mapper) symbol(ctx context.Context, name string, res *Result, quiet bool) {
	res.Checked++
	syms := m.resolve(ctx, name, m.from)
	if len(syms) == 0 {
		syms = m.inherited(ctx, name, m.from)
	}
	if len(syms) == 0 {
		if m.curatedOnly(ctx, name, res) {
			return
		}
		if there := m.resolve(ctx, name, m.to); len(there) > 0 {
			res.AlreadyIn = appendUnique(res.AlreadyIn, there[0].FQN)
		} else if !quiet {
			res.NotFound = appendUnique(res.NotFound, name)
		}
		return
	}
	for _, s := range syms {
		key := s.Language + " " + s.FQN
		if m.seen[key] {
			continue
		}
		m.seen[key] = true
		if m.from < m.to {
			m.forward(ctx, s, res)
		} else {
			m.backward(ctx, s, res)
		}
	}
}

// curatedOnly maps a name from the curated rules alone when its library has
// no API table for the season the rule starts from: a license-restricted
// vendor table is left out of the published index, but the rules are the
// project's own cited data and ship everywhere. Without a table there is
// nothing else to say about a type, so its members' rules always answer for
// it (new TalonFX(id, "canivore") under a TalonFX import).
func (m *mapper) curatedOnly(ctx context.Context, name string, res *Result) bool {
	name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "()"))
	if name == "" {
		return false
	}
	forward := m.from < m.to
	found := false
	for _, mr := range m.r.Migrations() {
		if m.lang != "" && mr.Language != m.lang {
			continue
		}
		side, season := mr.From, mr.FromSeason
		inRange := mr.FromSeason >= m.from && mr.ToSeason <= m.to
		if !forward {
			side, season = mr.To, mr.ToSeason
			inRange = mr.ToSeason <= m.from && mr.FromSeason >= m.to
		}
		owner, _, isMember := strings.Cut(side, "#")
		named := NameIs(side, name) || isMember && NameIs(owner, name)
		if side == "" || !inRange || !named {
			continue
		}
		if m.r.LibraryVersion(ctx, mr.Library, season, mr.Language) != "" {
			continue // the table decides: resolve found nothing, so the name is not there
		}
		if m.used[side] && !NameIs(side, name) {
			continue // code mode: the file uses this member itself, mapped at its own line
		}
		key := "rule " + mr.Language + " " + mr.RuleID + " " + mr.From
		if m.seen[key] {
			found = true
			continue
		}
		m.seen[key] = true
		c := curated(mr, m.line)
		if !forward {
			c.From, c.To, c.FromSeason, c.ToSeason = mr.To, mr.From, mr.ToSeason, mr.FromSeason
		}
		c.Notes = strings.TrimSpace(c.Notes + fmt.Sprintf(" (No %s %s API table is in this index; mapped from the curated rule alone.)",
			season, libName(mr.Library)))
		res.Mappings = append(res.Mappings, c)
		found = true
	}
	return found
}

// NameIs reports whether a name as written ("TalonFX", "TalonFX#setControl",
// "hardware.TalonFX.setControl", an FQN) denotes fqn. Case-sensitive.
func NameIs(fqn, name string) bool {
	if fqn == name || fqn == memberForm(name) {
		return true
	}
	return strings.HasSuffix("."+normalizeFQN(fqn), "."+normalizeFQN(name))
}

// resolve finds the symbols a name denotes in a season: an exact FQN (or
// its "Type.member" / "ns::Type::member" member form), else a name whose
// qualified tail matches ("ChassisSpeeds.fromFieldRelativeSpeeds"), else a
// case-sensitive simple name, preferring types. Overloads collapse to one
// row per FQN. The index's case-insensitive simple-name fallback is never
// trusted on its own: "Command" must not resolve to EventMarker#command.
func (m *mapper) resolve(ctx context.Context, name, season string) []index.Symbol {
	name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "()"))
	if name == "" {
		return nil
	}
	lookup := func(n string, exactOnly bool) []index.Symbol {
		syms, _ := m.r.Symbols(ctx, index.SymbolQuery{Name: n, Season: season, Language: m.lang, Limit: 50, Exact: exactOnly})
		return syms
	}
	var cands []index.Symbol
	qualified := strings.ContainsAny(name, ".#") || strings.Contains(name, "::")
	if qualified {
		cands = lookup(name, true)
		if len(cands) == 0 && !strings.Contains(name, "#") {
			if mf := memberForm(name); mf != "" {
				cands = lookup(mf, true)
			}
		}
		if len(cands) == 0 {
			want, tail := "."+normalizeFQN(name), index.SimpleName(name)
			if !strings.Contains(name, "#") {
				tail = index.SimpleName(memberForm(name))
			}
			for _, s := range lookup(tail, false) {
				if strings.HasSuffix("."+normalizeFQN(s.FQN), want) {
					cands = append(cands, s)
				}
			}
		}
	} else {
		for _, s := range lookup(name, false) {
			if index.SimpleName(s.FQN) == name {
				cands = append(cands, s)
			}
		}
	}
	var out []index.Symbol
	seen := map[string]bool{}
	for _, s := range cands {
		if s.Season != season || seen[s.Language+s.FQN] {
			continue
		}
		seen[s.Language+s.FQN] = true
		out = append(out, s)
	}
	// A simple name that matches both a type and members named alike: keep
	// the types.
	if !qualified {
		var types []index.Symbol
		for _, s := range out {
			if !strings.Contains(s.FQN, "#") {
				types = append(types, s)
			}
		}
		if len(types) > 0 {
			out = types
		}
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// inherited resolves "Type#member" (or "Type.member") where Type only
// inherits member: SparkMax#getOutputCurrent is declared on SparkBase, and
// the rules name the declaring type.
func (m *mapper) inherited(ctx context.Context, name, season string) []index.Symbol {
	owner, member, ok := strings.Cut(name, "#")
	if !ok {
		mf := memberForm(name)
		if mf == "" {
			return nil
		}
		owner, member, _ = strings.Cut(mf, "#")
	}
	for _, t := range m.resolve(ctx, owner, season) {
		if strings.Contains(t.FQN, "#") {
			continue
		}
		if d := verify.DeclaringType(ctx, m.r, t.FQN, member, season, t.Language); d != "" && d != t.FQN {
			if syms := m.resolve(ctx, d+"#"+member, season); len(syms) > 0 {
				return syms
			}
		}
	}
	return nil
}

// memberForm turns "a.b.Type.member" / "ns::Type::Member" into the index's
// "Type#member" form ("" when there is no separator).
func memberForm(name string) string {
	sep, width := strings.LastIndex(name, "::"), 2
	if dot := strings.LastIndexByte(name, '.'); dot > sep {
		sep, width = dot, 1
	}
	if sep <= 0 {
		return ""
	}
	return name[:sep] + "#" + name[sep+width:]
}

// normalizeFQN maps every separator ("::", "#") to "." for suffix matching.
func normalizeFQN(s string) string {
	return strings.NewReplacer("::", ".", "#", ".").Replace(s)
}

// forward maps s (a from-season symbol) hop by hop to the target season.
func (m *mapper) forward(ctx context.Context, s index.Symbol, res *Result) {
	seasons := m.hops(s.Season, m.to)
	cur := s
	var chain []Mapping
	for _, next := range seasons {
		mp, nxt, ok := m.hop(ctx, cur, next)
		if !ok {
			if m.r.LibraryVersion(ctx, cur.Library, next, cur.Language) == "" {
				res.Unresolved = append(res.Unresolved, Unresolved{Symbol: cur.FQN, Language: cur.Language, Library: cur.Library,
					Reason: fmt.Sprintf("no %s %s API table is indexed for %s yet; check its releases with frc_whats_new", next, cur.Language, libName(cur.Library)),
					Line:   m.line, NoTable: true})
				return
			}
			reason := fmt.Sprintf("not in the %s %s API and no curated, upstream or generated mapping", next, cur.Language)
			if cur.DeprecatedIn != "" {
				reason += fmt.Sprintf(" (deprecated since %s)", cur.DeprecatedIn)
			}
			res.Unresolved = append(res.Unresolved, Unresolved{Symbol: cur.FQN, Language: cur.Language, Library: cur.Library,
				Reason: reason, Line: m.line})
			return
		}
		chain = append(chain, mp...)
		if nxt == nil {
			break // removed: terminal
		}
		cur = *nxt
	}
	res.Mappings = append(res.Mappings, collapse(chain, m.line)...)
	if m.members && !strings.Contains(s.FQN, "#") {
		for _, mr := range m.r.MigrationsFrom(ctx, s.FQN, s.Language, true) {
			key := "rule " + mr.Language + " " + mr.RuleID + " " + mr.From
			if m.used[mr.From] {
				continue // the file calls this member: mapped at the call's own line
			}
			if mr.From != s.FQN && mr.FromSeason >= s.Season && mr.ToSeason <= m.to && !m.seen[key] {
				m.seen[key] = true
				m.seen[mr.Language+" "+mr.From] = true
				res.Mappings = append(res.Mappings, curated(mr, m.line))
			}
		}
	}
}

// hop maps one symbol from its season to the next indexed season. It
// returns the mappings for this hop and the symbol to continue from (nil
// when the chain ends: removed, or a target that is not an indexed symbol).
func (m *mapper) hop(ctx context.Context, cur index.Symbol, next string) ([]Mapping, *index.Symbol, bool) {
	var out []Mapping
	var target string
	terminal := false
	for _, mr := range m.r.MigrationsFrom(ctx, cur.FQN, cur.Language, false) {
		if mr.FromSeason != cur.Season || mr.ToSeason != next {
			continue
		}
		out = append(out, curated(mr, m.line))
		switch {
		case mr.Kind == "removed":
			terminal = true
		case mr.To != "" && target == "":
			target = mr.To
		}
	}
	if len(out) > 0 {
		if terminal && target == "" {
			return out, nil, true
		}
		if target == "" {
			target = cur.FQN // behavior/signature notes on a symbol that stays
		}
		if t := m.exact(ctx, target, next, cur.Language); t != nil {
			return out, t, true
		}
		return out, nil, true
	}
	// Same FQN in the next season.
	if t := m.exact(ctx, cur.FQN, next, cur.Language); t != nil {
		mp := Mapping{From: cur.FQN, To: t.FQN, FromSeason: cur.Season, ToSeason: next, Language: cur.Language,
			Library: cur.Library, Kind: SrcUnchanged, Source: SrcUnchanged, Confidence: "high"}
		if t.DeprecatedIn != "" {
			mp.Notes = fmt.Sprintf("deprecated in %s %s", libName(t.Library), t.DeprecatedIn)
			if t.Replacement != "" {
				mp.Notes += "; use " + t.Replacement
			}
		}
		return []Mapping{mp}, t, true
	}
	if cur.Replacement == "" {
		return m.viaOwner(ctx, cur, next)
	}
	src := cur.ReplacementSrc
	if src == "" {
		src = SrcUpstream
	}
	mp := Mapping{From: cur.FQN, To: cur.Replacement, FromSeason: cur.Season, ToSeason: next, Language: cur.Language,
		Library: cur.Library, Kind: "move", Source: src}
	if src == SrcUpstream {
		mp.Kind = "rename"
		mp.Notes = "from the library's deprecation note"
	}
	t := m.exact(ctx, cur.Replacement, next, cur.Language)
	if t == nil && src == SrcUpstream {
		// Deprecation notes name the replacement loosely ("DeferredCommand",
		// "AprilTagFieldLayout.loadField(AprilTagFields)"): resolve a unique
		// type in the next season.
		base, _, _ := strings.Cut(cur.Replacement, "(")
		if cands := m.resolve(ctx, base, next); len(cands) == 1 {
			t = &cands[0]
			mp.To = t.FQN
		}
	}
	switch {
	case t == nil:
		mp.Confidence = "low"
		mp.Notes = strings.TrimPrefix(mp.Notes+"; not an indexed "+next+" symbol", "; ")
		return []Mapping{mp}, nil, true
	case src == SrcGenerated && lastPkg(cur.FQN) == lastPkg(t.FQN):
		mp.Confidence = "high"
	default:
		mp.Confidence = "medium"
	}
	if src == SrcGenerated {
		mp.Notes = "same name, new package"
	}
	return []Mapping{mp}, t, true
}

// viaOwner maps a member whose own row carries no mapping through its
// type: when Type maps to Type' (curated, upstream or generated) and Type'
// declares or inherits the member in the next season, Type#m → Type'#m.
// A member of a removed type is removed with it.
func (m *mapper) viaOwner(ctx context.Context, cur index.Symbol, next string) ([]Mapping, *index.Symbol, bool) {
	owner, member, ok := strings.Cut(cur.FQN, "#")
	if !ok {
		return nil, nil, false
	}
	ownerSym := m.exact(ctx, owner, cur.Season, cur.Language)
	if ownerSym == nil {
		return nil, nil, false
	}
	omaps, target, ok := m.hop(ctx, *ownerSym, next)
	if !ok || len(omaps) == 0 {
		return nil, nil, false
	}
	om := omaps[0]
	mp := Mapping{From: cur.FQN, FromSeason: cur.Season, ToSeason: next, Language: cur.Language, Library: cur.Library,
		Source: om.Source, RuleID: om.RuleID, Citation: om.Citation}
	if om.Kind == "removed" {
		mp.Kind, mp.Confidence = "removed", om.Confidence
		mp.Notes = "its type " + index.SimpleName(owner) + " was removed: " + om.Notes
		return []Mapping{mp}, nil, true
	}
	if target == nil {
		return nil, nil, false
	}
	to := target.FQN + "#" + member
	t := m.exact(ctx, to, next, cur.Language)
	if t == nil {
		found, _ := verify.HasMember(ctx, m.r, target.FQN, member, next, cur.Language)
		if !found {
			return nil, nil, false
		}
		mp.Notes = "inherited by " + index.SimpleName(target.FQN) + "; "
	}
	// Derived through the type: not itself a cited rule, so it is reported
	// as generated (the type's rule id and citation stay as the reason).
	mp.To, mp.Kind, mp.Confidence, mp.Source = to, "move", "medium", SrcGenerated
	if om.Source == SrcUnchanged {
		mp.Kind, mp.Source, mp.Confidence = SrcUnchanged, SrcUnchanged, "high"
		mp.Notes = strings.TrimSuffix(mp.Notes, "; ")
		return []Mapping{mp}, t, true
	}
	mp.Notes += "same member of " + index.SimpleName(target.FQN) + " (type mapped " + index.SimpleName(owner) + " → " + index.SimpleName(target.FQN) + " by " + om.Source + " rule)"
	if t == nil {
		return []Mapping{mp}, nil, true
	}
	return []Mapping{mp}, t, true
}

// backward maps a newer symbol to its counterpart in an older season, hop
// by hop through every indexed season in between: each hop needs its own
// evidence (a curated rule into that season, the same FQN there, or a
// symbol there whose replacement is the current one).
func (m *mapper) backward(ctx context.Context, s index.Symbol, res *Result) {
	cur := s
	var chain []Mapping
	for _, prev := range m.hopsBack(s.Season, m.to) {
		mp, nxt := m.hopBack(ctx, cur, prev)
		if nxt == nil {
			reason := fmt.Sprintf("no known %s counterpart of %s (new in %s?)", prev, cur.FQN, cur.Season)
			noTable := m.r.LibraryVersion(ctx, cur.Library, prev, cur.Language) == ""
			if noTable {
				reason = fmt.Sprintf("no %s %s API table is indexed for %s", prev, cur.Language, libName(cur.Library))
			}
			res.Unresolved = append(res.Unresolved, Unresolved{Symbol: s.FQN, Language: s.Language, Library: s.Library,
				Reason: reason, Line: m.line, NoTable: noTable})
			return
		}
		chain = append(chain, mp)
		cur = *nxt
	}
	res.Mappings = append(res.Mappings, collapse(chain, m.line)...)
}

// hopBack maps cur (season S) to the previous indexed season prev.
func (m *mapper) hopBack(ctx context.Context, cur index.Symbol, prev string) (Mapping, *index.Symbol) {
	for _, mr := range m.r.MigrationsTo(ctx, cur.FQN, cur.Language) {
		if mr.ToSeason != cur.Season || mr.FromSeason != prev {
			continue
		}
		if t := m.exact(ctx, mr.From, prev, cur.Language); t != nil {
			c := curated(mr, m.line)
			c.From, c.To, c.FromSeason, c.ToSeason = mr.To, mr.From, mr.ToSeason, mr.FromSeason
			return c, t
		}
	}
	if t := m.exact(ctx, cur.FQN, prev, cur.Language); t != nil {
		return Mapping{From: cur.FQN, To: t.FQN, FromSeason: cur.Season, ToSeason: prev, Language: cur.Language,
			Library: cur.Library, Kind: SrcUnchanged, Source: SrcUnchanged, Confidence: "high"}, t
	}
	for _, old := range m.r.SymbolsReplacedBy(ctx, cur.FQN, prev) {
		if old.Language != cur.Language || old.Season != prev {
			continue
		}
		src := old.ReplacementSrc
		if src == "" {
			src = SrcUpstream
		}
		o := old
		return Mapping{From: cur.FQN, To: old.FQN, FromSeason: cur.Season, ToSeason: prev, Language: cur.Language,
			Library: cur.Library, Kind: "move", Source: src, Confidence: "medium"}, &o
	}
	return Mapping{}, nil
}

// hopsBack lists the indexed API seasons in [to, from), newest first; to
// itself is always the last hop.
func (m *mapper) hopsBack(from, to string) []string {
	var out []string
	ss := m.r.SymbolSeasons()
	for i := len(ss) - 1; i >= 0; i-- {
		if ss[i] < from && ss[i] >= to {
			out = append(out, ss[i])
		}
	}
	if len(out) == 0 || out[len(out)-1] != to {
		out = append(out, to)
	}
	return out
}

func (m *mapper) exact(ctx context.Context, fqn, season, lang string) *index.Symbol {
	s, _ := m.r.Symbols(ctx, index.SymbolQuery{Name: fqn, Season: season, Language: lang, Limit: 1, Exact: true})
	if len(s) == 0 || s[0].FQN != fqn {
		return nil
	}
	return &s[0]
}

// hops lists the indexed API seasons in (from, to].
func (m *mapper) hops(from, to string) []string {
	var out []string
	for _, s := range m.r.SymbolSeasons() {
		if s > from && s <= to {
			out = append(out, s)
		}
	}
	if len(out) == 0 || out[len(out)-1] != to {
		out = append(out, to)
	}
	return out
}

func curated(mr index.Migration, line int) Mapping {
	conf := "high"
	if !mr.Verified {
		conf = "medium"
	}
	return Mapping{From: mr.From, To: mr.To, FromSeason: mr.FromSeason, ToSeason: mr.ToSeason, Language: mr.Language,
		Library: mr.Library, Kind: mr.Kind, Notes: mr.Notes, Confidence: conf, Source: SrcCurated, RuleID: mr.RuleID,
		Citation: mr.Citation, Line: line}
}

// collapse joins a multi-season chain into one mapping per step that
// matters: unchanged hops disappear when a later hop changes the symbol.
func collapse(chain []Mapping, line int) []Mapping {
	var out []Mapping
	for _, c := range chain {
		c.Line = line
		if c.Source == SrcUnchanged && len(chain) > 1 && c.Notes == "" {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 && len(chain) > 0 {
		last := chain[len(chain)-1]
		last.From, last.FromSeason, last.Line = chain[0].From, chain[0].FromSeason, line
		out = append(out, last)
	}
	return out
}

// lastPkg is the innermost package segment of a type or member FQN
// ("edu.wpi.first.math.kinematics.X" → "kinematics", "frc::X" → "frc").
func lastPkg(fqn string) string {
	fqn, _, _ = strings.Cut(fqn, "#")
	sep := "."
	if strings.Contains(fqn, "::") {
		sep = "::"
	}
	parts := strings.Split(fqn, sep)
	last := ""
	for _, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			break
		}
		last = p
	}
	return last
}

func appendUnique(xs []string, x string) []string {
	if slices.Contains(xs, x) {
		return xs
	}
	return append(xs, x)
}

var libNames = map[string]string{"wpilib": "WPILib", "phoenix6": "Phoenix 6", "revlib": "REVLib", "photonvision": "PhotonLib",
	"pathplannerlib": "PathPlannerLib", "choreolib": "ChoreoLib", "advantagekit": "AdvantageKit", "yagsl": "YAGSL"}

func libName(lib string) string {
	if n := libNames[lib]; n != "" {
		return n
	}
	return lib
}
