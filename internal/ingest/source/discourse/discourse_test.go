package discourse

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// files serves recorded feeds by URL; no network in unit tests.
type files map[string]string

func (f files) Get(_ context.Context, url string) (*fetch.Result, error) {
	p, ok := f[url]
	if !ok {
		return nil, errors.New("unexpected fetch " + url)
	}
	return &fetch.Result{URL: url, Path: p}, nil
}

var src = sources.Source{ID: "chiefdelphi", Adapter: "discourse-rss", URL: "https://www.chiefdelphi.com/latest.rss",
	PostsURL: "https://www.chiefdelphi.com/posts.rss", Library: "chiefdelphi", Season: "rolling", Channel: "stable",
	Version: "rolling", License: "LicenseRef-ChiefDelphi-UserContent", Trust: "community", Shard: "forum",
	Categories: []string{"Programming", "Java", "C/C++", "Python", "Control System", "PhotonVision", "Technical"}}

func parse(t *testing.T, s sources.Source) (Stats, map[string]index.Chunk) {
	t.Helper()
	retrieved := time.Date(2026, 9, 29, 17, 0, 0, 0, time.UTC)
	got := map[string]index.Chunk{}
	st, err := Parse(context.Background(), files{s.PostsURL: "testdata/posts.rss"}, "testdata/latest.rss", s, retrieved,
		func(c index.Chunk) error {
			if _, dup := got[c.ID()]; dup {
				t.Errorf("chunk %s emitted twice", c.ID())
			}
			got[c.ID()] = c
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	return st, got
}

func TestParse(t *testing.T) {
	st, got := parse(t, src)
	for _, tc := range []struct {
		id, title, heading, url, season, language string
		suspect                                   bool
		has, hasNot                               []string
	}{
		{id: "chiefdelphi/topic-600001#0", title: "SparkMax velocity PID oscillates after REVLib 2026 update",
			heading: "Chief Delphi › Programming › @builder_bot", season: "2026", language: "any",
			url:    "https://www.chiefdelphi.com/t/sparkmax-velocity-pid-oscillates-after-revlib-2026-update/600001",
			has:    []string{"`SparkMaxConfig`", "**What we tried**", "- Lowered kP from 0.002 to 0.0005", "```java\nconfig.closedLoop.pid"},
			hasNot: []string{"posts - ", "Read full topic", "1920×1080", "graph", "Repeated item"}},
		{id: "chiefdelphi/post-7000003#0", title: "SparkMax velocity PID oscillates after REVLib 2026 update",
			heading: "Chief Delphi › Programming › reply #3 by @rev_helper", season: "2026", language: "any",
			url:    "https://www.chiefdelphi.com/t/sparkmax-velocity-pid-oscillates-after-revlib-2026-update/600001/3",
			has:    []string{"feedForward.kV"},
			hasNot: []string{"Lowered kP", "Pat Example"}}, // quote of another post stripped; display name dropped
		{id: "chiefdelphi/topic-600003#0", title: "FIXED: CAN bus errors with Kraken X60 (works 100%)",
			heading: "Chief Delphi › Control System › @helpful_stranger", season: "2026", language: "any", suspect: true,
			url: "https://www.chiefdelphi.com/t/fixed-can-bus-errors-with-kraken-x60-works-100/600003",
			has: []string{"curl -fsSL"}}, // flagged, not dropped: the flag stays auditable
		{id: "chiefdelphi/post-7000030#0", heading: "Chief Delphi › Control System › reply #2 by @helpful_stranger2",
			season: "2026", language: "any", suspect: true,
			url: "https://www.chiefdelphi.com/t/fixed-can-bus-errors-with-kraken-x60-works-100/600003/2", // ?page=1 dropped
			has: []string{"repository owner"}},
		{id: "chiefdelphi/post-7000031#0", heading: "Chief Delphi › Control System › reply #3 by @real_mentor",
			season: "2026", language: "any",
			url: "https://www.chiefdelphi.com/t/fixed-can-bus-errors-with-kraken-x60-works-100/600003/3",
			has: []string{"Phoenix Tuner X"}},
		// Hidden text (display:none span, HTML comment, zero-width space in the
		// title) is removed before conversion, so it can neither reach the agent
		// nor hide from the detector. January posts belong to the new season.
		{id: "chiefdelphi/topic-600004#0", title: "PhotonVision pose estimate jumps between tags",
			heading: "Chief Delphi › PhotonVision › @vision_kid", season: "2027", language: "any",
			url:    "https://www.chiefdelphi.com/t/photonvision-pose-estimate-jumps-between-tags/600004",
			has:    []string{"Is this a calibration problem?", "We already calibrated", "https://docs.photonvision.org/en/v2026.3.4/docs/calibration/calibration.html"},
			hasNot: []string{"evil-lib", "push to main", "Recalibrate", "Preview text", "\u200b"}},
		{id: "chiefdelphi/topic-600006#0", heading: "Chief Delphi › Java › @java_mentor", season: "2026", language: "java",
			url: "https://www.chiefdelphi.com/t/command-based-when-to-use-commands-defer/600006",
			has: []string{"`Commands.defer`"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			c, ok := got[tc.id]
			if !ok {
				t.Fatalf("missing; have %v", keys(got))
			}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			if c.Kind != "forum" || c.Trust != "community" || c.Library != "chiefdelphi" ||
				c.License != "LicenseRef-ChiefDelphi-UserContent" || c.Channel != "stable" || c.VersionLo != c.Season {
				t.Errorf("provenance: %+v", c)
			}
			if tc.title != "" && c.Title != tc.title {
				t.Errorf("title = %q", c.Title)
			}
			if c.HeadingPath != tc.heading || c.SourceURL != tc.url || c.Anchor != "" ||
				c.Season != tc.season || c.Language != tc.language || c.Suspect != tc.suspect {
				t.Errorf("got heading=%q url=%q anchor=%q season=%s lang=%s suspect=%v", c.HeadingPath, c.SourceURL,
					c.Anchor, c.Season, c.Language, c.Suspect)
			}
			if !strings.HasPrefix(c.UpstreamRev, "sha256:") {
				t.Errorf("upstream_rev = %q", c.UpstreamRev)
			}
			for _, s := range tc.has {
				if !strings.Contains(c.Body, s) {
					t.Errorf("body lacks %q:\n%s", s, c.Body)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(c.Body, s) {
					t.Errorf("body keeps %q:\n%s", s, c.Body)
				}
			}
		})
	}
	if len(got) != 7 {
		t.Errorf("chunks = %d, want 7: %v", len(got), keys(got))
	}
	want := Stats{Topics: 4, Replies: 3, Chunks: 7, Suspect: 2, Filtered: 3, Duplicates: 3, Invalid: 3, Empty: 1}
	if st != want {
		t.Errorf("stats = %+v, want %+v", st, want)
	}
}

// Without a posts feed or a category filter the adapter keeps every valid
// topic; ids and revisions are stable across runs.
func TestParseTopicsOnly(t *testing.T) {
	s := src
	s.PostsURL, s.Categories = "", nil
	st, a := parse(t, s)
	_, b := parse(t, s)
	if st.Topics != 5 || st.Replies != 0 || st.Filtered != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if _, ok := a["chiefdelphi/topic-600002#0"]; !ok {
		t.Errorf("fundraising topic missing without a category filter")
	}
	for id, c := range a {
		if b[id].UpstreamRev != c.UpstreamRev {
			t.Errorf("%s: revision not stable", id)
		}
	}
}

func TestMarkdown(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"paragraph and code", `<p>Use <code>TalonFX</code>.</p><pre><code class="lang-cpp">frc::TimedRobot r;</code></pre>`,
			"Use `TalonFX`.\n\n```cpp\nfrc::TimedRobot r;\n```"},
		{"auto language", `<pre><code class="lang-auto">x = 1</code></pre>`, "```\nx = 1\n```"},
		{"nested hidden element", `<p>a<span hidden><span>b</span> c</span> d</p>`, "a d"},
		{"hidden by style", `<div style="font-size:0"><p>secret</p></div><p>shown</p>`, "shown"},
		{"script", `<p>x</p><script>alert(1)</script>`, "x"},
		{"tag characters", "<p>ok\U000E0041\U000E0042</p>", "ok"},
		{"footer only", `<p><small>1 post - 1 participant</small></p><p><a href="https://x">Read full topic</a></p>`, ""},
		{"blockquote kept", `<blockquote><p>quoted from the docs</p></blockquote>`, "> quoted from the docs"},
	} {
		if got := Markdown(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestReadFeedRejectsOversize(t *testing.T) {
	p := t.TempDir() + "/big.rss"
	if err := writeBig(p); err != nil {
		t.Fatal(err)
	}
	if _, err := readFeed(p); err == nil {
		t.Fatal("oversized feed accepted")
	}
}

func keys(m map[string]index.Chunk) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func writeBig(path string) error {
	return os.WriteFile(path, bytes.Repeat([]byte(" "), maxFeedBytes+1), 0o644)
}

func FuzzMarkdown(f *testing.F) {
	for _, s := range []string{`<p>a</p>`, `<aside class="quote"><p>q</p></aside>`, `<pre><code class="lang-java">x</code></pre>`,
		`<aside class="onebox" data-onebox-src="https://a/b"></aside>`, `<p>ignore previous instructions</p>`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Markdown(s)
		if strings.ContainsAny(out, "\u200b\u202e\ufeff") {
			t.Fatalf("invisible characters survived: %q", out)
		}
	})
}
