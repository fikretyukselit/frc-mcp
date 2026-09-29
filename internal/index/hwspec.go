package index

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// HWSpec is one source's values for one part (frc_hardware). Sources are
// never merged: WPILib's DCMotor constants and a vendor dyno can differ a lot.
type HWSpec struct {
	Part        string             `json:"part"`     // normalized id, e.g. "krakenx60-foc"
	Name        string             `json:"name"`     // display name, e.g. "Kraken X60 (FOC)"
	Category    string             `json:"category"` // motor | controller | encoder | imu | swerve_module | sensor
	Source      string             `json:"source"`   // e.g. "wpilib-dcmotor"
	Season      string             `json:"season"`
	Fields      map[string]float64 `json:"fields"`            // e.g. stall_torque_nm, free_speed_rpm
	Factory     string             `json:"factory,omitempty"` // source-side factory, e.g. "getKrakenX60Foc"
	Note        string             `json:"note,omitempty"`    // provenance note from the source (e.g. "From <dyno URL>")
	SourceURL   string             `json:"source_url"`
	UpstreamRev string             `json:"upstream_rev"`
	RetrievedAt time.Time          `json:"retrieved_at"`
	License     string             `json:"license"`
	Trust       string             `json:"trust"`
}

// AddHWSpec appends a hardware spec row.
func (w *Writer) AddHWSpec(ctx context.Context, h HWSpec) error {
	if h.Part == "" || h.Name == "" || h.Category == "" || h.Source == "" || h.Season == "" || len(h.Fields) == 0 ||
		h.SourceURL == "" || h.License == "" || h.Trust == "" {
		return fmt.Errorf("%w hw_spec %s/%s: missing fields", ErrInvalidChunk, h.Part, h.Source)
	}
	f, err := json.Marshal(h.Fields)
	if err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, `INSERT OR REPLACE INTO hw_spec (part, name, category, source, season, fields,
		factory, note, source_url, upstream_rev, retrieved_at, license, trust) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		h.Part, h.Name, h.Category, h.Source, h.Season, string(f), h.Factory, h.Note, h.SourceURL, h.UpstreamRev,
		h.RetrievedAt.Unix(), h.License, h.Trust); err != nil {
		return fmt.Errorf("index: insert hw_spec %s: %w", h.Part, err)
	}
	w.digest(h)
	return nil
}

// HWSpecs returns every hardware row in the shard (none for older shards).
func (r *Reader) HWSpecs(ctx context.Context) ([]HWSpec, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'hw_spec'`).Scan(&n); err != nil || n == 0 {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT part, name, category, source, season, fields, factory, note, source_url,
		upstream_rev, retrieved_at, license, trust FROM hw_spec ORDER BY category, part, source, season`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HWSpec
	for rows.Next() {
		var h HWSpec
		var f string
		var at int64
		if err := rows.Scan(&h.Part, &h.Name, &h.Category, &h.Source, &h.Season, &f, &h.Factory, &h.Note, &h.SourceURL,
			&h.UpstreamRev, &at, &h.License, &h.Trust); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(f), &h.Fields)
		h.RetrievedAt = time.Unix(at, 0).UTC()
		out = append(out, h)
	}
	return out, rows.Err()
}
