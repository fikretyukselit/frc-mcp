package index

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OpenDir opens every *.sqlite shard in dir, sorted by file name. Shards that
// fail to open (e.g. incompatible schema) are skipped and reported in errs so
// one bad shard never takes the server down. A missing dir is not an error.
//
// When dir holds a synced manifest.json (frc-mcp sync), only the shards it
// lists are opened, so a previous version's files that are still on disk are
// never loaded next to the new ones.
func OpenDir(ctx context.Context, dir string) (shards []*Reader, errs []error) {
	paths, err := manifestShards(dir)
	if err != nil {
		errs = append(errs, err)
	}
	if paths == nil {
		if paths, err = filepath.Glob(filepath.Join(dir, "*.sqlite")); err != nil {
			return nil, []error{err}
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		r, err := Open(ctx, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		shards = append(shards, r)
	}
	if len(paths) == 0 {
		if _, err := os.Stat(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("index: %s: %w", dir, err))
		}
	}
	return shards, errs
}

// manifestShards returns the shard paths listed in dir/manifest.json, or nil
// when there is no manifest.
func manifestShards(dir string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m struct {
		Shards []struct {
			DB struct {
				Name string `json:"name"`
			} `json:"db"`
		} `json:"shards"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("index: %s/manifest.json: %w", dir, err)
	}
	out := []string{}
	for _, s := range m.Shards {
		if n := s.DB.Name; n != "" && !strings.Contains(n, "..") && !filepath.IsAbs(n) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out, nil
}

// DefaultEmbedModel is the lite-profile embedding model (ADR-0002/0005).
const DefaultEmbedModel = "potion-code-16M-v2"

// VectorPath is where a shard's vector layer for a model lives.
func VectorPath(shardPath, modelID string) string {
	return strings.TrimSuffix(shardPath, ".sqlite") + "." + modelID + ".vec"
}

// ModelDir is where models are stored under a shard directory.
func ModelDir(shardDir, modelID string) string { return filepath.Join(shardDir, "models", modelID) }

// DefaultDir is the per-user shard directory:
// $XDG_CACHE_HOME/frc-mcp/shards (Linux), ~/Library/Caches/frc-mcp/shards
// (macOS), %LocalAppData%\frc-mcp\shards (Windows).
func DefaultDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "frc-mcp", "shards")
}
