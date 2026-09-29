package index

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestLicensesCoverFactRows: a shard of hardware rows has no chunks, and its
// LicenseRef-* rows must still keep it out of a publish (dist.Publish reads
// Licenses).
func TestLicensesCoverFactRows(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "hardware-restricted.sqlite")
	w, err := Create(ctx, p, "hardware-restricted")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddHWSpec(ctx, HWSpec{Part: "neo", Name: "NEO", Category: "motor", Source: "rev-docs", Season: "all",
		Fields: map[string]float64{"stall_torque_nm": 2.6}, SourceURL: "https://docs.revrobotics.com/brushless/neo/v1.1",
		UpstreamRev: "etag", RetrievedAt: time.Unix(0, 0), License: "LicenseRef-REV-Docs-NoLicense", Trust: "vendor"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Licenses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"LicenseRef-REV-Docs-NoLicense"}) {
		t.Fatalf("licenses %v", got)
	}
}
