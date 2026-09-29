// Package repomd ingests Markdown documentation kept in a GitHub repository
// (PhotonVision, AdvantageKit, Choreo, PathPlanner). src.URL is the git tree
// API URL of a pinned tag or branch (…/git/trees/<ref>?recursive=1), so the
// season is exactly the season of that ref: 2026 docs come from the last 2026
// release tag, 2027 docs from the alpha tag. Only the Markdown files under
// src.Include are downloaded, from raw.githubusercontent.com with conditional
// GET; a tag's files never change, so re-runs are served from the cache.
// (A codeload tarball would be one request, but PhotonVision's is ~195 MB for
// ~100 Markdown files.)
package repomd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
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
	maxPage  = 2 << 20 // larger files are generated content
	maxFiles = 3000
)

type tree struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

// Stats summarizes a parse.
type Stats struct{ Pages, Chunks, Skipped int }

// Repo is the owner/name/ref triple parsed from a git tree API URL.
type Repo struct{ Owner, Name, Ref string }

// ParseTreeURL parses https://api.github.com/repos/<owner>/<name>/git/trees/<ref>.
func ParseTreeURL(raw string) (Repo, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Repo{}, err
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Host != "api.github.com" || len(p) < 6 || p[0] != "repos" || p[3] != "git" || p[4] != "trees" {
		return Repo{}, fmt.Errorf("repomd: %s is not a git tree API URL", raw)
	}
	return Repo{Owner: p[1], Name: p[2], Ref: strings.Join(p[5:], "/")}, nil
}

// Parse reads the tree listing at treePath, then fetches and chunks every
// .md/.mdx file under src.Include that is not under a src.Skip prefix.
func Parse(ctx context.Context, g Getter, treePath string, src sources.Source, retrieved time.Time, emit func(index.Chunk) error) (Stats, error) {
	var st Stats
	repo, err := ParseTreeURL(src.URL)
	if err != nil {
		return st, err
	}
	b, err := os.ReadFile(treePath)
	if err != nil {
		return st, err
	}
	var t tree
	if err := json.Unmarshal(b, &t); err != nil {
		return st, fmt.Errorf("repomd: tree %s: %w", src.URL, err)
	}
	if t.Truncated {
		return st, fmt.Errorf("repomd: tree %s is truncated; narrow the ref", src.URL)
	}
	var files []string
	for _, e := range t.Tree {
		rel, ok := strings.CutPrefix(e.Path, src.Include)
		isMD := strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".mdx")
		if e.Type != "blob" || !ok || e.Size > maxPage || !isMD || skipped(rel, src.Skip) {
			continue
		}
		files = append(files, e.Path)
	}
	sort.Strings(files) // deterministic build
	if len(files) > maxFiles {
		return st, fmt.Errorf("repomd: %s: %d files exceeds the %d cap", src.ID, len(files), maxFiles)
	}
	for _, f := range files {
		raw := "https://raw.githubusercontent.com/" + repo.Owner + "/" + repo.Name + "/" + repo.Ref + "/" + escapePath(f)
		r, err := g.Get(ctx, raw)
		if err != nil {
			return st, fmt.Errorf("repomd: %s: %w", f, err)
		}
		text, err := os.ReadFile(r.Path)
		if err != nil {
			return st, err
		}
		rel := strings.TrimPrefix(f, src.Include)
		stem := strings.TrimSuffix(strings.TrimSuffix(rel, ".mdx"), ".md")
		fm, _ := markdown.FrontMatter(string(text))
		cs := markdown.Chunks(markdown.Page{Rel: stem, URL: PageURL(src, stem, fm["slug"]), Title: fm["title"], Text: string(text)},
			src, repo.Ref, retrieved)
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

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func skipped(rel string, skip []string) bool {
	for _, s := range skip {
		if strings.HasPrefix(rel, s) || path.Base(rel) == s {
			return true
		}
	}
	return false
}

// PageURL maps a file stem to the published page URL for the site generator.
// A front-matter slug starting with "/" (Docusaurus) overrides the path.
func PageURL(src sources.Source, stem, slug string) string {
	if strings.HasPrefix(slug, "/") {
		return strings.TrimSuffix(src.BaseURL, "/") + slug
	}
	p := stem
	if src.Lowercase {
		p = strings.ToLower(p)
	}
	switch src.URLStyle {
	case "html":
		p += ".html"
	case "dir":
		p = strings.TrimSuffix(strings.TrimSuffix(p, "index"), "/")
		if p != "" {
			p += "/"
		}
	default: // plain
		p = strings.TrimSuffix(strings.TrimSuffix(p, "index"), "/")
	}
	return src.BaseURL + p
}
