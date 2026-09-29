package retrieve

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/facts"
	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func releaseEngine(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "releases.sqlite")
	w, err := index.Create(ctx, p, "releases")
	if err != nil {
		t.Fatal(err)
	}
	day := func(d string) time.Time { v, _ := time.Parse("2006-01-02", d); return v }
	for _, r := range []index.Release{
		{Library: "revlib", Version: "2026.0.2", Channel: "stable", PublishedAt: day("2026-02-01")},
		{Library: "revlib", Version: "2026.0.5", Channel: "stable", PublishedAt: day("2026-03-12"), Breaking: true},
		{Library: "revlib", Version: "2027.0.0-alpha-7", Channel: "alpha", PublishedAt: day("2026-09-10")},
		{Library: "phoenix6", Version: "26.70.0-alpha-2", Channel: "alpha", PublishedAt: day("2026-09-05")},
		{Library: "wpilib", Version: "2027.0.0-alpha-7", Channel: "alpha", PublishedAt: day("2026-09-01")},
	} {
		r.Season = facts.SeasonFor(r.Library, r.Version)
		r.SourceURL, r.UpstreamRev, r.License, r.Trust, r.RetrievedAt = "https://example.invalid/"+r.Version, "v"+r.Version, "MIT", "vendor", day("2026-09-29")
		if err := w.AddRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	rd, err := index.Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return New([]*index.Reader{rd}, Options{})
}

func versions(rs []index.Release) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Library+"@"+r.Version)
	}
	return out
}

func TestWhatsNew(t *testing.T) {
	e := releaseEngine(t)
	for _, tc := range []struct {
		name string
		q    WhatsNewQuery
		want string
	}{
		{"alias + since version", WhatsNewQuery{Library: "rev", Since: "2026.0.2"}, "[revlib@2027.0.0-alpha-7 revlib@2026.0.5]"},
		{"stable only", WhatsNewQuery{Library: "REVLib", Stable: true}, "[revlib@2026.0.5 revlib@2026.0.2]"},
		{"season (CTRE 26.70 is 2027)", WhatsNewQuery{Season: "2027"}, "[revlib@2027.0.0-alpha-7 phoenix6@26.70.0-alpha-2 wpilib@2027.0.0-alpha-7]"},
		{"after date, limit", WhatsNewQuery{Library: "all", After: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), Limit: 1}, "[revlib@2027.0.0-alpha-7]"},
		{"unknown library", WhatsNewQuery{Library: "nope"}, "[]"},
	} {
		if got := fmtList(versions(e.WhatsNew(tc.q))); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	if got := e.ReleaseLibraries(); fmtList(got) != "[phoenix6 revlib wpilib]" {
		t.Errorf("libraries %v", got)
	}
}

func fmtList(xs []string) string {
	out := "["
	for i, x := range xs {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out + "]"
}
