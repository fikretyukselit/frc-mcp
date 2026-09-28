package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Vendordep is one version of a vendor library from the WPILib catalog.
type Vendordep struct {
	UUID        string          `json:"uuid"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Season      string          `json:"season"`
	Channel     string          `json:"channel"`
	FRCYear     string          `json:"frc_year"`
	FileName    string          `json:"file_name"`
	JSONURL     string          `json:"json_url"`
	MavenURLs   []string        `json:"maven_urls"`
	Conflicts   []Conflict      `json:"conflicts_with,omitempty"`
	JavaDeps    int             `json:"java_deps"`
	CppDeps     int             `json:"cpp_deps"`
	Description string          `json:"description,omitempty"`
	Website     string          `json:"website,omitempty"`
	Raw         json.RawMessage `json:"-"`
	SourceURL   string          `json:"source_url"`
	UpstreamRev string          `json:"upstream_rev"`
	RetrievedAt time.Time       `json:"retrieved_at"`
}

// Conflict is a vendordep's declared incompatibility.
type Conflict struct {
	UUID            string `json:"uuid"`
	ErrorMessage    string `json:"errorMessage"`
	OfflineFileName string `json:"offlineFileName,omitempty"`
}

// AddVendordep appends a vendordep fact.
func (w *Writer) AddVendordep(ctx context.Context, v Vendordep) error {
	if v.UUID == "" || v.Name == "" || v.Version == "" || v.Season == "" || v.SourceURL == "" || len(v.Raw) == 0 {
		return fmt.Errorf("%w vendordep %s@%s: missing uuid/name/version/season/source/raw", ErrInvalidChunk, v.Name, v.Version)
	}
	if v.JSONURL != "" && !strings.HasPrefix(v.JSONURL, "https://") {
		v.JSONURL = "" // never hand out a non-https install URL
	}
	maven, _ := json.Marshal(nonNil(v.MavenURLs))
	conf, _ := json.Marshal(v.Conflicts)
	if v.Conflicts == nil {
		conf = []byte("[]")
	}
	_, err := w.tx.ExecContext(ctx, `INSERT OR REPLACE INTO vendordep (uuid, name, version, season, channel, frc_year,
		file_name, json_url, maven_urls, conflicts, java_deps, cpp_deps, description, website, raw_json, source_url,
		upstream_rev, retrieved_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.UUID, v.Name, v.Version, v.Season, v.Channel, v.FRCYear, v.FileName, v.JSONURL, string(maven), string(conf),
		v.JavaDeps, v.CppDeps, v.Description, v.Website, string(v.Raw), v.SourceURL, v.UpstreamRev, v.RetrievedAt.Unix())
	if err != nil {
		return fmt.Errorf("index: insert vendordep %s@%s: %w", v.Name, v.Version, err)
	}
	w.digest(v)
	return nil
}

// Vendordeps returns every catalog entry of a season (all versions); the
// caller picks versions with facts.CompareVersions. Empty season = all.
func (r *Reader) Vendordeps(ctx context.Context, season string) ([]Vendordep, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT uuid, name, version, season, channel, frc_year, file_name, json_url,
		maven_urls, conflicts, java_deps, cpp_deps, description, website, raw_json, source_url, upstream_rev, retrieved_at
		FROM vendordep WHERE (?1 = '' OR season = ?1) ORDER BY name, version`, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vendordep
	for rows.Next() {
		var v Vendordep
		var maven, conf, raw string
		var at int64
		if err := rows.Scan(&v.UUID, &v.Name, &v.Version, &v.Season, &v.Channel, &v.FRCYear, &v.FileName, &v.JSONURL,
			&maven, &conf, &v.JavaDeps, &v.CppDeps, &v.Description, &v.Website, &raw, &v.SourceURL, &v.UpstreamRev, &at); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(maven), &v.MavenURLs)
		_ = json.Unmarshal([]byte(conf), &v.Conflicts)
		v.Raw = json.RawMessage(raw)
		v.RetrievedAt = time.Unix(at, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ = sql.ErrNoRows

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
