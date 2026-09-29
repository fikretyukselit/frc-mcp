package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// A shared (HTTP) server never loads prohibited content, and loads content
// without a license only with --include-unlicensed; a local one loads all.
func TestOpenEngineLicensePolicy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for name, lic := range map[string]string{"open": "BSD-3-Clause", "nolicense": "LicenseRef-REVLib-API",
		"eula": "LicenseRef-CTRE-Phoenix-EULA", "forum": "LicenseRef-ChiefDelphi-UserContent"} {
		w, err := index.Create(ctx, filepath.Join(dir, name+".sqlite"), name)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.AddSymbol(ctx, index.Symbol{FQN: "x." + name + ".T", Library: "x", Version: "1", Season: "2026", Language: "java",
			Kind: "class", Signature: "s", SourceURL: "https://x", UpstreamRev: "r", RetrievedAt: time.Unix(1, 0), License: lic, Trust: "vendor"}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		shared, unlicensed bool
		want               []string
	}{
		{false, true, []string{"eula", "forum", "nolicense", "open"}},
		{true, false, []string{"open"}},
		{true, true, []string{"nolicense", "open"}},
	} {
		_, shards := openEngineWith(ctx, log, dir, "", false, tc.shared, tc.unlicensed)
		var got []string
		for _, s := range shards {
			got = append(got, s.Meta().Name)
			s.Close()
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("shared=%v unlicensed=%v: %v, want %v", tc.shared, tc.unlicensed, got, tc.want)
		}
	}
}
