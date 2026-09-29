package main

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// BenchmarkSearchRealIndex measures frc_search over the real index built by
// `make index` (skipped when .shards is absent). CPU-profile it with
// -cpuprofile to find driver-level costs (see docs/benchmarks.md §M3).
func BenchmarkSearchRealIndex(b *testing.B) {
	if _, err := os.Stat("../../.shards"); err != nil {
		b.Skip("no ../../.shards; run `make index`")
	}
	e, shards := openEngine(context.Background(), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), "../../.shards", "", true)
	if e == nil {
		b.Skip("no shards")
	}
	b.Cleanup(func() {
		for _, s := range shards {
			s.Close()
		}
	})
	qs := []string{"How do I configure Motion Magic on a TalonFX?", "SwerveDriveKinematics", "swerve odometry with photonvision",
		"closed loop position control on a SPARK MAX with REVLib", "command based programming"}
	i := 0
	for b.Loop() {
		if _, err := e.Search(context.Background(), retrieve.Query{Text: qs[i%len(qs)]}); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
