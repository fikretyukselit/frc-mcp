// Package retrieve is the query-time retrieval engine (docs/retrieval.md §2):
//
//	router → pin filter → { exact symbol ∥ FTS5 BM25 [∥ dense] } → RRF → boosts
//	       → per-document diversity → abstention / version_mismatch
//
// It is read-only, stateless per request, and safe for concurrent use.
package retrieve

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/fikretyukselit/frc-mcp/internal/embed/m2v"
	"github.com/fikretyukselit/frc-mcp/internal/facts"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/router"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
	"github.com/fikretyukselit/frc-mcp/internal/vec"
)

// Tunables. They are constants (not flags) so evaluation runs are reproducible;
// changing any of them requires an eval diff in the PR.
const (
	rrfK            = 60   // reciprocal rank fusion constant
	ftsDepth        = 50   // BM25 candidates per shard
	maxIdentifiers  = 4    // symbol lookups per query
	maxPerDoc       = 2    // diversity: chunks per source document
	maxRanked       = 50   // results kept for pagination
	lowConfidence   = 0.35 // below → status low_confidence
	lowConfidenceK  = 3    // abstain rather than pad
	strongOther     = 0.6  // other-season confidence that makes a weak pinned result a version_mismatch
	otherSeasonHits = 5
)

// Status values (mirrors docs/mcp-surface.md §2).
const (
	StatusOK              = "ok"
	StatusLowConfidence   = "low_confidence"
	StatusNoMatch         = "no_match"
	StatusVersionMismatch = "version_mismatch"
	StatusSyncing         = "syncing"
)

// ErrNotReady is returned when no shard is loaded yet (first run / syncing).
var ErrNotReady = errors.New("retrieve: index not ready")

// Engine searches a fixed set of shards. Replace the whole Engine to hot-swap.
type Engine struct {
	shards        []*index.Reader
	defaultSeason string
	digest        string
	builtAt       time.Time

	// Dense retrieval (optional): one vector layer + row metadata per shard.
	model    *m2v.Model
	layers   []*vec.Layer
	metas    []*index.RowMeta
	allowMu  sync.Mutex
	allow    map[string][][]uint64 // filter key → per-shard bitsets
	warnings []string

	catalog *facts.Catalog // vendordep facts from every shard
	expires time.Time

	// Per-shard pruning facts from shard metadata: a season-pinned query
	// never touches shards without that season, and symbol lookups skip
	// shards without symbol tables (28 shards → typically 8–10 queried).
	seasons    []map[string]bool
	hasSymbols []bool

	releases []index.Release // every shard's release facts, newest first
	hwspecs  []index.HWSpec  // hardware rows, by category/part/source/season

	// Curated migration rules (a few hundred rows), keyed by source and
	// target FQN; symbolSeasons are the seasons that have API tables.
	migrations    []index.Migration
	migFrom       map[string][]int
	migTo         map[string][]int
	symbolSeasons []string
}

// Options configures an Engine.
type Options struct {
	// DefaultSeason is used when neither the caller nor the query pins one.
	// Empty selects the newest season that has a stable channel.
	DefaultSeason string
	// Model enables dense retrieval for shards that ship a matching vector
	// layer (<shard>.<model>.vec). Nil runs BM25 + exact symbol only.
	Model *m2v.Model
	// DisableDense turns dense retrieval off even when available (eval A/B).
	DisableDense bool
	// Expires is the installed manifest's expiry (zero: unmanaged/dev index).
	Expires time.Time
}

// New builds an engine over opened shards (which it does not own).
func New(shards []*index.Reader, opt Options) *Engine {
	e := &Engine{shards: shards, defaultSeason: opt.DefaultSeason, expires: opt.Expires}
	h := sha256.New()
	best := ""
	for _, s := range shards {
		m := s.Meta()
		ss := map[string]bool{}
		for _, sc := range m.Seasons {
			ss[sc.Season] = true
		}
		e.seasons = append(e.seasons, ss)
		e.hasSymbols = append(e.hasSymbols, m.Symbols > 0)
		h.Write([]byte(m.BuildID))
		if m.BuiltAt.After(e.builtAt) {
			e.builtAt = m.BuiltAt
		}
		// The default season is the newest *stable API* season: shards with
		// symbol tables define what a season is. Release-note shards span
		// seasons and a vendor's "stable" 2027 tag must not flip every
		// unpinned query to 2027.
		if m.Symbols == 0 {
			continue
		}
		for _, sc := range m.Seasons {
			if sc.Channel == "stable" && sc.Season > best {
				best = sc.Season
			}
		}
	}
	if best == "" { // no API shards (fixture-only / docs-only indexes)
		for _, s := range shards {
			for _, sc := range s.Meta().Seasons {
				if sc.Channel == "stable" && sc.Season > best {
					best = sc.Season
				}
			}
		}
	}
	e.digest = hex.EncodeToString(h.Sum(nil))[:16]
	if e.defaultSeason == "" {
		e.defaultSeason = best
	}
	if opt.Model != nil && !opt.DisableDense {
		e.loadDense(opt.Model)
	}
	var vds []index.Vendordep
	for _, s := range shards {
		rows, err := s.Vendordeps(context.Background(), "")
		if err != nil {
			e.warnings = append(e.warnings, "vendordeps:"+s.Meta().Name+": "+err.Error())
			continue
		}
		vds = append(vds, rows...)
	}
	e.catalog = facts.NewCatalog(vds)
	for _, s := range shards {
		rs, err := s.Releases(context.Background())
		if err != nil {
			e.warnings = append(e.warnings, "releases:"+s.Meta().Name+": "+err.Error())
			continue
		}
		e.releases = append(e.releases, rs...)
	}
	for _, s := range shards {
		hs, err := s.HWSpecs(context.Background())
		if err != nil {
			e.warnings = append(e.warnings, "hw_spec:"+s.Meta().Name+": "+err.Error())
			continue
		}
		e.hwspecs = append(e.hwspecs, hs...)
	}
	// Rows of one part can come from several shards (hardware and
	// hardware-restricted): order them as one table would.
	slices.SortStableFunc(e.hwspecs, func(a, b index.HWSpec) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Part, b.Part), cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Season, b.Season))
	})
	e.migFrom, e.migTo = map[string][]int{}, map[string][]int{}
	symSeasons := map[string]bool{}
	for i, s := range shards {
		if e.hasSymbols[i] {
			for se := range e.seasons[i] {
				symSeasons[se] = true
			}
		}
		ms, err := s.AllMigrations(context.Background())
		if err != nil {
			e.warnings = append(e.warnings, "migration:"+s.Meta().Name+": "+err.Error())
			continue
		}
		for _, m := range ms {
			e.migFrom[m.From] = append(e.migFrom[m.From], len(e.migrations))
			if m.To != "" {
				e.migTo[m.To] = append(e.migTo[m.To], len(e.migrations))
			}
			e.migrations = append(e.migrations, m)
		}
	}
	for se := range symSeasons {
		e.symbolSeasons = append(e.symbolSeasons, se)
	}
	slices.Sort(e.symbolSeasons)
	slices.SortStableFunc(e.releases, func(a, b index.Release) int {
		if c := b.PublishedAt.Compare(a.PublishedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Library, b.Library)
	})
	return e
}

// WhatsNewQuery selects release facts. Zero values mean "any".
type WhatsNewQuery struct {
	Library string    // library id or alias; "" or "all" = every library
	Season  string    // FRC season; "" = any
	Since   string    // a version of Library (exclusive), or empty
	After   time.Time // releases published after this time, or zero
	Stable  bool      // stable channel only
	Limit   int
}

// ReleaseLibraries lists the libraries with release facts, sorted.
func (e *Engine) ReleaseLibraries() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range e.releases {
		if !seen[r.Library] {
			seen[r.Library] = true
			out = append(out, r.Library)
		}
	}
	slices.Sort(out)
	return out
}

// WhatsNew returns matching releases, newest first. It is an exact fact
// lookup (no similarity): the order is publication time.
func (e *Engine) WhatsNew(q WhatsNewQuery) []index.Release {
	lib := LibraryID(q.Library)
	var out []index.Release
	for _, r := range e.releases {
		switch {
		case lib != "" && r.Library != lib:
		case q.Season != "" && r.Season != q.Season:
		case q.Stable && r.Channel != "stable":
		case !q.After.IsZero() && !r.PublishedAt.After(q.After):
		case q.Since != "" && facts.CompareVersions(r.Version, q.Since) <= 0:
		default:
			out = append(out, r)
			if q.Limit > 0 && len(out) == q.Limit {
				return out
			}
		}
	}
	return out
}

// ---- hardware (frc_hardware) ----

// partAliases map common names to hw_spec part ids.
var partAliases = map[string]string{
	"kraken": "krakenx60", "x60": "krakenx60", "krakenx60": "krakenx60", "x44": "krakenx44", "krakenx44": "krakenx44",
	"falcon": "falcon500", "falcon500": "falcon500", "vortex": "neovortex", "neovortex": "neovortex", "sparkflexvortex": "neovortex",
	"neo": "neo", "neov11": "neo", "neov1.1": "neo", "neo2.0": "neo2", "neov2": "neo2", "neo550": "neo550", "550": "neo550",
	"775": "vex775pro", "775pro": "vex775pro", "vex775pro": "vex775pro", "redline": "775redline", "775redline": "775redline",
	"cim": "cim", "minicim": "minicim", "bag": "bag", "minion": "minion", "pulsar": "thriftypulsar", "romi": "romibuiltin",
	"pigeon": "ctrepigeon2", "pigeon2": "ctrepigeon2", "pigeon2.0": "ctrepigeon2", "canandgyro": "reduxcanandgyro",
	"borongyro": "reduxcanandgyro", "boron": "reduxcanandgyro", "throughbore": "revthroughborev2",
	"throughboreencoder": "revthroughborev2", "tbe": "revthroughborev2", "maxswerve": "revmaxswerve",
}

// PartID normalizes a part name: "Kraken X60 FOC" → "krakenx60-foc".
func PartID(name string) string {
	n := strings.ToLower(name)
	n = strings.NewReplacer(" ", "", "-", "", "_", "", "(", "", ")", "").Replace(n)
	foc := false
	if base, ok := strings.CutSuffix(n, "foc"); ok && base != "" {
		n, foc = base, true
	}
	if id, ok := partAliases[n]; ok {
		n = id
	}
	if foc {
		return n + "-foc"
	}
	return n
}

// SeasonAll is the season of hardware rows that do not depend on the FRC
// season (ReCalc, vendor spec pages, curated data): they answer every season.
const SeasonAll = "all"

// HWPart groups every source's rows for one part.
type HWPart struct {
	Part, Name, Category string
	Rows                 []index.HWSpec
}

// Hardware returns the rows for the given parts (or a category), one group
// per part, with every source and season kept separate. A season filter
// keeps that season's rows plus the season-independent ones (SeasonAll).
func (e *Engine) Hardware(parts []string, category, season string) (found []HWPart, unknown []string) {
	known := e.HardwareParts()
	want := map[string]bool{}
	resolved := map[string]string{} // input → part id
	for _, p := range parts {
		id := PartID(p)
		if !slices.Contains(known, id) {
			// A unique prefix is accepted ("andymark rs775" → andymarkrs775_125),
			// then a unique suffix, for names without the vendor ("mk4i" →
			// sdsmk4i, "cancoder" → ctrecancoder).
			for _, match := range []func(k string) bool{
				func(k string) bool { return strings.HasPrefix(k, id) },
				func(k string) bool { return strings.HasSuffix(k, id) },
			} {
				var cands []string
				for _, k := range known {
					if match(k) && !strings.HasSuffix(k, "-foc") {
						cands = append(cands, k)
					}
				}
				if len(cands) == 1 {
					id = cands[0]
					break
				}
			}
		}
		want[id], resolved[p] = true, id
	}
	idx := map[string]int{}
	for _, h := range e.hwspecs {
		switch {
		case len(want) > 0 && !want[h.Part]:
			continue
		case len(want) == 0 && category != "" && h.Category != category:
			continue
		case season != "" && h.Season != season && h.Season != SeasonAll:
			continue
		}
		i, ok := idx[h.Part]
		if !ok {
			i = len(found)
			idx[h.Part] = i
			found = append(found, HWPart{Part: h.Part, Name: h.Name, Category: h.Category})
		}
		found[i].Rows = append(found[i].Rows, h)
	}
	order := map[string]int{}
	for i, p := range parts {
		if _, ok := idx[resolved[p]]; !ok {
			unknown = append(unknown, p)
		}
		if _, ok := order[resolved[p]]; !ok {
			order[resolved[p]] = i
		}
	}
	if len(parts) > 0 { // answer in the order asked
		slices.SortStableFunc(found, func(a, b HWPart) int { return order[a.Part] - order[b.Part] })
	}
	return found, unknown
}

// HardwareParts lists the known part ids.
func (e *Engine) HardwareParts() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range e.hwspecs {
		if !seen[h.Part] {
			seen[h.Part] = true
			out = append(out, h.Part)
		}
	}
	slices.Sort(out)
	return out
}

// SimFactory finds the DCMotor factory for a WPILib factory name (Java
// "getKrakenX60Foc") in another language's symbol table (C++
// "KrakenX60FOC", Python "krakenX60FOC"), by case-insensitive name match,
// then a unique suffix match ("getAndymarkRs775_125" ↔ "RS775_125").
func (e *Engine) SimFactory(ctx context.Context, javaFactory, season, language string) string {
	var owner string
	for _, sh := range e.shards {
		syms, err := sh.Symbols(ctx, index.SymbolQuery{Name: "DCMotor", Season: season, Language: language, Limit: 5})
		if err != nil {
			continue
		}
		for _, s := range syms {
			if s.Kind == "class" {
				owner = s.FQN
				break
			}
		}
		if owner != "" {
			break
		}
	}
	if owner == "" {
		return ""
	}
	want := strings.ToLower(javaFactory)
	if language != "java" {
		want = strings.TrimPrefix(want, "get")
	}
	var suffix []string
	for _, sh := range e.shards {
		ms, err := sh.Members(ctx, owner, season, language)
		if err != nil {
			continue
		}
		for _, m := range ms {
			name := index.SimpleName(m.FQN)
			l := strings.ToLower(name)
			switch {
			case l == want:
				return owner + "#" + name
			case len(l) >= 4 && strings.HasSuffix(want, l) && m.Kind != "field":
				suffix = append(suffix, owner+"#"+name)
			}
		}
	}
	if len(suffix) == 1 {
		return suffix[0]
	}
	return ""
}

// libraryAliases maps common names to library ids (frc_whats_new input).
var libraryAliases = map[string]string{
	"all": "", "wpilib": "wpilib", "allwpilib": "wpilib", "gradlerio": "wpilib",
	"phoenix": "phoenix6", "phoenix6": "phoenix6", "ctre": "phoenix6",
	"rev": "revlib", "revlib": "revlib", "photon": "photonvision", "photonlib": "photonvision", "photonvision": "photonvision",
	"pathplanner": "pathplannerlib", "pathplannerlib": "pathplannerlib", "choreo": "choreolib", "choreolib": "choreolib",
	"akit": "advantagekit", "advantagekit": "advantagekit", "yagsl": "yagsl",
}

// LibraryID normalizes a library name or alias; unknown names pass through
// lower-cased (so the caller can report "no releases for X").
func LibraryID(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(n)
	if id, ok := libraryAliases[n]; ok {
		return id
	}
	return n
}

// Stale reports whether the installed index is past its manifest expiry
// (a stalled publisher or a freeze attack; docs/security.md §2.4).
func (e *Engine) Stale(now time.Time) bool { return !e.expires.IsZero() && now.After(e.expires) }

// Catalog returns the vendordep catalog (possibly empty).
func (e *Engine) Catalog() *facts.Catalog { return e.catalog }

// loadDense opens vector layers whose model id and row count match. A
// mismatched or missing layer disables dense for that shard only (reported by
// Warnings and per-result Degraded), never the whole engine.
func (e *Engine) loadDense(m *m2v.Model) {
	e.model = m
	e.layers = make([]*vec.Layer, len(e.shards))
	e.metas = make([]*index.RowMeta, len(e.shards))
	e.allow = map[string][][]uint64{}
	for i, s := range e.shards {
		l, err := vec.Open(index.VectorPath(s.Path(), m.ID))
		if err != nil {
			e.warnings = append(e.warnings, "dense:"+s.Meta().Name+": no vector layer")
			continue
		}
		meta, err := s.RowMeta(context.Background())
		if err != nil || l.ModelID != m.ID || l.Dims != m.Dims || l.Rows != meta.Len() {
			l.Close()
			e.warnings = append(e.warnings, "dense:"+s.Meta().Name+": layer does not match shard/model")
			continue
		}
		e.layers[i], e.metas[i] = l, meta
	}
}

// Warnings lists degraded capabilities detected at load time.
func (e *Engine) Warnings() []string { return e.warnings }

// Dense reports whether any shard has dense retrieval.
func (e *Engine) Dense() bool {
	for _, l := range e.layers {
		if l != nil {
			return true
		}
	}
	return false
}

// Close releases vector layers (shards are owned by the caller).
func (e *Engine) Close() {
	for _, l := range e.layers {
		if l != nil {
			l.Close()
		}
	}
}

func (e *Engine) allowFor(f index.Filter) [][]uint64 {
	key := f.Season + "|" + f.Language + "|" + strings.Join(f.Libraries, ",") + "|" + strings.Join(f.Kinds, ",") + "|" + fmt.Sprint(f.IncludeCommunity)
	e.allowMu.Lock()
	defer e.allowMu.Unlock()
	if a, ok := e.allow[key]; ok {
		return a
	}
	a := make([][]uint64, len(e.shards))
	for i, m := range e.metas {
		if m != nil {
			a[i] = m.Allow(f)
		}
	}
	if len(e.allow) > 256 { // bound the cache; filter combinations are few in practice
		clear(e.allow)
	}
	e.allow[key] = a
	return a
}

// Ready reports whether any shard is loaded.
func (e *Engine) Ready() bool { return e != nil && len(e.shards) > 0 }

// Digest identifies the loaded shard set (used to invalidate cursors).
func (e *Engine) Digest() string { return e.digest }

// BuiltAt is the newest shard build time.
func (e *Engine) BuiltAt() time.Time { return e.builtAt }

// DefaultSeason is the season applied when nothing pins one.
func (e *Engine) DefaultSeason() string { return e.defaultSeason }

// Query is a search request after tool-argument validation.
type Query struct {
	Text             string
	Season           string // explicit pin (tool arg)
	Channel          string
	Language         string
	Libraries        []string // explicit filter
	Kinds            []string
	IncludeCommunity bool
	// Window selects which ranked hits get their full chunk loaded
	// (the page the caller will display). Zero Limit hydrates the top 5.
	Offset, Limit int
}

// Hit is a ranked candidate. Chunk is nil unless the hit falls inside the
// hydrated window (or is the top hit, used for confidence).
type Hit struct {
	Shard       int
	Row         int64
	DocNum      int32
	Score       float64
	SymbolMatch bool
	Season      string // effective filter season or "" (cross-season), for bookkeeping
	Chunk       *index.Chunk

	trust, kind uint8
	alpha       bool
	suspect     bool
	libMatch    bool
}

// Result is the engine's answer. Hits is the full ranked list (≤ maxRanked);
// only Hits[Offset:Offset+Limit] and Hits[0] carry a Chunk.
type Result struct {
	Status      string
	Confidence  float64
	Season      string // effective season filter
	PinSource   string // arg | query | default
	Language    string
	Decision    router.Decision
	Hits        []Hit
	OtherSeason []Hit          // version_mismatch: strongest hits from other seasons (hydrated)
	Symbols     []index.Symbol // exact symbol matches
	Degraded    []string       // retrievers unavailable for this call
}

// Search runs the full pipeline.
func (e *Engine) Search(ctx context.Context, q Query) (Result, error) {
	if !e.Ready() {
		return Result{Status: StatusSyncing}, ErrNotReady
	}
	if q.Limit <= 0 {
		q.Limit = 5
	}
	d := router.Decide(q.Text)
	res := Result{Decision: d, Season: q.Season, PinSource: "arg", Language: q.Language}
	switch {
	case res.Season != "":
	case d.Season != "":
		res.Season, res.PinSource = d.Season, "query"
	default:
		res.Season, res.PinSource = e.defaultSeason, "default"
	}
	if res.Language == "" {
		res.Language = d.Language
	}
	includeCommunity := q.IncludeCommunity || slices.Contains(q.Kinds, "forum") || d.Intent == router.IntentTroubleshoot
	f := index.Filter{Season: res.Season, Language: res.Language, Libraries: q.Libraries, Kinds: q.Kinds,
		IncludeCommunity: includeCommunity}
	terms := contentTerms(q.Text)

	hits, syms, active, err := e.rank(ctx, q, d, f)
	if err != nil {
		return res, err
	}
	res.Hits, res.Symbols = hits, syms
	if e.model != nil && !e.Dense() {
		res.Degraded = append(res.Degraded, "dense")
	}
	if len(hits) > 0 {
		if err := e.hydrate(ctx, hits[:1]); err != nil {
			return res, err
		}
		res.Confidence = confidence(hits, len(syms) > 0, active, terms)
	}

	// Never blend seasons. When the pinned season has nothing, or only a weak
	// match, look across seasons and report where a strong answer lives.
	if f.Season != "" && (len(hits) == 0 || res.Confidence < lowConfidence) {
		f.Season = ""
		other, osyms, _, err := e.rank(ctx, q, d, f)
		if err != nil {
			return res, err
		}
		other = slices.DeleteFunc(other, func(h Hit) bool { return h.Chunk != nil && h.Chunk.Season == res.Season })
		other = other[:min(len(other), otherSeasonHits+lowConfidenceK)]
		if err := e.hydrate(ctx, other); err != nil {
			return res, err
		}
		other = slices.DeleteFunc(other, func(h Hit) bool { return h.Chunk.Season == res.Season })
		if len(other) > 0 && (len(hits) == 0 || confidence(other, len(osyms) > 0, active, terms) >= strongOther) {
			res.Status = StatusVersionMismatch
			res.OtherSeason = other[:min(len(other), otherSeasonHits)]
			res.Hits = hits[:min(len(hits), lowConfidenceK)]
			if len(res.Symbols) == 0 {
				res.Symbols = osyms
			}
			return res, e.hydrate(ctx, res.Hits)
		}
	}
	switch {
	case len(hits) == 0:
		res.Status = StatusNoMatch
		return res, nil
	case res.Confidence < lowConfidence:
		res.Status = StatusLowConfidence
		res.Hits = hits[:min(len(hits), lowConfidenceK)]
		return res, e.hydrate(ctx, res.Hits)
	}
	res.Status = StatusOK
	lo := min(q.Offset, len(hits))
	return res, e.hydrate(ctx, hits[lo:min(lo+q.Limit, len(hits))])
}

// contentTerms are the lower-cased, non-stopword query tokens.
func contentTerms(q string) []string {
	var out []string
	for _, t := range textutil.Tokens(q) {
		l := strings.ToLower(t)
		if len(l) >= 2 && !textutil.IsStopword(l) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

type key struct {
	shard int
	row   int64
}

// rank runs the retrievers on every shard concurrently, fuses them with RRF,
// applies boosts and diversity. Candidates stay light (integers only).
func (e *Engine) rank(ctx context.Context, q Query, d router.Decision, f index.Filter) ([]Hit, []index.Symbol, int, error) {
	match := textutil.FTSQuery(q.Text)
	ids := d.Identifiers[:min(len(d.Identifiers), maxIdentifiers)]
	b := index.Boost{Libraries: d.Libraries}
	for _, id := range ids {
		b.Symbols = append(b.Symbols, index.SimpleName(id))
	}

	type shardOut struct {
		fts       []index.Scored
		dense     []vec.Hit
		syms      []index.Symbol
		symChunks []string // chunk ids referenced by matching symbols, in rank order
	}
	// Dense: one query embedding for all shards. Skipped for non-English
	// queries (the lite model is English-centric) and when no layer exists.
	var q8 []int8
	var qs float32
	useDense := e.Dense() && !d.NonEnglish && strings.TrimSpace(q.Text) != ""
	var allow [][]uint64
	if useDense {
		qv := e.model.Encode(q.Text, nil)
		q8, qs = vec.Quantize(qv, nil)
		useDense = qs != 0
		allow = e.allowFor(f)
	}
	outs := make([]shardOut, len(e.shards))
	g, gctx := errgroup.WithContext(ctx)
	// The pure-Go SQLite driver serializes its allocator behind one mutex;
	// beyond ~4 concurrent shard queries the goroutines mostly wait on it
	// (measured on 28 shards: unlimited 4.9 ms/op, 4 → 3.5 ms/op, 1 → 6.6).
	g.SetLimit(min(4, max(1, runtime.GOMAXPROCS(0))))
	for i, sh := range e.shards {
		if f.Season != "" && !e.seasons[i][f.Season] {
			continue
		}
		g.Go(func() error {
			o := &outs[i]
			var err error
			if o.fts, err = sh.FTS(gctx, match, f, b, ftsDepth, scoredPool.get()); err != nil {
				return err
			}
			if useDense && e.layers[i] != nil {
				o.dense = e.layers[i].Search(q8, qs, allow[i], ftsDepth, nil)
			}
			if !e.hasSymbols[i] {
				return nil
			}
			for _, id := range ids {
				syms, err := sh.Symbols(gctx, index.SymbolQuery{Name: id, Season: f.Season, Language: f.Language, Limit: 8})
				if err != nil {
					return err
				}
				o.syms = append(o.syms, syms...)
				for _, s := range syms {
					if s.ChunkID != "" {
						o.symChunks = append(o.symChunks, s.ChunkID)
					}
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, 0, err
	}

	// BM25: merge shards by score (comparable across shards of one schema).
	type ranked struct {
		shard int
		s     index.Scored
	}
	var bm []ranked
	var syms []index.Symbol
	for i, o := range outs {
		for _, s := range o.fts {
			bm = append(bm, ranked{i, s})
		}
		syms = append(syms, o.syms...)
	}
	slices.SortStableFunc(bm, func(a, b ranked) int { return cmp.Compare(a.s.Score, b.s.Score) })

	cands := make(map[key]*Hit, len(bm)+4)
	for r, x := range bm {
		k := key{x.shard, x.s.Row}
		h := cands[k]
		if h == nil {
			h = &Hit{Shard: x.shard, Row: x.s.Row, DocNum: x.s.DocNum, SymbolMatch: x.s.SymMatch,
				trust: x.s.Trust, kind: x.s.Kind, alpha: x.s.Alpha, suspect: x.s.Suspect, libMatch: x.s.LibMatch}
			cands[k] = h
		}
		h.Score += 1 / float64(rrfK+r+1)
	}
	for i := range outs {
		scoredPool.put(outs[i].fts)
	}
	// Dense list: merge shards by cosine (comparable: same model everywhere).
	active := 1
	if len(ids) > 0 {
		active++
	}
	if useDense {
		active++
		type dh struct {
			shard int
			h     vec.Hit
		}
		var dl []dh
		for i, o := range outs {
			for _, h := range o.dense {
				dl = append(dl, dh{i, h})
			}
		}
		slices.SortStableFunc(dl, func(a, b dh) int { return cmp.Compare(b.h.Score, a.h.Score) })
		for r, x := range dl[:min(len(dl), ftsDepth)] {
			row := int64(x.h.Row) + 1
			k := key{x.shard, row}
			h := cands[k]
			if h == nil {
				m := e.metas[x.shard]
				i := int(x.h.Row)
				h = &Hit{Shard: x.shard, Row: row, DocNum: m.DocNum[i], trust: m.Trust[i], kind: m.Kind[i],
					alpha: m.Alpha[i], suspect: m.Suspect[i],
					libMatch: slices.Contains(d.Libraries, m.Libraries[m.Library[i]])}
				cands[k] = h
			}
			h.Score += 1 / float64(rrfK+r+1)
		}
	}
	// Exact-symbol list. Chunks it references that BM25 missed are loaded.
	seen := map[key]bool{}
	for i, o := range outs {
		for r, id := range o.symChunks {
			row, ch, err := e.shards[i].ChunkRowByID(ctx, id)
			if err != nil || !passes(ch, f) {
				continue
			}
			k := key{i, row}
			if seen[k] {
				continue
			}
			seen[k] = true
			h := cands[k]
			if h == nil {
				h = lightFromChunk(i, row, ch, d.Libraries)
				cands[k] = h
			}
			h.Score += 1 / float64(rrfK+r+1)
			h.SymbolMatch = true
		}
	}
	hits := make([]Hit, 0, len(cands))
	for _, h := range cands {
		hits = append(hits, *h)
	}
	boost(hits, d, q)
	return diversify(hits), dedupeSymbols(syms), active, nil
}

func lightFromChunk(shard int, row int64, c *index.Chunk, libs []string) *Hit {
	trust := map[string]uint8{"official": index.TrustOfficial, "vendor": index.TrustVendor}[c.Trust]
	if c.Trust == "community" {
		trust = index.TrustCommunity
	}
	kind := uint8(slices.Index(index.Kinds, c.Kind))
	return &Hit{Shard: shard, Row: row, DocNum: c.DocNum, Chunk: c, trust: trust, kind: kind,
		alpha: c.Channel == "alpha", suspect: c.Suspect, libMatch: slices.Contains(libs, c.Library)}
}

// hydrate loads full chunks for hits that lack one, one query per shard.
func (e *Engine) hydrate(ctx context.Context, hits []Hit) error {
	var need [8][]int64 // common case: ≤ 8 shards, no allocation for the index
	byShard := need[:0]
	for len(byShard) < len(e.shards) {
		byShard = append(byShard, nil)
	}
	pending := false
	for _, h := range hits {
		if h.Chunk == nil {
			byShard[h.Shard] = append(byShard[h.Shard], h.Row)
			pending = true
		}
	}
	if !pending {
		return nil
	}
	for si, rows := range byShard {
		if len(rows) == 0 {
			continue
		}
		cs, err := e.shards[si].Chunks(ctx, rows)
		if err != nil {
			return err
		}
		for i := range hits {
			if hits[i].Shard == si && hits[i].Chunk == nil {
				hits[i].Chunk = cs[hits[i].Row]
			}
		}
	}
	return nil
}

// passes re-applies the filter to a chunk loaded outside the FTS query.
func passes(c *index.Chunk, f index.Filter) bool {
	if f.Season != "" && c.Season != f.Season {
		return false
	}
	if f.Language != "" && f.Language != "any" && c.Language != f.Language && c.Language != "any" {
		return false
	}
	if !f.IncludeCommunity && c.Trust == "community" {
		return false
	}
	if len(f.Libraries) > 0 && !slices.Contains(f.Libraries, c.Library) {
		return false
	}
	if len(f.Kinds) > 0 && !slices.Contains(f.Kinds, c.Kind) {
		return false
	}
	return true
}

// boost applies deterministic multiplicative boosts (docs/retrieval.md §2).
func boost(hits []Hit, d router.Decision, q Query) {
	for i := range hits {
		h := &hits[i]
		m := 1.0
		if h.SymbolMatch {
			m *= 2.0
		}
		// Trust tiers rank sources when the query names no library. When it
		// does, the named library's docs win: a vendor question ("Motion Magic
		// on a TalonFX") must not be answered by a WPILib page that merely
		// mentions the device.
		switch {
		case len(d.Libraries) > 0 && h.libMatch:
			m *= 1.6
		case len(d.Libraries) > 0:
		case h.trust == index.TrustOfficial:
			m *= 1.2
		case h.trust == index.TrustVendor:
			m *= 1.1
		}
		if h.kind == index.KindCode && d.Intent == router.IntentHowTo {
			m *= 1.2
		}
		// Class reference pages answer symbol questions; the exact symbols are
		// already listed above the hits. For every other intent the guide
		// pages should lead.
		if h.kind == index.KindAPI && d.Intent != router.IntentSymbol {
			m *= 0.5
		}
		if h.kind == index.KindForum && d.Intent != router.IntentTroubleshoot {
			m *= 0.7
		}
		if h.suspect {
			m *= 0.3
		}
		if h.alpha && q.Channel != "alpha" && d.Season == "" && q.Season == "" {
			m *= 0.5
		}
		h.Score *= m
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		if a.Shard != b.Shard { // deterministic ties
			return cmp.Compare(a.Shard, b.Shard)
		}
		return cmp.Compare(a.Row, b.Row)
	})
}

// diversify keeps at most maxPerDoc chunks per source document and caps the
// ranked list.
func diversify(hits []Hit) []Hit {
	type dk struct {
		shard int
		doc   int32
	}
	perDoc := make(map[dk]uint8, len(hits))
	out := hits[:0]
	for _, h := range hits {
		k := dk{h.Shard, h.DocNum}
		if perDoc[k] >= maxPerDoc {
			continue
		}
		perDoc[k]++
		out = append(out, h)
		if len(out) == maxRanked {
			break
		}
	}
	return out
}

// confidence is a logistic over cheap, explainable features:
//
//   - top:      the top fused score relative to the best achievable with the
//     retrievers that actually ran (so prose queries, which only have BM25,
//     are not penalized for lacking a symbol list)
//   - coverage: share of query content terms present in the top chunk
//   - symbol:   an exact API symbol matched
//   - gap:      separation between the first and second result
//
// Coefficients are placeholders until fit on eval/qrels (data/abstain.json,
// M1); they encode "exact or well-covered → confident; lone weak lexical match
// → not".
func confidence(hits []Hit, symbolHit bool, active int, terms []string) float64 {
	best := float64(active) / float64(rrfK+1) * 1.2
	if active > 1 {
		best *= 2.0 // exact-symbol boost is part of the achievable maximum
	}
	top := min(hits[0].Score/best, 1)
	gap := 0.0
	if len(hits) > 1 {
		gap = min((hits[0].Score-hits[1].Score)/best, 1)
	}
	z := -3.0 + 2.0*top + 3.0*coverage(hits[0].Chunk, terms) + 1.0*gap
	if symbolHit || hits[0].SymbolMatch {
		z += 1.5
	}
	return sigmoid(z)
}

func coverage(c *index.Chunk, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	hay := strings.ToLower(c.Title + "\n" + c.HeadingPath + "\n" + c.Symbol + "\n" + c.Body)
	n := 0
	for _, t := range terms {
		if strings.Contains(hay, t) {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}

func dedupeSymbols(in []index.Symbol) []index.Symbol {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := s.Library + "|" + s.Version + "|" + s.Language + "|" + s.FQN + "|" + s.Signature
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// Fetch resolves a chunk id across shards.
func (e *Engine) Fetch(ctx context.Context, id string) (*index.Chunk, error) {
	if !e.Ready() {
		return nil, ErrNotReady
	}
	for _, s := range e.shards {
		c, err := s.ChunkByID(ctx, id)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, index.ErrNotFound) {
			return nil, err
		}
	}
	return nil, index.ErrNotFound
}

// Symbols looks a symbol up across shards.
func (e *Engine) Symbols(ctx context.Context, q index.SymbolQuery) ([]index.Symbol, error) {
	if !e.Ready() {
		return nil, ErrNotReady
	}
	var out []index.Symbol
	for _, s := range e.shards {
		r, err := s.Symbols(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, r...)
	}
	return dedupeSymbols(out), nil
}

type pool[T any] struct{ p sync.Pool }

func (p *pool[T]) get() []T {
	if v, ok := p.p.Get().(*[]T); ok {
		return (*v)[:0]
	}
	return make([]T, 0, ftsDepth)
}

func (p *pool[T]) put(s []T) { s = s[:0]; p.p.Put(&s) }

var scoredPool pool[index.Scored]

// DocExists reports whether a document id (or chunk id "<doc>#<n>") exists.
func (e *Engine) DocExists(ctx context.Context, id string) bool {
	if strings.Contains(id, "#") {
		_, err := e.Fetch(ctx, id)
		return err == nil
	}
	for _, s := range e.shards {
		if s.DocExists(ctx, id) {
			return true
		}
	}
	return false
}

// PackageExists reports whether a Java package / C++ namespace exists.
func (e *Engine) PackageExists(ctx context.Context, pkg, season, language string) bool {
	for _, s := range e.shards {
		if s.PackageExists(ctx, pkg, season, language) {
			return true
		}
	}
	return false
}

// LibraryVersion is the indexed symbol-table version of a library, or "".
func (e *Engine) LibraryVersion(ctx context.Context, library, season, language string) string {
	for _, s := range e.shards {
		if v := s.LibraryVersion(ctx, library, season, language); v != "" {
			return v
		}
	}
	return ""
}

// SymbolsReplacedBy is the reverse migration lookup across shards.
func (e *Engine) SymbolsReplacedBy(ctx context.Context, fqn, season string) []index.Symbol {
	var out []index.Symbol
	for _, s := range e.shards {
		if r, err := s.SymbolsReplacedBy(ctx, fqn, season); err == nil {
			out = append(out, r...)
		}
	}
	return out
}

// MigrationsFrom returns curated rules whose source is fqn (and, with
// members, rules on members "fqn#…"), for a language ("" = any).
func (e *Engine) MigrationsFrom(_ context.Context, fqn, language string, members bool) []index.Migration {
	var out []index.Migration
	pick := func(idx []int) {
		for _, i := range idx {
			if m := e.migrations[i]; language == "" || m.Language == language {
				out = append(out, m)
			}
		}
	}
	pick(e.migFrom[fqn])
	if members {
		for from, idx := range e.migFrom {
			if strings.HasPrefix(from, fqn+"#") {
				pick(idx)
			}
		}
		slices.SortFunc(out, func(a, b index.Migration) int { return strings.Compare(a.From+a.RuleID, b.From+b.RuleID) })
	}
	return out
}

// MigrationsTo returns curated rules whose target is fqn.
func (e *Engine) MigrationsTo(_ context.Context, fqn, language string) []index.Migration {
	var out []index.Migration
	for _, i := range e.migTo[fqn] {
		if m := e.migrations[i]; language == "" || m.Language == language {
			out = append(out, m)
		}
	}
	return out
}

// Migrations returns every curated rule (coverage listing).
func (e *Engine) Migrations() []index.Migration { return e.migrations }

// SymbolSeasons lists the seasons that have API symbol tables, oldest first.
func (e *Engine) SymbolSeasons() []string { return e.symbolSeasons }

// HasSeason reports whether any shard contains the season.
func (e *Engine) HasSeason(season string) bool {
	for _, s := range e.shards {
		for _, sc := range s.Meta().Seasons {
			if sc.Season == season {
				return true
			}
		}
	}
	return false
}

// Metas returns metadata of the loaded shards.
func (e *Engine) Metas() []index.Meta {
	out := make([]index.Meta, len(e.shards))
	for i, s := range e.shards {
		out[i] = s.Meta()
	}
	return out
}
