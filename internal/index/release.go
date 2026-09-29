package index

import (
	"context"
	"fmt"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/ingest/sanitize"
)

// Release is one upstream release of a library (GitHub releases): an exact
// fact for frc_whats_new. Title and Summary are sanitized at write time;
// release notes are often generated from pull-request titles, so they are an
// injection vector (docs/security.md T1).
type Release struct {
	Library     string    `json:"library"`
	Version     string    `json:"version"`
	Season      string    `json:"season"`
	Channel     string    `json:"channel"`
	PublishedAt time.Time `json:"published_at"`
	Breaking    bool      `json:"breaking"`
	Title       string    `json:"title,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	ChunkID     string    `json:"chunk_id,omitempty"`
	SourceURL   string    `json:"source_url"`
	UpstreamRev string    `json:"upstream_rev"`
	RetrievedAt time.Time `json:"retrieved_at"`
	License     string    `json:"license"`
	Trust       string    `json:"trust"`
}

// AddRelease appends a release fact.
func (w *Writer) AddRelease(ctx context.Context, r Release) error {
	if r.Library == "" || r.Version == "" || r.Season == "" || r.SourceURL == "" || r.License == "" || r.Trust == "" || r.PublishedAt.IsZero() {
		return fmt.Errorf("%w release %s@%s: missing library/version/season/source/license/trust/date", ErrInvalidChunk, r.Library, r.Version)
	}
	r.Title, r.Summary = sanitize.Clean(r.Title), sanitize.Clean(r.Summary)
	_, err := w.tx.ExecContext(ctx, `INSERT OR REPLACE INTO release (library, version, season, channel, published_at,
		breaking, title, summary, chunk_id, source_url, upstream_rev, retrieved_at, license, trust)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Library, r.Version, r.Season, r.Channel, r.PublishedAt.Unix(), b2i(r.Breaking), r.Title, r.Summary, r.ChunkID,
		r.SourceURL, r.UpstreamRev, r.RetrievedAt.Unix(), r.License, r.Trust)
	if err != nil {
		return fmt.Errorf("index: insert release %s@%s: %w", r.Library, r.Version, err)
	}
	w.digest(r)
	return nil
}

// Releases returns every release in the shard, newest first. Shards built
// before the table existed return none.
func (r *Reader) Releases(ctx context.Context) ([]Release, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'release'`).Scan(&n); err != nil || n == 0 {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT library, version, season, channel, published_at, breaking, title, summary,
		chunk_id, source_url, upstream_rev, retrieved_at, license, trust FROM release ORDER BY published_at DESC, library`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var x Release
		var pub, at int64
		var brk int
		if err := rows.Scan(&x.Library, &x.Version, &x.Season, &x.Channel, &pub, &brk, &x.Title, &x.Summary, &x.ChunkID,
			&x.SourceURL, &x.UpstreamRev, &at, &x.License, &x.Trust); err != nil {
			return nil, err
		}
		x.PublishedAt, x.RetrievedAt, x.Breaking = time.Unix(pub, 0).UTC(), time.Unix(at, 0).UTC(), brk == 1
		out = append(out, x)
	}
	return out, rows.Err()
}
