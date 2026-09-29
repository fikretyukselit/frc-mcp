package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/fikretyukselit/frc-mcp/internal/agenteval"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

func runCmd(ctx context.Context, args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	tasksDir := fs.String("tasks", "eval/tasks", "task directory")
	indexDir := fs.String("index", index.DefaultDir(), "shard directory (served to the agent in the mcp condition)")
	cache := fs.String("cache", defaultCache(), "Maven jar cache")
	out := fs.String("out", "", "results JSONL (appended; finished runs are skipped, so a run can be resumed)")
	keep := fs.String("keep", "", "copy each run's workspace and transcript here (default: next to --out)")
	model := fs.String("model", "sonnet", "agent model")
	claude := fs.String("claude", "claude", "claude CLI")
	frcmcp := fs.String("frc-mcp", "", "frc-mcp binary for the mcp condition (default: build ./cmd/frc-mcp)")
	trials := fs.Int("trials", 1, "trials per task and condition")
	parallel := fs.Int("parallel", 2, "concurrent agent runs")
	only := fs.String("only", "", "comma-separated task ids")
	conds := fs.String("conditions", agenteval.WithMCP+","+agenteval.Baseline, "conditions to run")
	timeout := fs.Duration("timeout", 15*time.Minute, "per-run agent timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is required")
	}
	if *keep == "" {
		*keep = strings.TrimSuffix(*out, filepath.Ext(*out)) + ".runs"
	}
	tasks, err := agenteval.LoadTasks(*tasksDir)
	if err != nil {
		return err
	}
	if *only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(*only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		var sel []agenteval.Task
		for _, t := range tasks {
			if want[t.ID] {
				sel = append(sel, t)
			}
		}
		tasks = sel
	}
	absIndex, err := filepath.Abs(*indexDir)
	if err != nil {
		return err
	}
	if *frcmcp == "" {
		*frcmcp = filepath.Join(os.TempDir(), "agenteval-frc-mcp")
		if b, err := exec.CommandContext(ctx, "go", "build", "-o", *frcmcp, "./cmd/frc-mcp").CombinedOutput(); err != nil { //nolint:gosec // G204: builds this repo's own binary
			return fmt.Errorf("build frc-mcp: %w: %s", err, b)
		}
	}
	e, err := openEnv(ctx, *indexDir, *cache)
	if err != nil {
		return err
	}
	defer e.close()
	done := map[string]bool{}
	if prev, err := agenteval.ReadResults(*out); err == nil {
		for _, r := range prev {
			done[r.Key()] = true
		}
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // G302: results are meant to be shared
	if err != nil {
		return err
	}
	defer f.Close()
	var mu sync.Mutex
	agent := agenteval.Agent{Bin: *claude, Model: *model, FRCMCP: *frcmcp, Index: absIndex, Timeout: *timeout}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(*parallel)
	for trial := 1; trial <= *trials; trial++ {
		for _, t := range tasks {
			for _, cond := range strings.Split(*conds, ",") {
				r := agenteval.Result{Task: t.ID, Season: t.Season, Tags: t.Tags, Condition: cond, Trial: trial, Model: *model}
				if done[r.Key()] {
					continue
				}
				g.Go(func() error {
					res, err := runOne(gctx, e, agent, t, r, *keep, log)
					if err != nil {
						return fmt.Errorf("%s: %w", r.Key(), err)
					}
					b, _ := json.Marshal(res)
					mu.Lock()
					defer mu.Unlock()
					_, err = f.Write(append(b, '\n'))
					log.Info("run", "task", t.ID, "cond", cond, "trial", trial, "compile", res.Compile.Pass,
						"errors", res.Compile.Errors, "frc_calls", res.Agent.FRCCalls, "cost", res.Agent.CostUSD, "s", res.Agent.Millis/1000)
					return err
				})
			}
		}
	}
	return g.Wait()
}

func runOne(ctx context.Context, e *env, agent agenteval.Agent, t agenteval.Task, r agenteval.Result, keep string, log *slog.Logger) (agenteval.Result, error) {
	s := agenteval.Seasons()[t.Season]
	deps, err := e.vendordeps(t.Season, t.Vendordeps)
	if err != nil {
		return r, err
	}
	cp, proc, err := e.classpath(ctx, s, deps)
	if err != nil {
		return r, err
	}
	tmp, err := os.MkdirTemp("", "agenteval-")
	if err != nil {
		return r, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	ws, err := agenteval.NewWorkspace(tmp, s, deps)
	if err != nil {
		return r, err
	}
	dst := filepath.Join(keep, r.Task, r.Condition, fmt.Sprint(r.Trial))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return r, err
	}
	tr, err := os.Create(filepath.Join(dst, "transcript.jsonl"))
	if err != nil {
		return r, err
	}
	defer tr.Close()
	r.Agent, err = agent.Run(ctx, ws, t.Prompt, r.Condition, tr)
	if err != nil {
		return r, err
	}
	if r.Condition == agenteval.WithMCP && !hasServer(r.Agent.MCPServers, "frc:connected") {
		log.Warn("frc-mcp did not connect", "task", t.ID, "servers", r.Agent.MCPServers)
	}
	if r.Condition == agenteval.Baseline && len(r.Agent.MCPServers) > 0 {
		return r, fmt.Errorf("baseline run had MCP servers %v; the A/B is invalid", r.Agent.MCPServers)
	}
	r.Compile = agenteval.Compile(ctx, e.javac, s.Release, cp, proc, ws, t.Files)
	r.VerifyErrors = verifyDir(ctx, e, ws, t.Season)
	r.Dir, r.At = dst, time.Now().UTC().Format(time.RFC3339)
	return r, copyTree(filepath.Join(ws, "src"), filepath.Join(dst, "src"))
}

func hasServer(servers []string, want string) bool {
	for _, s := range servers {
		if s == want {
			return true
		}
	}
	return false
}

func verifyDir(ctx context.Context, e *env, root, season string) int {
	n := 0
	_ = filepath.WalkDir(filepath.Join(root, "src"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".java") {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // G122: the eval's own temp workspace
		if err == nil {
			n += verify.Check(ctx, e.engine, string(b), "java", season, verify.Options{}).Errors
		}
		return nil
	})
	return n
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a missing src (agent wrote nothing) is not an eval error
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p) //nolint:gosec // G122: the eval's own temp workspace
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644) //nolint:gosec // G703: target is under the run's own keep directory
	})
}

func reportCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agenteval report FILE…")
	}
	rs, err := agenteval.ReadResults(args...)
	if err != nil {
		return err
	}
	byModel := map[string][]agenteval.Result{}
	for _, r := range rs {
		byModel[r.Model] = append(byModel[r.Model], r)
	}
	for _, m := range sortedKeys(byModel) {
		fmt.Println(agenteval.Summarize(byModel[m]).Markdown())
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
