package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned when a chunk or symbol lookup has no result.
var ErrNotFound = errors.New("index: not found")

// Reader serves one immutable shard. It is safe for concurrent use; all
// statements are prepared once and database/sql caches them per connection.
type Reader struct {
	db   *sql.DB
	meta Meta
	path string

	fts, chunks, byKey, symFQN, symSimple, symMember, docExists, pkgExists, byRepl *sql.Stmt
}

// Meta describes a shard.
type Meta struct {
	Name    string    `json:"name"`
	BuildID string    `json:"build_id"`
	BuiltAt time.Time `json:"built_at"`
	Chunks  int       `json:"chunks"`
	Symbols int       `json:"symbols"`
	Schema  int       `json:"schema_version"`
	// Seasons lists the (season, channel) pairs present, newest first.
	Seasons []SeasonChannel `json:"seasons"`
}

// SeasonChannel is a season and release channel present in a shard.
type SeasonChannel struct {
	Season  string `json:"season"`
	Channel string `json:"channel"`
}

// Open opens a shard read-only and immutable (no locking, no journal, no
// change detection — the file must not change while open), with memory-mapped
// I/O. Connections are capped at GOMAXPROCS.
func Open(ctx context.Context, path string) (*Reader, error) {
	dsn := "file:" + path + "?mode=ro&immutable=1" +
		"&_pragma=query_only(1)&_pragma=mmap_size(1073741824)&_pragma=cache_size(-16384)&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	n := runtime.GOMAXPROCS(0)
	db.SetMaxOpenConns(n)
	db.SetMaxIdleConns(n)
	db.SetConnMaxIdleTime(0)
	r := &Reader{db: db, path: path}
	if err := r.loadMeta(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("index: open %s: %w", path, err)
	}
	if r.meta.Schema != SchemaVersion {
		db.Close()
		return nil, fmt.Errorf("index: %s has schema v%d, this binary reads v%d — run `frc-mcp sync`", path, r.meta.Schema, SchemaVersion)
	}
	if err := r.prepare(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

// Path is the shard file path (vector layers live next to it).
func (r *Reader) Path() string { return r.path }

// Close releases the shard.
func (r *Reader) Close() error { return r.db.Close() }

// Meta returns shard metadata.
func (r *Reader) Meta() Meta { return r.meta }

func (r *Reader) loadMeta(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM meta`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		switch k {
		case metaName:
			r.meta.Name = v
		case metaBuildID:
			r.meta.BuildID = v
		case metaBuiltAt:
			r.meta.BuiltAt, _ = time.Parse(time.RFC3339, v)
		case metaChunks:
			r.meta.Chunks, _ = strconv.Atoi(v)
		case metaSymbols:
			r.meta.Symbols, _ = strconv.Atoi(v)
		case metaSchema:
			r.meta.Schema, _ = strconv.Atoi(v)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sr, err := r.db.QueryContext(ctx, `SELECT DISTINCT season, channel FROM chunk ORDER BY season DESC, channel DESC`)
	if err != nil {
		return err
	}
	defer sr.Close()
	for sr.Next() {
		var sc SeasonChannel
		if err := sr.Scan(&sc.Season, &sc.Channel); err != nil {
			return err
		}
		r.meta.Seasons = append(r.meta.Seasons, sc)
	}
	return sr.Err()
}

const chunkCols = `c.id, c.doc_id, c.doc_num, c.ord, c.library, c.version_lo, c.version_hi, c.season, c.channel, c.language,
	c.kind, c.title, c.heading_path, c.symbol, c.prefix, c.body, c.source_url, c.anchor, c.upstream_rev,
	c.retrieved_at, c.license, c.trust, c.suspect, c.authority, c.tokens`

const symbolCols = `fqn, library, version, season, language, kind, signature, summary, since, deprecated_in,
	removed_in, replacement, chunk_id, source_url, upstream_rev, retrieved_at, license, trust`

// Filter predicates shared by FTS and symbol queries. Empty string / empty
// JSON array means "no constraint". Parameters are positional:
//
//	?2 season  ?3 language  ?4 include community (0/1)  ?5 libraries JSON  ?6 kinds JSON
const chunkFilter = `
	AND (?2 = '' OR c.season = ?2)
	AND (?3 = '' OR ?3 = 'any' OR c.language = ?3 OR c.language = 'any')
	AND (?4 = 1 OR c.trust <> 'community')
	AND (?5 = '[]' OR c.library IN (SELECT value FROM json_each(?5)))
	AND (?6 = '[]' OR c.kind IN (SELECT value FROM json_each(?6)))`

func (r *Reader) prepare(ctx context.Context) error {
	for _, p := range []struct {
		dst **sql.Stmt
		q   string
	}{
		{&r.fts, `SELECT c.id, bm25(chunk_fts, ` + ftsWeights + `) AS s, c.doc_num,
				CASE c.trust WHEN 'official' THEN 0 WHEN 'vendor' THEN 1 ELSE 2 END,
				CASE c.kind WHEN 'prose' THEN 0 WHEN 'code' THEN 1 WHEN 'api' THEN 2 WHEN 'release' THEN 3
					WHEN 'rule' THEN 4 ELSE 5 END,
				c.channel = 'alpha', c.suspect,
				c.library IN (SELECT value FROM json_each(?8)),
				c.symbol_key <> '' AND c.symbol_key IN (SELECT lower(value) FROM json_each(?9))
			FROM chunk_fts JOIN chunk c ON c.id = chunk_fts.rowid
			WHERE chunk_fts MATCH ?1` + chunkFilter + `
			ORDER BY s LIMIT ?7`},
		{&r.chunks, `SELECT ` + chunkCols + ` FROM chunk c WHERE c.id IN (SELECT value FROM json_each(?1))`},
		{&r.byKey, `SELECT ` + chunkCols + ` FROM chunk c WHERE c.doc_id = ?1 AND c.ord = ?2`},
		{&r.docExists, `SELECT 1 FROM chunk WHERE doc_id = ?1 LIMIT 1`},
		{&r.pkgExists, `SELECT 1 FROM symbol WHERE fqn >= ?1 AND fqn < ?2 AND (?3 = '' OR season = ?3) AND language = ?4 LIMIT 1`},
		{&r.byRepl, `SELECT ` + symbolCols + ` FROM symbol WHERE replacement = ?1 AND (?2 = '' OR season = ?2) LIMIT 5`},
		{&r.symFQN, `SELECT ` + symbolCols + ` FROM symbol WHERE fqn = ?1 AND (?2 = '' OR season = ?2)
			AND (?3 = '' OR ?3 = 'any' OR language = ?3) ORDER BY season DESC, fqn, signature LIMIT ?4`},
		{&r.symSimple, `SELECT ` + symbolCols + ` FROM symbol WHERE simple = ?1 AND (?2 = '' OR season = ?2)
			AND (?3 = '' OR ?3 = 'any' OR language = ?3) ORDER BY season DESC, kind <> 'class', fqn, signature LIMIT ?4`},
		{&r.symMember, `SELECT ` + symbolCols + ` FROM symbol WHERE owner = ?1 AND simple = ?2 AND (?3 = '' OR season = ?3)
			AND (?4 = '' OR ?4 = 'any' OR language = ?4) ORDER BY season DESC, fqn, signature LIMIT ?5`},
	} {
		st, err := r.db.PrepareContext(ctx, p.q)
		if err != nil {
			return fmt.Errorf("index: prepare: %w", err)
		}
		*p.dst = st
	}
	return nil
}

// Filter restricts a query. Zero values mean "unconstrained".
type Filter struct {
	Season           string
	Language         string
	Libraries        []string
	Kinds            []string
	IncludeCommunity bool
}

// Scored is a light FTS result: everything ranking needs, as integers, so a
// candidate costs no string allocation. Bodies are loaded later (Chunks) only
// for the results actually displayed.
type Scored struct {
	Row      int64
	Score    float64 // bm25: lower is better; only rank order is used
	DocNum   int32
	Trust    uint8 // TrustOfficial | TrustVendor | TrustCommunity
	Kind     uint8 // KindProse … KindForum
	Alpha    bool
	Suspect  bool
	LibMatch bool // library ∈ Boost.Libraries
	SymMatch bool // chunk's own symbol simple name ∈ Boost.Symbols
}

// Boost carries the router signals evaluated inside the FTS query.
type Boost struct {
	Libraries []string
	Symbols   []string // simple names, any case
}

// FTS runs a MATCH query (already built by textutil.FTSQuery) and returns up
// to limit light rows in rank order. dst is reused to avoid allocation.
func (r *Reader) FTS(ctx context.Context, match string, f Filter, b Boost, limit int, dst []Scored) ([]Scored, error) {
	dst = dst[:0]
	if match == "" {
		return dst, nil
	}
	rows, err := r.fts.QueryContext(ctx, match, f.Season, f.Language, b2i(f.IncludeCommunity),
		jsonList(f.Libraries), jsonList(f.Kinds), limit, jsonList(b.Libraries), jsonList(b.Symbols))
	if err != nil {
		return dst, fmt.Errorf("index: fts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s Scored
		if err := rows.Scan(&s.Row, &s.Score, &s.DocNum, &s.Trust, &s.Kind, &s.Alpha, &s.Suspect, &s.LibMatch, &s.SymMatch); err != nil {
			return dst, err
		}
		dst = append(dst, s)
	}
	return dst, rows.Err()
}

// Chunks loads the given rows. Missing rows are omitted.
func (r *Reader) Chunks(ctx context.Context, ids []int64) (map[int64]*Chunk, error) {
	out := make(map[int64]*Chunk, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	b, _ := json.Marshal(ids)
	rows, err := r.chunks.QueryContext(ctx, string(b))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		id, c, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// PackageExists reports whether any symbol lives in pkg (Java package or
// C++ namespace prefix) for a season ("" = any) and language.
func (r *Reader) PackageExists(ctx context.Context, pkg, season, language string) bool {
	var one int
	return r.pkgExists.QueryRowContext(ctx, pkg+".", pkg+"/", season, language).Scan(&one) == nil
}

// SymbolsReplacedBy finds symbols whose generated or upstream Replacement is
// fqn (reverse of the migration map: which older API became this one).
func (r *Reader) SymbolsReplacedBy(ctx context.Context, fqn, season string) ([]Symbol, error) {
	rows, err := r.byRepl.QueryContext(ctx, fqn, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var s Symbol
		var at int64
		if err := rows.Scan(&s.FQN, &s.Library, &s.Version, &s.Season, &s.Language, &s.Kind, &s.Signature,
			&s.Summary, &s.Since, &s.DeprecatedIn, &s.RemovedIn, &s.Replacement, &s.ChunkID, &s.SourceURL,
			&s.UpstreamRev, &at, &s.License, &s.Trust); err != nil {
			return nil, err
		}
		s.RetrievedAt = time.Unix(at, 0).UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// LibraryVersion returns the version of a library's symbol table for a season
// and language, or "" when the shard has none.
func (r *Reader) LibraryVersion(ctx context.Context, library, season, language string) string {
	var v string
	_ = r.db.QueryRowContext(ctx, `SELECT version FROM symbol WHERE library = ? AND season = ? AND language = ? LIMIT 1`,
		library, season, language).Scan(&v)
	return v
}

// Licenses lists the distinct chunk licenses in the shard (publish policy).
func (r *Reader) Licenses(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT license FROM chunk ORDER BY license`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DocExists reports whether any chunk belongs to docID.
func (r *Reader) DocExists(ctx context.Context, docID string) bool {
	var one int
	return r.docExists.QueryRowContext(ctx, docID).Scan(&one) == nil
}

// ChunkByID resolves a public chunk id ("<doc_id>#<ord>").
func (r *Reader) ChunkByID(ctx context.Context, id string) (*Chunk, error) {
	_, c, err := r.ChunkRowByID(ctx, id)
	return c, err
}

// ChunkRowByID is ChunkByID that also returns the row id.
func (r *Reader) ChunkRowByID(ctx context.Context, id string) (int64, *Chunk, error) {
	docID, ord, err := ParseID(id)
	if err != nil {
		return 0, nil, err
	}
	rows, err := r.byKey.QueryContext(ctx, docID, ord)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, nil, err
		}
		return 0, nil, ErrNotFound
	}
	return scanChunk(rows)
}

// SymbolQuery is an exact symbol lookup.
type SymbolQuery struct {
	// Name is an FQN ("edu.wpi.first.wpilibj.TimedRobot", "pkg.Type#method",
	// "frc::DCMotor"), "Type#member", or a simple name ("SparkMax").
	Name     string
	Season   string
	Language string
	Limit    int
	// Exact disables the simple-name fallback for qualified names (the
	// verifier must never treat "edu.wpi.first.X" as found because some
	// other package declares X).
	Exact bool
}

// Symbols performs an exact lookup, trying FQN, then Type#member, then simple
// name (case-insensitive).
func (r *Reader) Symbols(ctx context.Context, q SymbolQuery) ([]Symbol, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	name := strings.TrimSpace(q.Name)
	var rows *sql.Rows
	var err error
	isQualified := strings.ContainsAny(name, ".") || strings.Contains(name, "::")
	switch hash := strings.LastIndexByte(name, '#'); {
	case isQualified:
		rows, err = r.symFQN.QueryContext(ctx, name, q.Season, q.Language, q.Limit)
	case hash > 0:
		rows, err = r.symMember.QueryContext(ctx, name[:hash], name[hash+1:], q.Season, q.Language, q.Limit)
	default:
		rows, err = r.symSimple.QueryContext(ctx, name, q.Season, q.Language, q.Limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var s Symbol
		var at int64
		if err := rows.Scan(&s.FQN, &s.Library, &s.Version, &s.Season, &s.Language, &s.Kind, &s.Signature,
			&s.Summary, &s.Since, &s.DeprecatedIn, &s.RemovedIn, &s.Replacement, &s.ChunkID, &s.SourceURL,
			&s.UpstreamRev, &at, &s.License, &s.Trust); err != nil {
			return nil, err
		}
		s.RetrievedAt = time.Unix(at, 0).UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A qualified name that is not an FQN may still be Type#member written as
	// "Type.member" or a C++ "ns::Type::member"; fall back to simple lookup.
	if len(out) == 0 && isQualified && !strings.Contains(name, "#") && !q.Exact {
		// "frc::TimedRobot::AddPeriodic" / "edu.wpi.first.wpilibj.Timer.get":
		// try the member form of the FQN first, then the simple name.
		sep := strings.LastIndex(name, "::")
		width := 2
		if dot := strings.LastIndexByte(name, '.'); dot > sep {
			sep, width = dot, 1
		}
		if sep > 0 {
			if m, err := r.Symbols(ctx, SymbolQuery{Name: name[:sep] + "#" + name[sep+width:], Season: q.Season,
				Language: q.Language, Limit: q.Limit, Exact: true}); err == nil && len(m) > 0 {
				return m, nil
			}
		}
		return r.Symbols(ctx, SymbolQuery{Name: SimpleName(name), Season: q.Season, Language: q.Language, Limit: q.Limit})
	}
	return out, nil
}

type scanner interface{ Scan(...any) error }

func scanChunk(s scanner) (int64, *Chunk, error) {
	var c Chunk
	var id, at int64
	var suspect int
	err := s.Scan(&id, &c.DocID, &c.DocNum, &c.Ord, &c.Library, &c.VersionLo, &c.VersionHi, &c.Season, &c.Channel,
		&c.Language, &c.Kind, &c.Title, &c.HeadingPath, &c.Symbol, &c.Prefix, &c.Body, &c.SourceURL, &c.Anchor,
		&c.UpstreamRev, &at, &c.License, &c.Trust, &suspect, &c.Authority, &c.Tokens)
	c.RetrievedAt = time.Unix(at, 0).UTC()
	c.Suspect = suspect != 0
	return id, &c, err
}

func jsonList(xs []string) string {
	if len(xs) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(xs)
	return string(b)
}
