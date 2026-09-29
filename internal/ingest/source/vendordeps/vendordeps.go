// Package vendordeps ingests WPILib's vendor-json-repo catalog: the list of
// vendordep JSON files per season that the WPILib VS Code Dependency Manager
// shows. Every file becomes an exact fact row; the newest version of each
// library also becomes one searchable prose chunk ("how do I install X").
package vendordeps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/facts"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/project"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// Getter is the fetch dependency (fetch.Fetcher in production).
type Getter interface {
	Get(ctx context.Context, url string) (*fetch.Result, error)
}

type listing struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
}

type vendordepJSON struct {
	FileName      string            `json:"fileName"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	FRCYear       any               `json:"frcYear"`
	WPILibYear    any               `json:"wpilibYear"` // replaces frcYear from 2027
	UUID          string            `json:"uuid"`
	MavenURLs     []string          `json:"mavenUrls"`
	JSONURL       string            `json:"jsonUrl"`
	ConflictsWith []index.Conflict  `json:"conflictsWith"`
	JavaDeps      []json.RawMessage `json:"javaDependencies"`
	CppDeps       []json.RawMessage `json:"cppDependencies"`
}

// Stats summarizes a parse.
type Stats struct{ Files, Libraries int }

// Parse lists the catalog folder (src.URL: GitHub contents API), fetches each
// vendordep JSON (conditional GET, cached) and emits facts and chunks.
func Parse(ctx context.Context, g Getter, src sources.Source, retrieved time.Time,
	emitVendordep func(index.Vendordep) error, emitChunk func(index.Chunk) error) (Stats, error) {
	var st Stats
	res, err := g.Get(ctx, src.URL)
	if err != nil {
		return st, err
	}
	b, err := os.ReadFile(res.Path)
	if err != nil {
		return st, err
	}
	var files []listing
	if err := json.Unmarshal(b, &files); err != nil {
		return st, fmt.Errorf("vendordeps: listing %s: %w", src.URL, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	latest := map[string]index.Vendordep{}
	for _, f := range files {
		if f.Type != "file" || !strings.HasSuffix(f.Name, ".json") || !strings.HasPrefix(f.DownloadURL, "https://") {
			continue
		}
		r, err := g.Get(ctx, f.DownloadURL)
		if err != nil {
			return st, err
		}
		raw, err := os.ReadFile(r.Path)
		if err != nil {
			return st, err
		}
		var v vendordepJSON
		if err := json.Unmarshal(raw, &v); err != nil || v.UUID == "" || v.Version == "" {
			continue // malformed catalog entry: skip, never guess
		}
		rev := strings.Trim(r.ETag, `"W/`)
		if rev == "" {
			rev = "sha256:" + r.SHA256[:16]
		}
		fy := project.VendordepYear(v.FRCYear, v.WPILibYear)
		vd := index.Vendordep{UUID: v.UUID, Name: v.Name, Version: v.Version, Season: src.Season, Channel: src.Channel,
			FRCYear: fy, FileName: v.FileName, JSONURL: v.JSONURL, MavenURLs: v.MavenURLs, Conflicts: v.ConflictsWith,
			JavaDeps: len(v.JavaDeps), CppDeps: len(v.CppDeps), Raw: raw, SourceURL: f.DownloadURL, UpstreamRev: rev,
			RetrievedAt: r.FetchedAt}
		if err := emitVendordep(vd); err != nil {
			return st, err
		}
		st.Files++
		if cur, ok := latest[vd.UUID]; !ok || facts.CompareVersions(vd.Version, cur.Version) > 0 {
			latest[vd.UUID] = vd
		}
	}
	uuids := make([]string, 0, len(latest))
	for u := range latest {
		uuids = append(uuids, u)
	}
	sort.Slice(uuids, func(i, j int) bool { return latest[uuids[i]].Name < latest[uuids[j]].Name })
	for _, u := range uuids {
		v := latest[u]
		st.Libraries++
		if err := emitChunk(chunkFor(v, src, retrieved)); err != nil {
			return st, err
		}
	}
	return st, nil
}

func chunkFor(v index.Vendordep, src sources.Source, retrieved time.Time) index.Chunk {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s is the newest %s version of this vendor library in the WPILib vendordep catalog (frcYear %s).\n\n",
		v.Name, v.Version, src.Season, v.FRCYear)
	if v.JSONURL != "" {
		fmt.Fprintf(&b, "Install or update from the online JSON (VS Code: WPILib: Manage Vendor Libraries → Install new library (online)), or:\n\n```\n./gradlew vendordep --url=%s\n```\n\n", v.JSONURL)
	} else {
		b.WriteString("Install it with the WPILib Dependency Manager in VS Code (WPILib: Manage Vendor Libraries).\n\n")
	}
	fmt.Fprintf(&b, "Vendordep file name: `%s`. UUID: `%s`. Java artifacts: %d, C++ artifacts: %d.", v.FileName, v.UUID, v.JavaDeps, v.CppDeps)
	if len(v.Conflicts) > 0 {
		b.WriteString(" Conflicts with:")
		for _, c := range v.Conflicts {
			fmt.Fprintf(&b, " %s (%s);", c.OfflineFileName, c.ErrorMessage)
		}
	}
	// Names are not unique (e.g. Phoenix 5 and its replay variant share one);
	// the uuid prefix keeps doc ids stable and distinct.
	return index.Chunk{DocID: "vendordeps/" + src.Season + "/" + slug(v.Name) + "-" + v.UUID[:min(8, len(v.UUID))], Ord: 0, Library: "vendordeps",
		VersionLo: v.Version, Season: src.Season, Channel: src.Channel, Language: "any", Kind: "release",
		Title: v.Name + " vendordep", HeadingPath: "Vendordep catalog › " + src.Season, Body: b.String(),
		SourceURL: v.SourceURL, UpstreamRev: v.UpstreamRev, RetrievedAt: retrieved, License: src.License,
		Trust: src.Trust, Authority: 2}
}

func slug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	return strings.Trim(path.Clean(b.String()), "-")
}
