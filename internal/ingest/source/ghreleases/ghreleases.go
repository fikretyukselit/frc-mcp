// Package ghreleases ingests a repository's GitHub releases (src.URL: the
// REST releases listing, ?per_page=100). Each release becomes an exact fact
// row (frc_whats_new) and a searchable "release" chunk. Only releases of
// seasons ≥ minSeason are kept; drafts are skipped; src.Include filters by
// tag prefix (REV's binaries repository mixes products).
package ghreleases

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/facts"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/chunking"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// Getter is the fetch dependency (fetch.Fetcher in production).
type Getter interface {
	Get(ctx context.Context, url string) (*fetch.Result, error)
}

const (
	minSeason  = "2025"
	maxSummary = 600
)

type release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// breakingRe flags notes that announce incompatible changes. It is a hint
// for the agent ("read this one"), not a verdict.
var breakingRe = regexp.MustCompile(`(?i)\bbreaking\b|\b(remov(ed|es)|renam(ed|es)|drop(s|ped) support|no longer (supported|available|works?)|incompatible|now (take|takes|require|requires))\b|⚠`)

// Stats summarizes a parse.
type Stats struct{ Releases, Chunks int }

// Version strips the tag prefix and a leading "v".
func Version(tag, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(tag, prefix), "v")
}

// Channel classifies a release.
func Channel(version string, prerelease bool) string {
	l := strings.ToLower(version)
	switch {
	case strings.Contains(l, "alpha"):
		return "alpha"
	case strings.Contains(l, "beta") || strings.Contains(l, "rc") || strings.Contains(l, "pre") || prerelease:
		return "beta"
	}
	return "stable"
}

// Parse reads the listing at listPath and emits release facts and chunks.
func Parse(_ context.Context, _ Getter, listPath string, src sources.Source, retrieved time.Time,
	emitRelease func(index.Release) error, emitChunk func(index.Chunk) error) (Stats, error) {
	var st Stats
	b, err := os.ReadFile(listPath)
	if err != nil {
		return st, err
	}
	var rs []release
	if err := json.Unmarshal(b, &rs); err != nil {
		return st, fmt.Errorf("ghreleases: %s: %w", src.URL, err)
	}
	for _, r := range rs {
		if r.Draft || r.PublishedAt.IsZero() || !strings.HasPrefix(r.TagName, src.Include) || !strings.HasPrefix(r.HTMLURL, "https://") {
			continue
		}
		ver := Version(r.TagName, src.Include)
		season := facts.SeasonFor(src.Library, ver)
		if season == "" || season < minSeason {
			continue
		}
		body := strings.TrimSpace(strings.ReplaceAll(r.Body, "\r\n", "\n"))
		title := strings.TrimSpace(r.Name)
		if title == "" {
			title = r.TagName
		}
		docID := src.Library + "-releases/" + season + "/" + ver
		rel := index.Release{Library: src.Library, Version: ver, Season: season, Channel: Channel(ver, r.Prerelease),
			PublishedAt: r.PublishedAt.UTC(), Breaking: breakingRe.MatchString(body), Title: title,
			Summary: summarize(body), SourceURL: r.HTMLURL, UpstreamRev: r.TagName, RetrievedAt: retrieved,
			License: src.License, Trust: src.Trust}
		if body != "" {
			rel.ChunkID = docID + "#0"
		}
		if err := emitRelease(rel); err != nil {
			return st, err
		}
		st.Releases++
		if body == "" {
			continue
		}
		heading := libTitle(src.Library) + " " + ver + " release notes"
		ord := 0
		for _, part := range chunking.Split(heading + " (" + rel.PublishedAt.Format("2006-01-02") + ")\n\n" + body) {
			if textutil.EstimateTokens(part) < chunking.MinTokens && ord > 0 {
				continue
			}
			if err := emitChunk(index.Chunk{DocID: docID, Ord: ord, Library: src.Library, VersionLo: ver, Season: season,
				Channel: rel.Channel, Language: "any", Kind: "release", Title: heading, HeadingPath: heading,
				Body: part, SourceURL: r.HTMLURL, UpstreamRev: r.TagName, RetrievedAt: retrieved, License: src.License,
				Trust: src.Trust, Authority: 1}); err != nil {
				return st, err
			}
			ord++
			st.Chunks++
		}
	}
	return st, nil
}

// summarize keeps the first lines of the notes (bullets first) for the
// frc_whats_new digest; the full text is in the chunk.
func summarize(body string) string {
	var out []string
	n := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "<!--") || strings.HasPrefix(t, "**Full Changelog**") {
			continue
		}
		if n+len(t) > maxSummary {
			out = append(out, "…")
			break
		}
		out = append(out, t)
		n += len(t) + 1
	}
	return strings.Join(out, "\n")
}

var libTitles = map[string]string{"wpilib": "WPILib", "phoenix6": "Phoenix 6", "revlib": "REVLib", "photonvision": "PhotonVision",
	"pathplannerlib": "PathPlannerLib", "choreolib": "Choreo", "advantagekit": "AdvantageKit", "yagsl": "YAGSL"}

func libTitle(lib string) string {
	if t := libTitles[lib]; t != "" {
		return t
	}
	return lib
}
