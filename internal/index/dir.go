package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// OpenDir opens every *.sqlite shard in dir, sorted by file name. Shards that
// fail to open (e.g. incompatible schema) are skipped and reported in errs so
// one bad shard never takes the server down. A missing dir is not an error.
func OpenDir(ctx context.Context, dir string) (shards []*Reader, errs []error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.sqlite"))
	if err != nil {
		return nil, []error{err}
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
