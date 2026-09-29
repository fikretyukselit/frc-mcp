// Package gitbook ingests GitBook sites (REVLib, YAGSL) through the files
// GitBook publishes for LLMs: src.URL is the site's llms.txt, whose Markdown
// link list names every page's .md rendition. Pages under src.Include are
// fetched with conditional GET (so re-runs are mostly 304s) at a polite rate.
package gitbook

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/markdown"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// Getter is the fetch dependency (fetch.Fetcher in production).
type Getter interface {
	Get(ctx context.Context, url string) (*fetch.Result, error)
}

const (
	maxPages = 2000
	// interval keeps us well under common docs-host limits (< 4 req/s).
	interval = 300 * time.Millisecond
)

var link = regexp.MustCompile(`^\s*[-*]\s+\[([^\]]+)\]\((https://[^)\s]+\.md)\)`)

// Stats summarizes a parse.
type Stats struct{ Pages, Chunks, Skipped int }

// Entry is one page listed in llms.txt.
type Entry struct{ Title, MD string }

// Entries parses llms.txt and returns same-host pages under include, sorted
// and de-duplicated.
func Entries(llms string, base *url.URL, include string) []Entry {
	seen := map[string]bool{}
	var out []Entry
	for _, line := range strings.Split(llms, "\n") {
		m := link.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		u, err := url.Parse(m[2])
		if err != nil || u.Host != base.Host || !strings.HasPrefix(u.Path, include) || seen[u.Path] {
			continue
		}
		seen[u.Path] = true
		out = append(out, Entry{Title: m[1], MD: u.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MD < out[j].MD })
	if len(out) > maxPages {
		out = out[:maxPages]
	}
	return out
}

// Parse reads llms.txt at llmsPath, then fetches and chunks each page.
func Parse(ctx context.Context, g Getter, llmsPath string, src sources.Source, retrieved time.Time, emit func(index.Chunk) error) (Stats, error) {
	var st Stats
	b, err := os.ReadFile(llmsPath)
	if err != nil {
		return st, err
	}
	base, err := url.Parse(src.URL)
	if err != nil {
		return st, err
	}
	last := time.Time{}
	for _, e := range Entries(string(b), base, src.Include) {
		rel := strings.TrimSuffix(strings.TrimPrefix(mustPath(e.MD), "/"), ".md")
		if skipped(strings.TrimPrefix("/"+rel, src.Include), src.Skip) {
			continue
		}
		if wait := interval - time.Since(last); wait > 0 {
			select {
			case <-ctx.Done():
				return st, ctx.Err()
			case <-time.After(wait):
			}
		}
		last = time.Now()
		r, err := g.Get(ctx, e.MD)
		if err != nil {
			return st, fmt.Errorf("gitbook: %s: %w", e.MD, err)
		}
		text, err := os.ReadFile(r.Path)
		if err != nil {
			return st, err
		}
		rev := strings.Trim(r.ETag, `"W/`)
		if rev == "" {
			rev = "sha256:" + r.SHA256[:16]
		}
		cs := markdown.Chunks(markdown.Page{Rel: rel, URL: strings.TrimSuffix(e.MD, ".md"), Title: e.Title, Text: string(text)},
			src, rev, retrieved)
		if len(cs) == 0 {
			st.Skipped++
			continue
		}
		st.Pages++
		for _, c := range cs {
			if err := emit(c); err != nil {
				return st, err
			}
			st.Chunks++
		}
	}
	return st, nil
}

func mustPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Path
}

func skipped(rel string, skip []string) bool {
	for _, s := range skip {
		if strings.HasPrefix(strings.TrimPrefix(rel, "/"), s) {
			return true
		}
	}
	return false
}
