// Package build is the ingestion-plane orchestrator for `frc-mcp index run`:
//
//	sources.yaml → fetch (conditional GET via netguard) → adapter parse
//	  → cross-season symbol diff → embed → shard + vector layer (atomic)
//
// It is deterministic for identical upstream bytes: same input, same build id.
package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/apisym"
	"github.com/fikretyukselit/frc-mcp/internal/embed/m2v"
	"github.com/fikretyukselit/frc-mcp/internal/hwdata"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/dcmotor"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/discourse"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/doxygen"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/ghreleases"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/gitbook"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/javadoc"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/pypi"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/recalc"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/repomd"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/specpage"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/sphinx"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/vendordeps"
	"github.com/fikretyukselit/frc-mcp/internal/migrate"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/vec"
)

// Options configures a build run.
type Options struct {
	Registry  *sources.Registry
	OutDir    string // shard output directory (also receives models/)
	CacheDir  string // fetch cache
	Version   string // binary version for the User-Agent
	SourceIDs []string
	// MigrationsDir holds the curated rules (data/migrations); "" skips them.
	MigrationsDir string
	// HardwareDir holds the curated hardware specs (data/hardware); "" skips them.
	HardwareDir string
	Embed       bool // compute vector layers with the registry's first model
	Log         *slog.Logger
}

// Report summarizes a run.
type Report struct {
	Sources []SourceReport `json:"sources"`
	Shards  []ShardReport  `json:"shards"`
	Diffs   []DiffReport   `json:"diffs"`
	// Migrations counts curated rules written; rules whose from-season table
	// is not part of this run are skipped (that shard is not rebuilt).
	Migrations        int `json:"migrations"`
	MigrationsSkipped int `json:"migrations_skipped,omitempty"`
	// HardwareCurated counts data/hardware rows written; rows whose shard is
	// not part of this run are skipped (that shard is not rebuilt).
	HardwareCurated        int    `json:"hardware_curated,omitempty"`
	HardwareCuratedSkipped int    `json:"hardware_curated_skipped,omitempty"`
	Model                  string `json:"model,omitempty"`
	Elapsed                string `json:"elapsed"`
}

type SourceReport struct {
	ID          string `json:"id"`
	NotModified bool   `json:"not_modified"`
	Bytes       int64  `json:"bytes"`
	Chunks      int    `json:"chunks"`
	Symbols     int    `json:"symbols"`
	Rev         string `json:"rev"`
	// Forum feeds: posts flagged by the suspect detector, and items outside
	// the allowed categories.
	Suspect  int `json:"suspect,omitempty"`
	Filtered int `json:"filtered,omitempty"`
}

type ShardReport struct {
	Name       string `json:"name"`
	Chunks     int    `json:"chunks"`
	Symbols    int    `json:"symbols"`
	Rejected   int    `json:"rejected"`
	Vectors    int    `json:"vectors"`
	Vendordeps int    `json:"vendordeps,omitempty"`
	Releases   int    `json:"releases,omitempty"`
	HWSpecs    int    `json:"hw_specs,omitempty"`
	BuildID    string `json:"build_id"`
}

type DiffReport struct {
	Library, Language, From, To string
	Removed, Added, Mapped      int
}

type shardData struct {
	name       string
	chunks     []index.Chunk
	symbols    []index.Symbol
	vendordeps []index.Vendordep
	releases   []index.Release
	hwspecs    []index.HWSpec
	migrations []index.Migration
}

// Run executes a build.
func Run(ctx context.Context, opt Options) (*Report, error) {
	start := time.Now()
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	reg := opt.Registry
	f := fetch.New(opt.CacheDir, reg.UA(opt.Version), reg.Hosts(), 512<<20)
	rep := &Report{}
	shards := map[string]*shardData{}
	shardSrc := map[string]sources.Source{}

	for _, src := range reg.Select(opt.SourceIDs...) {
		t0 := time.Now()
		res, err := f.GetFresh(ctx, src.URL, src.Interval())
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", src.ID, err)
		}
		rev := strings.Trim(res.ETag, `"W/`)
		if rev == "" {
			rev = "sha256:" + res.SHA256[:16]
		}
		sd := shards[src.Shard]
		if sd == nil {
			sd = &shardData{name: src.Shard}
			shards[src.Shard] = sd
			shardSrc[src.Shard] = src
		}
		sr := SourceReport{ID: src.ID, NotModified: res.NotModified, Bytes: res.Size, Rev: rev}
		addChunk := func(c index.Chunk) error { sd.chunks = append(sd.chunks, c); sr.Chunks++; return nil }
		addHW := func(h index.HWSpec) error { sd.hwspecs = append(sd.hwspecs, h); sr.Symbols++; return nil }
		switch src.Adapter {
		case "sphinx-htmlzip":
			_, err = sphinx.Parse(res.Path, src, rev, res.FetchedAt, addChunk)
		case "vendordep-catalog":
			_, err = vendordeps.Parse(ctx, f, src, res.FetchedAt,
				func(v index.Vendordep) error { sd.vendordeps = append(sd.vendordeps, v); sr.Symbols++; return nil }, addChunk)
		case "github-markdown":
			_, err = repomd.Parse(ctx, f, res.Path, src, res.FetchedAt, addChunk)
		case "gitbook-llms":
			_, err = gitbook.Parse(ctx, f, res.Path, src, res.FetchedAt, addChunk)
		case "github-releases":
			_, err = ghreleases.Parse(ctx, f, res.Path, src, res.FetchedAt,
				func(r index.Release) error { sd.releases = append(sd.releases, r); sr.Symbols++; return nil }, addChunk)
		case "pypi-wheel":
			_, err = pypi.Parse(ctx, f, res.Path, src, res.FetchedAt,
				func(s index.Symbol) error { sd.symbols = append(sd.symbols, s); sr.Symbols++; return nil }, addChunk)
		case "doxygen-zip":
			_, err = doxygen.Parse(res.Path, src, rev, res.FetchedAt,
				func(s index.Symbol) error { sd.symbols = append(sd.symbols, s); sr.Symbols++; return nil }, addChunk)
		case "wpilib-dcmotor":
			_, err = dcmotor.Parse(res.Path, src, rev, res.FetchedAt, addHW)
		case "recalc-motors":
			_, err = recalc.Parse(res.Path, src, res.FetchedAt, addHW)
		case "gitbook-spec-table":
			_, err = specpage.Parse(res.Path, src, rev, res.FetchedAt, addHW)
		case "javadoc-zip":
			_, err = javadoc.Parse(res.Path, src, rev, res.FetchedAt,
				func(s index.Symbol) error { sd.symbols = append(sd.symbols, s); sr.Symbols++; return nil }, addChunk)
		case "discourse-rss":
			var st discourse.Stats
			st, err = discourse.Parse(ctx, f.Within(src.Interval()), res.Path, src, res.FetchedAt, addChunk)
			sr.Suspect, sr.Filtered = st.Suspect, st.Filtered
			log.Info("forum feed", "id", src.ID, "topics", st.Topics, "replies", st.Replies, "suspect", st.Suspect,
				"filtered", st.Filtered, "duplicates", st.Duplicates, "invalid", st.Invalid, "empty", st.Empty)
		default:
			err = fmt.Errorf("unknown adapter %q", src.Adapter)
		}
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", src.ID, err)
		}
		log.Info("source parsed", "id", src.ID, "not_modified", res.NotModified, "chunks", sr.Chunks,
			"symbols", sr.Symbols, "ms", time.Since(t0).Milliseconds())
		rep.Sources = append(rep.Sources, sr)
	}

	rep.Diffs = diffSeasons(shards, shardSrc)
	if opt.MigrationsDir != "" {
		rules, err := migrate.Load(opt.MigrationsDir)
		if err != nil {
			return nil, err
		}
		if rep.Migrations, rep.MigrationsSkipped, err = applyMigrations(shards, rules); err != nil {
			return nil, err
		}
	}

	if opt.HardwareDir != "" {
		rows, err := hwdata.Load(opt.HardwareDir)
		if err != nil {
			return nil, err
		}
		if rep.HardwareCurated, rep.HardwareCuratedSkipped, err = applyHardware(shards, rows); err != nil {
			return nil, err
		}
	}

	var model *m2v.Model
	if opt.Embed && len(reg.Models) > 0 {
		m, err := ensureModel(ctx, f, reg.Models[0], opt.OutDir)
		if err != nil {
			return nil, err
		}
		model, rep.Model = m, m.ID
	}

	names := make([]string, 0, len(shards))
	for n := range shards {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sr, err := writeShard(ctx, opt.OutDir, shards[n], model)
		if err != nil {
			return nil, fmt.Errorf("shard %s: %w", n, err)
		}
		log.Info("shard written", "name", n, "chunks", sr.Chunks, "symbols", sr.Symbols, "rejected", sr.Rejected, "vectors", sr.Vectors)
		rep.Shards = append(rep.Shards, *sr)
	}
	rep.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return rep, nil
}

// diffSeasons runs apisym.Diff over consecutive seasons of each
// (library, language) symbol table.
func diffSeasons(shards map[string]*shardData, _ map[string]sources.Source) []DiffReport {
	// Group symbols by (library, language, season) across shards: one shard
	// may hold several libraries (vendor-java-2026), and one library-season
	// may span several sources (photonlib + photontargeting).
	type key struct{ lib, lang string }
	type ref struct {
		sd *shardData
		i  int
	}
	groups := map[key]map[string][]ref{}
	names := make([]string, 0, len(shards))
	for name := range shards {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic grouping order
	for _, name := range names {
		sd := shards[name]
		for i := range sd.symbols {
			s := &sd.symbols[i]
			k := key{s.Library, s.Language}
			if groups[k] == nil {
				groups[k] = map[string][]ref{}
			}
			groups[k][s.Season] = append(groups[k][s.Season], ref{sd, i})
		}
	}
	var out []DiffReport
	for k, bySeason := range groups {
		seasons := make([]string, 0, len(bySeason))
		for se := range bySeason {
			seasons = append(seasons, se)
		}
		sort.Strings(seasons)
		for i := 1; i < len(seasons); i++ {
			oldRefs, newRefs := bySeason[seasons[i-1]], bySeason[seasons[i]]
			old, nu := make([]index.Symbol, len(oldRefs)), make([]index.Symbol, len(newRefs))
			for j, r := range oldRefs {
				old[j] = r.sd.symbols[r.i]
			}
			for j, r := range newRefs {
				nu[j] = r.sd.symbols[r.i]
			}
			from, to := old[0].Version, nu[0].Version
			r, a, m := apisym.Diff(old, nu, to)
			for j, rf := range oldRefs {
				rf.sd.symbols[rf.i] = old[j]
			}
			for j, rf := range newRefs {
				rf.sd.symbols[rf.i] = nu[j]
			}
			out = append(out, DiffReport{Library: k.lib, Language: k.lang, From: from, To: to, Removed: r, Added: a, Mapped: m})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Library+out[i].To < out[j].Library+out[j].To })
	return out
}

// applyMigrations validates curated rules against the symbol tables of this
// run and writes each into the shard holding its from-season table. Curated
// replacements override upstream and generated ones. A rule naming a symbol
// that the table lacks fails the build: rules must track upstream exactly.
func applyMigrations(shards map[string]*shardData, rules []index.Migration) (written, skipped int, err error) {
	type tkey struct{ lib, lang, season string }
	type ref struct {
		sd *shardData
		i  int
	}
	tables := map[tkey]map[string][]ref{}
	names := make([]string, 0, len(shards))
	for n := range shards {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sd := shards[n]
		for i := range sd.symbols {
			s := &sd.symbols[i]
			k := tkey{s.Library, s.Language, s.Season}
			if tables[k] == nil {
				tables[k] = map[string][]ref{}
			}
			tables[k][s.FQN] = append(tables[k][s.FQN], ref{sd, i})
		}
	}
	var bad []string
	for _, m := range rules {
		from := tables[tkey{m.Library, m.Language, m.FromSeason}]
		if from == nil {
			skipped++
			continue
		}
		refs := from[m.From]
		if len(refs) == 0 {
			bad = append(bad, fmt.Sprintf("%s (%s): %s not in the %s table", m.RuleID, m.Language, m.From, m.FromSeason))
			continue
		}
		if to := tables[tkey{m.Library, m.Language, m.ToSeason}]; to != nil && m.To != "" && len(to[m.To]) == 0 {
			bad = append(bad, fmt.Sprintf("%s (%s): %s not in the %s table", m.RuleID, m.Language, m.To, m.ToSeason))
			continue
		}
		if m.To != "" && m.To != m.From {
			for _, r := range refs {
				s := &r.sd.symbols[r.i]
				s.Replacement, s.ReplacementSrc = m.To, "curated"
			}
		}
		refs[0].sd.migrations = append(refs[0].sd.migrations, m)
		written++
	}
	if len(bad) > 0 {
		return 0, 0, fmt.Errorf("migrate: %d curated rule(s) do not match the symbol tables:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
	return written, skipped, nil
}

// applyHardware adds the curated hardware rows to their shard when that shard
// is built in this run (a partial run must not overwrite the full shard with
// only the curated rows), and fails on a (part, source, season) that two
// sources both produce: hw_spec rows are never silently replaced.
func applyHardware(shards map[string]*shardData, rows []hwdata.Row) (written, skipped int, err error) {
	for _, r := range rows {
		sd := shards[r.Shard]
		if sd == nil {
			skipped++
			continue
		}
		sd.hwspecs = append(sd.hwspecs, r.Spec)
		written++
	}
	names := make([]string, 0, len(shards))
	for n := range shards {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		seen := map[string]bool{}
		for _, h := range shards[n].hwspecs {
			k := h.Part + "/" + h.Source + "/" + h.Season
			if seen[k] {
				return 0, 0, fmt.Errorf("hardware: shard %s has two rows for part %s from source %s (%s)", n, h.Part, h.Source, h.Season)
			}
			seen[k] = true
		}
	}
	return written, skipped, nil
}

// writeShard writes <name>.sqlite and <name>.<model>.vec atomically (tmp +
// rename) so a running server never sees a half-written shard.
func writeShard(ctx context.Context, dir string, sd *shardData, model *m2v.Model) (*ShardReport, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	final := filepath.Join(dir, sd.name+".sqlite")
	tmp := final + ".tmp"
	w, err := index.Create(ctx, tmp, sd.name)
	if err != nil {
		return nil, err
	}
	rep := &ShardReport{Name: sd.name}
	var vectors [][]float32
	for _, c := range sd.chunks {
		row, stored, err := w.Add(ctx, c)
		if errors.Is(err, index.ErrInvalidChunk) {
			rep.Rejected++
			continue
		}
		if err != nil {
			return nil, err
		}
		rep.Chunks++
		if model != nil {
			if int(row) != len(vectors)+1 {
				return nil, fmt.Errorf("row %d out of sequence (have %d vectors)", row, len(vectors))
			}
			vectors = append(vectors, model.Encode(EmbedText(&stored), nil))
		}
	}
	for _, s := range sd.symbols {
		if err := w.AddSymbol(ctx, s); err != nil {
			if errors.Is(err, index.ErrInvalidChunk) {
				rep.Rejected++
				continue
			}
			// Duplicate (fqn, signature) pairs occur for overloads Javadoc
			// renders identically; keep the first.
			if strings.Contains(err.Error(), "UNIQUE") {
				continue
			}
			return nil, err
		}
		rep.Symbols++
	}
	for _, v := range sd.vendordeps {
		if err := w.AddVendordep(ctx, v); err != nil {
			if errors.Is(err, index.ErrInvalidChunk) {
				rep.Rejected++
				continue
			}
			return nil, err
		}
		rep.Vendordeps++
	}
	for _, r := range sd.releases {
		if err := w.AddRelease(ctx, r); err != nil {
			if errors.Is(err, index.ErrInvalidChunk) {
				rep.Rejected++
				continue
			}
			return nil, err
		}
		rep.Releases++
	}
	for _, h := range sd.hwspecs {
		if err := w.AddHWSpec(ctx, h); err != nil {
			if errors.Is(err, index.ErrInvalidChunk) {
				rep.Rejected++
				continue
			}
			return nil, err
		}
		rep.HWSpecs++
	}
	for _, m := range sd.migrations {
		if err := w.AddMigration(ctx, m); err != nil {
			return nil, err
		}
	}
	if err := w.Close(ctx); err != nil {
		return nil, err
	}
	if model != nil {
		vp := index.VectorPath(final, model.ID)
		if err := vec.Write(vp+".tmp", model.ID, model.Dims, vectors); err != nil {
			return nil, err
		}
		if err := os.Rename(vp+".tmp", vp); err != nil {
			return nil, err
		}
		rep.Vectors = len(vectors)
	}
	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	r, err := index.Open(ctx, final)
	if err != nil {
		return nil, err
	}
	rep.BuildID = r.Meta().BuildID
	r.Close()
	return rep, nil
}

// EmbedText is the text a chunk is embedded as: its context prefix plus body.
// Query-time and index-time must agree on the model, not on this format.
func EmbedText(c *index.Chunk) string {
	return c.Prefix + "\n" + c.Body
}

// ensureModel downloads model files (cached, conditional GET) into
// <out>/models/<id>/ and loads the model.
func ensureModel(ctx context.Context, f *fetch.Fetcher, m sources.Model, out string) (*m2v.Model, error) {
	dir := index.ModelDir(out, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(m.Files))
	for n := range m.Files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, name := range names {
		if name != filepath.Base(name) || name == "." || name == ".." {
			return nil, fmt.Errorf("model %s: file name %q must be a plain file name", m.ID, name)
		}
		res, err := f.Get(ctx, m.Files[name])
		if err != nil {
			return nil, fmt.Errorf("model %s: %w", m.ID, err)
		}
		b, err := os.ReadFile(res.Path)
		if err != nil {
			return nil, err
		}
		//nolint:gosec // G703: name is validated above as a plain file name from the operator's sources.yaml
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return nil, err
		}
	}
	return m2v.Load(dir, m.ID)
}
