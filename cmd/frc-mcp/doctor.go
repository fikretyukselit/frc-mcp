package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// doctor reports the local index state and measures open + query latency on
// this machine (docs budgets: open ≤ 150 ms, search p95 ≤ 50 ms).
func doctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dir := fs.String("index", index.DefaultDir(), "directory containing *.sqlite shards")
	n := fs.Int("n", 200, "benchmark iterations")
	noDense := fs.Bool("no-dense", false, "disable dense retrieval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Printf("frc-mcp %s\nindex dir: %s\n", buildVersion(), *dir)
	t0 := time.Now()
	e, shards := openEngine(ctx, slog.New(slog.NewTextHandler(os.Stderr, nil)), *dir, !*noDense)
	open := time.Since(t0)
	if len(shards) == 0 {
		fmt.Println("  no shards found — build one with `frc-mcp index build` (sync arrives in M2)")
		return nil
	}
	defer func() {
		for _, s := range shards {
			s.Close()
		}
	}()
	for _, s := range shards {
		m := s.Meta()
		fmt.Printf("  ✓ %-24s schema v%d · %d chunks · %d symbols · built %s · %v\n", m.Name, m.Schema, m.Chunks,
			m.Symbols, m.BuiltAt.Format(time.RFC3339), m.Seasons)
	}
	fmt.Printf("default season: %s · dense: %v · open (shards + model + vector layers): %s\n", e.DefaultSeason(), e.Dense(), open.Round(time.Microsecond))

	queries := []string{"How do I configure Motion Magic on a TalonFX?", "SwerveDriveKinematics",
		"swerve odometry with vision measurements", "CANSparkMax current limit", "what changed in 2027"}
	lat := make([]time.Duration, 0, *n)
	for i := range *n {
		q := queries[i%len(queries)]
		t := time.Now()
		if _, err := e.Search(ctx, retrieve.Query{Text: q}); err != nil {
			fmt.Fprintln(os.Stderr, "search:", err)
			return err
		}
		lat = append(lat, time.Since(t))
	}
	slices.Sort(lat)
	p := func(q float64) time.Duration { return lat[int(q*float64(len(lat)-1))].Round(time.Microsecond) }
	fmt.Printf("frc_search latency over %d queries: p50 %s · p95 %s · p99 %s · max %s (budget p95 ≤ 50ms)\n",
		len(lat), p(0.50), p(0.95), p(0.99), lat[len(lat)-1].Round(time.Microsecond))
	return nil
}
