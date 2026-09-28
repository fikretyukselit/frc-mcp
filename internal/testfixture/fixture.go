// Package testfixture builds the synthetic fixture shard for tests across
// packages. It is imported only from _test files.
package testfixture

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// Root is the absolute path of testdata/fixture.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixture")
}

// Shard builds and opens the fixture shard in a temp dir.
func Shard(tb testing.TB) *index.Reader {
	tb.Helper()
	ctx := context.Background()
	out := filepath.Join(tb.TempDir(), "fixture.sqlite")
	if _, err := index.BuildFromJSONL(ctx, out, "fixture", filepath.Join(Root(), "chunks.jsonl"), filepath.Join(Root(), "symbols.jsonl")); err != nil {
		tb.Fatal(err)
	}
	r, err := index.Open(ctx, out)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { r.Close() })
	return r
}

// Engine returns an engine over the fixture shard.
func Engine(tb testing.TB) *retrieve.Engine {
	return retrieve.New([]*index.Reader{Shard(tb)}, retrieve.Options{})
}
