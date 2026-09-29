package index

import (
	"context"
	"fmt"
	"strings"
)

// Migration is one curated rule for one language: from_fqn in from_season
// becomes to_fqn in to_season (docs/mcp-surface.md frc_migrate). Rules come
// from data/migrations/*.yaml and always carry a citation.
type Migration struct {
	RuleID     string `json:"rule_id"`
	Library    string `json:"library"`
	Language   string `json:"language"`
	FromSeason string `json:"from_season"`
	ToSeason   string `json:"to_season"`
	Kind       string `json:"kind"` // rename | move | removed | signature | behavior
	From       string `json:"from"`
	To         string `json:"to,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Citation   string `json:"citation"`
	Verified   bool   `json:"verified"`
}

// MigrationKinds are the allowed rule kinds.
var MigrationKinds = []string{"rename", "move", "removed", "signature", "behavior"}

// Validate checks a rule's required fields.
func (m *Migration) Validate() error {
	switch {
	case m.RuleID == "" || m.Library == "" || m.From == "" || m.FromSeason == "" || m.ToSeason == "":
		return fmt.Errorf("%w migration %q: missing id/library/from/seasons", ErrInvalidChunk, m.RuleID)
	case !contains(Languages, m.Language) || m.Language == "any":
		return fmt.Errorf("%w migration %s: language=%q", ErrInvalidChunk, m.RuleID, m.Language)
	case !contains(MigrationKinds, m.Kind):
		return fmt.Errorf("%w migration %s: kind=%q not in %v", ErrInvalidChunk, m.RuleID, m.Kind, MigrationKinds)
	case !strings.HasPrefix(m.Citation, "https://"):
		return fmt.Errorf("%w migration %s: citation must be an https URL", ErrInvalidChunk, m.RuleID)
	case m.To == "" && m.Kind != "removed" && m.Kind != "behavior":
		return fmt.Errorf("%w migration %s: kind %s needs a target", ErrInvalidChunk, m.RuleID, m.Kind)
	}
	return nil
}

// AddMigration appends a curated rule.
func (w *Writer) AddMigration(ctx context.Context, m Migration) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, `INSERT OR REPLACE INTO migration (rule_id, library, language, from_season,
		to_season, kind, from_fqn, to_fqn, notes, citation, verified) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		m.RuleID, m.Library, m.Language, m.FromSeason, m.ToSeason, m.Kind, m.From, m.To, m.Notes, m.Citation,
		b2i(m.Verified)); err != nil {
		return fmt.Errorf("index: insert migration %s: %w", m.RuleID, err)
	}
	w.digest(m)
	return nil
}

const migrationCols = `rule_id, library, language, from_season, to_season, kind, from_fqn, to_fqn, notes, citation, verified`

// MigrationsFrom returns the curated rules whose source symbol is fqn, or a
// member of type fqn when members is set ("T" matches "T" and "T#…").
func (r *Reader) MigrationsFrom(ctx context.Context, fqn, language string, members bool) ([]Migration, error) {
	q := `SELECT ` + migrationCols + ` FROM migration WHERE (from_fqn = ?1 OR (?2 = 1 AND from_fqn >= ?1 || '#' AND from_fqn < ?1 || '$'))
		AND (?3 = '' OR language = ?3) ORDER BY from_fqn, rule_id`
	return r.queryMigrations(ctx, q, fqn, b2i(members), language)
}

// MigrationsTo returns the curated rules whose target is fqn (reverse lookup).
func (r *Reader) MigrationsTo(ctx context.Context, fqn, language string) ([]Migration, error) {
	q := `SELECT ` + migrationCols + ` FROM migration WHERE to_fqn = ?1 AND ?2 = ?2 AND (?3 = '' OR language = ?3) ORDER BY rule_id`
	return r.queryMigrations(ctx, q, fqn, 0, language)
}

// AllMigrations lists every curated rule in the shard.
func (r *Reader) AllMigrations(ctx context.Context) ([]Migration, error) {
	return r.queryMigrations(ctx, `SELECT `+migrationCols+` FROM migration ORDER BY library, language, rule_id, from_fqn`)
}

func (r *Reader) queryMigrations(ctx context.Context, q string, args ...any) ([]Migration, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("index: migrations: %w", err)
	}
	defer rows.Close()
	var out []Migration
	for rows.Next() {
		var m Migration
		var v int
		if err := rows.Scan(&m.RuleID, &m.Library, &m.Language, &m.FromSeason, &m.ToSeason, &m.Kind, &m.From, &m.To,
			&m.Notes, &m.Citation, &v); err != nil {
			return nil, err
		}
		m.Verified = v == 1
		out = append(out, m)
	}
	return out, rows.Err()
}
