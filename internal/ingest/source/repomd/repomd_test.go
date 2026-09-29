package repomd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

type fakeGetter struct {
	dir   string
	files map[string]string // raw URL -> body
	hits  []string
}

func (f *fakeGetter) Get(_ context.Context, u string) (*fetch.Result, error) {
	f.hits = append(f.hits, u)
	p := filepath.Join(f.dir, strings.ReplaceAll(u, "/", "_"))
	if err := os.WriteFile(p, []byte(f.files[u]), 0o644); err != nil {
		return nil, err
	}
	return &fetch.Result{URL: u, Path: p}, nil
}

func TestParse(t *testing.T) {
	dir := t.TempDir()
	page := "# Pose Estimation\n\nThe PhotonPoseEstimator fuses AprilTag detections into a single robot pose estimate for odometry.\n"
	type e struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	tr := map[string]any{"truncated": false, "tree": []e{
		{"docs/source/docs/programming/estimator.md", "blob", 100},
		{"docs/source/docs/contributing/building.md", "blob", 100},
		{"docs/source/docs/index.md", "blob", 100},
		{"docs/source/docs/big.md", "blob", 10 << 20},
		{"README.md", "blob", 100},
		{"docs/source/docs/programming/image.png", "blob", 100},
		{"docs/source/docs/programming", "tree", 0},
	}}
	b, _ := json.Marshal(tr)
	treePath := filepath.Join(dir, "tree.json")
	if err := os.WriteFile(treePath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	raw := "https://raw.githubusercontent.com/PhotonVision/photonvision/v2026.3.4/docs/source/docs/"
	g := &fakeGetter{dir: dir, files: map[string]string{raw + "programming/estimator.md": page, raw + "index.md": page}}
	src := sources.Source{ID: "photon", URL: "https://api.github.com/repos/PhotonVision/photonvision/git/trees/v2026.3.4?recursive=1",
		Library: "photonvision", Season: "2026", Channel: "stable", Version: "v2026.3.4", License: "CC-BY-4.0",
		Trust: "vendor", Include: "docs/source/", Skip: []string{"docs/contributing/"}, URLStyle: "html",
		BaseURL: "https://docs.photonvision.org/en/v2026.3.4/"}
	var got []index.Chunk
	st, err := Parse(context.Background(), g, treePath, src, time.Unix(0, 0), func(c index.Chunk) error { got = append(got, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if st.Pages != 2 || len(got) != 2 || len(g.hits) != 2 {
		t.Fatalf("stats %+v chunks %d hits %v", st, len(got), g.hits)
	}
	c := got[1]
	if c.SourceURL != "https://docs.photonvision.org/en/v2026.3.4/docs/programming/estimator.html" ||
		c.DocID != "photonvision-docs/2026/docs/programming/estimator" || c.UpstreamRev != "v2026.3.4" {
		t.Errorf("provenance: %s %s %s", c.SourceURL, c.DocID, c.UpstreamRev)
	}
}

func TestParseTreeURL(t *testing.T) {
	r, err := ParseTreeURL("https://api.github.com/repos/SleipnirGroup/Choreo/git/trees/v2026.0.3?recursive=1")
	if err != nil || r != (Repo{"SleipnirGroup", "Choreo", "v2026.0.3"}) {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := ParseTreeURL("https://codeload.github.com/a/b/tar.gz/x"); err == nil {
		t.Fatal("want error")
	}
}

func TestPageURL(t *testing.T) {
	for _, tc := range []struct {
		style string
		lower bool
		stem  string
		slug  string
		want  string
	}{
		{"html", false, "docs/a/b", "", "https://x/docs/a/b.html"},
		{"html", true, "pplib-Getting-Started", "", "https://x/pplib-getting-started.html"},
		{"dir", false, "choreolib/getting-started", "", "https://x/choreolib/getting-started/"},
		{"dir", false, "index", "", "https://x/"},
		{"plain", false, "getting-started/installation/index", "", "https://x/getting-started/installation"},
		{"plain", false, "whatever", "/custom/slug", "https://x/custom/slug"},
	} {
		src := sources.Source{BaseURL: "https://x/", URLStyle: tc.style, Lowercase: tc.lower}
		if got := PageURL(src, tc.stem, tc.slug); got != tc.want {
			t.Errorf("%s %s: %s, want %s", tc.style, tc.stem, got, tc.want)
		}
	}
}
