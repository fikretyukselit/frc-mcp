package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/ingest/sanitize"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"

	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite"), FTS5 included
)

// Writer builds a shard. It is single-use and not safe for concurrent use:
// Create → Add* → Close. A shard that fails validation is never finalized.
type Writer struct {
	db      *sql.DB
	tx      *sql.Tx
	chunk   *sql.Stmt
	symbol  *sql.Stmt
	path    string
	name    string
	h       hash.Hash
	nChunk  int
	nSymbol int
	docNums map[string]int
}

// Create starts a new shard at path, replacing any existing file.
func Create(ctx context.Context, path, name string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)&_pragma=page_size(8192)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		db.Close()
		return nil, fmt.Errorf("index: create schema: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, err
	}
	w := &Writer{db: db, tx: tx, path: path, name: name, h: sha256.New(), docNums: map[string]int{}}
	w.chunk, err = tx.PrepareContext(ctx, `INSERT INTO chunk (doc_id, doc_num, ord, library, version_lo, version_hi,
		season, channel, language, kind, title, heading_path, symbol, symbol_key, prefix, expand, body, source_url,
		anchor, upstream_rev, retrieved_at, license, trust, suspect, authority, tokens)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		w.abort()
		return nil, err
	}
	w.symbol, err = tx.PrepareContext(ctx, `INSERT INTO symbol (fqn, simple, owner, library, version, season, language,
		kind, signature, summary, since, deprecated_in, removed_in, replacement, chunk_id, source_url, upstream_rev,
		retrieved_at, license, trust) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		w.abort()
		return nil, err
	}
	return w, nil
}

// AddChunk sanitizes, validates and appends a chunk. Title, heading path,
// symbol and body are cleaned of hidden-text vectors; Suspect is OR-ed with
// the detector's verdict so an upstream flag is never cleared.
func (w *Writer) AddChunk(ctx context.Context, c Chunk) error {
	_, _, err := w.Add(ctx, c)
	return err
}

// Add is AddChunk returning the stored row id (1-based, dense: row i+1 is the
// i-th accepted chunk, which aligns vector layers) and the sanitized chunk.
func (w *Writer) Add(ctx context.Context, c Chunk) (int64, Chunk, error) {
	c.Title = sanitize.Clean(c.Title)
	c.HeadingPath = sanitize.Clean(c.HeadingPath)
	c.Body = sanitize.Clean(c.Body)
	c.Suspect = c.Suspect || sanitize.Suspect(c.Title) || sanitize.Suspect(c.Body)
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix(&c)
	}
	if c.Tokens == 0 {
		c.Tokens = textutil.EstimateTokens(c.Body)
	}
	if err := c.Validate(); err != nil {
		return 0, c, err
	}
	expand := textutil.Expand(c.Symbol, c.Title, c.Body)
	num, ok := w.docNums[c.DocID]
	if !ok {
		num = len(w.docNums)
		w.docNums[c.DocID] = num
	}
	symKey := ""
	if c.Symbol != "" {
		symKey = strings.ToLower(SimpleName(c.Symbol))
	}
	res, err := w.chunk.ExecContext(ctx, c.DocID, num, c.Ord, c.Library, c.VersionLo, c.VersionHi, c.Season,
		c.Channel, c.Language, c.Kind, c.Title, c.HeadingPath, c.Symbol, symKey, c.Prefix, expand, c.Body, c.SourceURL,
		c.Anchor, c.UpstreamRev, c.RetrievedAt.Unix(), c.License, c.Trust, b2i(c.Suspect), c.Authority, c.Tokens)
	if err != nil {
		return 0, c, fmt.Errorf("index: insert %s: %w", c.ID(), err)
	}
	row, err := res.LastInsertId()
	if err != nil {
		return 0, c, err
	}
	w.digest(c)
	w.nChunk++
	return row, c, nil
}

// AddSymbol validates and appends an API symbol.
func (w *Writer) AddSymbol(ctx context.Context, s Symbol) error {
	s.Summary = sanitize.Clean(s.Summary)
	if err := s.Validate(); err != nil {
		return err
	}
	if _, err := w.symbol.ExecContext(ctx, s.FQN, SimpleName(s.FQN), OwnerName(s.FQN), s.Library, s.Version,
		s.Season, s.Language, s.Kind, s.Signature, s.Summary, s.Since, s.DeprecatedIn, s.RemovedIn, s.Replacement,
		s.ChunkID, s.SourceURL, s.UpstreamRev, s.RetrievedAt.Unix(), s.License, s.Trust); err != nil {
		return fmt.Errorf("index: insert symbol %s: %w", s.FQN, err)
	}
	w.digest(s)
	w.nSymbol++
	return nil
}

// Close finalizes the shard: builds and optimizes the FTS index, records
// metadata, analyzes and vacuums. The resulting file is immutable by contract.
func (w *Writer) Close(ctx context.Context) error {
	defer w.db.Close()
	if err := w.tx.Commit(); err != nil {
		return err
	}
	id := hex.EncodeToString(w.h.Sum(nil))[:32]
	stmts := []string{
		`INSERT INTO chunk_fts(chunk_fts) VALUES ('rebuild')`,
		`INSERT INTO chunk_fts(chunk_fts) VALUES ('optimize')`,
	}
	for _, s := range stmts {
		if _, err := w.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("index: fts: %w", err)
		}
	}
	meta := map[string]string{
		metaSchema: strconv.Itoa(SchemaVersion), metaName: w.name, metaBuildID: id,
		metaBuiltAt: time.Now().UTC().Format(time.RFC3339), metaChunks: strconv.Itoa(w.nChunk),
		metaSymbols: strconv.Itoa(w.nSymbol),
	}
	for k, v := range meta {
		if _, err := w.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}
	// sqlite_stat4 samples make the planner read bound parameter values, and
	// SQLite then re-prepares a statement whenever those values change, i.e.
	// on every search (measured: ~35% of search CPU). stat1 is enough for
	// these small, immutable shards.
	for _, s := range []string{`ANALYZE`, `DELETE FROM sqlite_stat4`, `VACUUM`} {
		if s == `DELETE FROM sqlite_stat4` {
			var n int
			if err := w.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'sqlite_stat4'`).Scan(&n); err != nil {
				return fmt.Errorf("index: stat4: %w", err)
			}
			if n == 0 {
				continue // SQLite built without STAT4
			}
		}
		if _, err := w.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("index: %s: %w", strings.ToLower(s), err)
		}
	}
	return nil
}

func (w *Writer) abort() {
	if w.tx != nil {
		_ = w.tx.Rollback()
	}
	_ = w.db.Close()
	_ = os.Remove(w.path)
}

// digest folds every record into the build id so identical input yields an
// identical id (content-addressed shards, free cache validation).
func (w *Writer) digest(v any) {
	b, _ := json.Marshal(v)
	_, _ = w.h.Write(b)
	_, _ = w.h.Write([]byte{'\n'})
}

// DefaultPrefix builds the deterministic context prefix indexed with each
// chunk: library › version › title › heading path › symbol.
func DefaultPrefix(c *Chunk) string {
	parts := []string{c.Library, c.Version()}
	for _, p := range []string{c.Title, c.HeadingPath, c.Symbol} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " › ")
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
