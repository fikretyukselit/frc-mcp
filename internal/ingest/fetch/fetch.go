// Package fetch is the ingestion plane's HTTP layer: conditional GET
// (ETag / Last-Modified) through netguard, with an on-disk cache so repeated
// index runs cost a 304 per unchanged source (docs/ingestion.md §2).
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/netguard"
)

// Fetcher downloads through a guarded client into a content cache.
type Fetcher struct {
	Client *http.Client
	Dir    string // cache directory
}

// Result describes a fetched resource.
type Result struct {
	URL          string    `json:"url"`
	Path         string    `json:"path"` // local file with the body
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
	FetchedAt    time.Time `json:"fetched_at"`
	NotModified  bool      `json:"-"` // served from cache after a 304
}

// New builds a fetcher for an allowlist.
func New(dir, userAgent string, hosts []string, maxBody int64) *Fetcher {
	p := &netguard.Policy{Hosts: hosts, UserAgent: userAgent, MaxBody: maxBody, Timeout: 10 * time.Minute}
	return &Fetcher{Client: p.Client(), Dir: dir}
}

// Get returns the resource, revalidating a cached copy with conditional GET.
func (f *Fetcher) Get(ctx context.Context, url string) (*Result, error) {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(url))
	base := filepath.Join(f.Dir, hex.EncodeToString(key[:])[:24])
	var cached *Result
	if b, err := os.ReadFile(base + ".json"); err == nil {
		var r Result
		if json.Unmarshal(b, &r) == nil {
			if _, err := os.Stat(r.Path); err == nil {
				cached = &r
			}
		}
	}
	hdr := http.Header{}
	if cached != nil {
		if cached.ETag != "" {
			hdr.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			hdr.Set("If-Modified-Since", cached.LastModified)
		}
	}
	resp, err := netguard.Get(ctx, f.Client, url, hdr)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		cached.NotModified = true
		return cached, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	tmp := base + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), resp.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	r := &Result{URL: url, Path: base + ".body", ETag: resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"), SHA256: hex.EncodeToString(h.Sum(nil)), Size: n,
		FetchedAt: time.Now().UTC()}
	if err := os.Rename(tmp, r.Path); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(base+".json", b, 0o644); err != nil {
		return nil, err
	}
	return r, nil
}

// ErrNoCache is returned by Offline when nothing is cached for a URL.
var ErrNoCache = errors.New("fetch: not cached")
