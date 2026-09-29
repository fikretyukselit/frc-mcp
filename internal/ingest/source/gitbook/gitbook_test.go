package gitbook

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

const llms = `# REV

## REVLib
- [REVLib](https://docs.revrobotics.com/revlib/revlib.md)
- [Closed Loop](https://docs.revrobotics.com/revlib/spark/closed-loop.md): gains
- [Dup](https://docs.revrobotics.com/revlib/revlib.md)
- [ION](https://docs.revrobotics.com/ion-control/sw/revlib.md)
- [Evil](https://evil.example/revlib/x.md)
`

type fakeGetter struct {
	dir  string
	hits []string
}

func (f *fakeGetter) Get(_ context.Context, u string) (*fetch.Result, error) {
	f.hits = append(f.hits, u)
	p := filepath.Join(f.dir, filepath.Base(u))
	body := "# " + filepath.Base(u) + "\n\nConfigure the SPARK MAX closed loop controller with SparkMaxConfig and apply it to the device.\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return nil, err
	}
	return &fetch.Result{URL: u, Path: p, ETag: `W/"abc"`, SHA256: "0123456789abcdef0123"}, nil
}

func TestEntries(t *testing.T) {
	base, _ := url.Parse("https://docs.revrobotics.com/llms.txt")
	es := Entries(llms, base, "/revlib/")
	if len(es) != 2 || es[0].MD != "https://docs.revrobotics.com/revlib/revlib.md" || es[1].Title != "Closed Loop" {
		t.Fatalf("%+v", es)
	}
}

func TestParse(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, "llms.txt")
	if err := os.WriteFile(lp, []byte(llms), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGetter{dir: dir}
	src := sources.Source{URL: "https://docs.revrobotics.com/llms.txt", Include: "/revlib/", Skip: []string{"spark/"},
		Library: "revlib", Season: "2026", Channel: "stable", Version: "2026", License: "LicenseRef-REV-Docs", Trust: "vendor"}
	var got []index.Chunk
	st, err := Parse(context.Background(), g, lp, src, time.Unix(0, 0), func(c index.Chunk) error { got = append(got, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if st.Pages != 1 || len(g.hits) != 1 || got[0].SourceURL != "https://docs.revrobotics.com/revlib/revlib" ||
		got[0].DocID != "revlib-docs/2026/revlib/revlib" || got[0].UpstreamRev != "abc" {
		t.Fatalf("stats %+v hits %v chunk %+v", st, g.hits, got)
	}
}
