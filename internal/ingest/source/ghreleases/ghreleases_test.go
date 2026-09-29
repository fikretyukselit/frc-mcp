package ghreleases

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

func TestParse(t *testing.T) {
	day := func(d string) time.Time { v, _ := time.Parse("2006-01-02", d); return v }
	list := []release{
		{TagName: "revlib-2027.0.0-alpha-7", Body: "## Changelog:\n- Renames getBusId() to getCanPort()", HTMLURL: "https://github.com/x/y/releases/tag/revlib-2027.0.0-alpha-7", Prerelease: true, PublishedAt: day("2026-09-11")},
		{TagName: "revlib-2026.0.5", Body: "## Changelog:\n- Fixes a bug in the velocity filter configuration of SPARK Flex.", HTMLURL: "https://github.com/x/y/releases/tag/revlib-2026.0.5", PublishedAt: day("2026-03-12")},
		{TagName: "spline-26.1.4", Body: "other product", HTMLURL: "https://github.com/x/y/releases/tag/spline-26.1.4", PublishedAt: day("2026-07-03")},
		{TagName: "revlib-2024.2.4", Body: "old", HTMLURL: "https://github.com/x/y/releases/tag/revlib-2024.2.4", PublishedAt: day("2024-03-01")},
		{TagName: "revlib-2026.0.6", Draft: true, HTMLURL: "https://github.com/x/y/releases/tag/revlib-2026.0.6", PublishedAt: day("2026-09-20")},
	}
	b, _ := json.Marshal(list)
	p := filepath.Join(t.TempDir(), "list.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	src := sources.Source{Library: "revlib", Include: "revlib-", License: "MIT", Trust: "vendor", URL: "https://api.github.com/repos/x/y/releases"}
	var rels []index.Release
	var chunks []index.Chunk
	st, err := Parse(context.Background(), nil, p, src, day("2026-09-29"),
		func(r index.Release) error { rels = append(rels, r); return nil },
		func(c index.Chunk) error { chunks = append(chunks, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if st.Releases != 2 || len(rels) != 2 {
		t.Fatalf("stats %+v releases %+v", st, rels)
	}
	a, s := rels[0], rels[1]
	if a.Version != "2027.0.0-alpha-7" || a.Season != "2027" || a.Channel != "alpha" || !a.Breaking || a.ChunkID != "revlib-releases/2027/2027.0.0-alpha-7#0" {
		t.Errorf("alpha: %+v", a)
	}
	if s.Version != "2026.0.5" || s.Season != "2026" || s.Channel != "stable" || s.Breaking {
		t.Errorf("stable: %+v", s)
	}
	for _, c := range chunks {
		if err := c.Validate(); err != nil || c.Kind != "release" {
			t.Errorf("chunk %+v: %v", c, err)
		}
	}
}

func TestChannel(t *testing.T) {
	for v, want := range map[string]string{"2027.0.0-alpha-7": "alpha", "2026.0.0-beta-1": "beta", "2026.1.0-rc1": "beta", "2026.3.4": "stable"} {
		if got := Channel(v, false); got != want {
			t.Errorf("%s: %s want %s", v, got, want)
		}
	}
}
