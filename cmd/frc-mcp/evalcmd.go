package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/eval"
	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// evalCmd runs the retrieval eval (docs/retrieval.md §6). With --ab it runs
// with and without dense retrieval to apply the M1 exit rule.
func evalCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	dir := fs.String("index", index.DefaultDir(), "shard directory")
	path := fs.String("queries", "eval/queries.jsonl", "judged queries")
	ab := fs.Bool("ab", false, "compare BM25+symbol with and without dense retrieval")
	out := fs.String("json", "", "write the full report(s) as JSON to this file")
	failures := fs.Int("failures", 10, "print up to N failed queries")
	baseline := fs.String("baseline", "", "compare the hybrid arm to this baseline and fail on regression")
	writeBaseline := fs.Bool("write-baseline", false, "write the hybrid overall metrics to --baseline")
	if err := fs.Parse(args); err != nil {
		return err
	}
	qs, err := eval.Load(*path)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	type arm struct {
		name  string
		dense bool
	}
	arms := []arm{{"hybrid", true}}
	if *ab {
		arms = []arm{{"lexical", false}, {"hybrid", true}}
	}
	reports := map[string]*eval.Report{}
	for _, a := range arms {
		e, shards := openEngine(ctx, log, *dir, a.dense)
		if e == nil {
			return errors.New("no shards in " + *dir)
		}
		if err := eval.Validate(ctx, e, qs); err != nil {
			return err
		}
		rep, err := eval.Run(ctx, e, qs)
		e.Close()
		for _, s := range shards {
			s.Close()
		}
		if err != nil {
			return err
		}
		reports[a.name] = rep
		fmt.Printf("\n== %s (dense=%v) · %d queries\n", a.name, e.Dense(), rep.Overall.N)
		printMetrics("overall", rep.Overall)
		keys := make([]string, 0, len(rep.ByBucket))
		for k := range rep.ByBucket {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			printMetrics("  "+k, rep.ByBucket[k])
		}
		for _, k := range []string{"train", "holdout"} {
			if m, ok := rep.BySplit[k]; ok {
				printMetrics("  split:"+k, m)
			}
		}
		for i, f := range rep.Failures {
			if i >= *failures {
				fmt.Printf("  … %d more failures\n", len(rep.Failures)-i)
				break
			}
			fmt.Printf("  ✗ %s %q\n      want %v\n      got  %v\n", f.ID, f.Query, f.Want, f.Got)
		}
	}
	if *ab {
		d := reports["hybrid"].Overall.NDCG10 - reports["lexical"].Overall.NDCG10
		verdict := "KEEP dense (≥ +0.01 nDCG@10)"
		if d < 0.01 {
			verdict = "DROP dense (< +0.01 nDCG@10) per the M1 exit rule"
		}
		fmt.Printf("\ndense Δ nDCG@10 = %+.3f → %s\n", d, verdict)
	}
	if *out != "" {
		b, _ := json.MarshalIndent(reports, "", "  ")
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			return err
		}
	}
	if *baseline != "" {
		cur := reports["hybrid"].Overall
		if *writeBaseline {
			b, _ := json.MarshalIndent(cur, "", "  ")
			return os.WriteFile(*baseline, append(b, '\n'), 0o644)
		}
		return gate(*baseline, cur)
	}
	return nil
}

// Regression gate (docs/retrieval.md §6): fail if Recall@10 or nDCG@10 drop
// by more than 1.5 points, or wrong-season@5 rises above zero.
const gateTolerance = 0.015

func gate(path string, cur eval.Metrics) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var base eval.Metrics
	if err := json.Unmarshal(b, &base); err != nil {
		return err
	}
	var fails []string
	if d := cur.Recall10 - base.Recall10; d < -gateTolerance {
		fails = append(fails, fmt.Sprintf("Recall@10 %.3f → %.3f (%+.3f)", base.Recall10, cur.Recall10, d))
	}
	if d := cur.NDCG10 - base.NDCG10; d < -gateTolerance {
		fails = append(fails, fmt.Sprintf("nDCG@10 %.3f → %.3f (%+.3f)", base.NDCG10, cur.NDCG10, d))
	}
	if cur.WrongSeason5 > 0 {
		fails = append(fails, fmt.Sprintf("wrong-season@5 = %.3f (must be 0)", cur.WrongSeason5))
	}
	if len(fails) > 0 {
		return fmt.Errorf("eval regression vs %s:\n  %s", path, strings.Join(fails, "\n  "))
	}
	fmt.Printf("\ngate: OK vs %s (R@10 %.3f → %.3f, nDCG@10 %.3f → %.3f)\n", path, base.Recall10, cur.Recall10, base.NDCG10, cur.NDCG10)
	return nil
}

func printMetrics(label string, m eval.Metrics) {
	fmt.Printf("%-22s n=%-4d R@5 %.3f  R@10 %.3f  nDCG@10 %.3f  MRR %.3f  wrong-season@5 %.3f  no-match %.3f  low-conf %.3f  p50 %.1fms p95 %.1fms\n",
		label, m.N, m.Recall5, m.Recall10, m.NDCG10, m.MRR10, m.WrongSeason5, m.NoMatch, m.LowConf, m.P50ms, m.P95ms)
}
