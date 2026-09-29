package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/agenteval"
	"github.com/fikretyukselit/frc-mcp/internal/index"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func compileCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	season := fs.String("season", "2027", "season toolchain")
	indexDir := fs.String("index", index.DefaultDir(), "shard directory (vendordep catalog)")
	cache := fs.String("cache", defaultCache(), "Maven jar cache")
	var deps multi
	fs.Var(&deps, "vendordep", "catalog vendordep name (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: agenteval compile --season 2027 [--vendordep NAME] DIR")
	}
	e, err := openEnv(ctx, *indexDir, *cache)
	if err != nil {
		return err
	}
	defer e.close()
	s, ok := agenteval.Seasons()[*season]
	if !ok {
		return fmt.Errorf("season %s not supported", *season)
	}
	vds, err := e.vendordeps(*season, deps)
	if err != nil {
		return err
	}
	cp, proc, err := e.classpath(ctx, s, vds)
	if err != nil {
		return err
	}
	r := agenteval.Compile(ctx, e.javac, s.Release, cp, proc, fs.Arg(0), nil)
	fmt.Print(r.Output)
	fmt.Printf("COMPILE %s season=%s pass=%v errors=%d files=%d ms=%d\n", fs.Arg(0), *season, r.Pass, r.Errors, r.Files, r.Millis)
	if !r.Pass {
		return failure("compile failed")
	}
	return nil
}

// referencesCmd compiles every task's reference solution
// (eval/tasks/reference/<id>/src/main/java/…): a task whose reference does
// not compile is a broken task, not an agent failure.
func referencesCmd(ctx context.Context, args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("references", flag.ContinueOnError)
	tasksDir := fs.String("tasks", "eval/tasks", "task directory")
	indexDir := fs.String("index", index.DefaultDir(), "shard directory")
	cache := fs.String("cache", defaultCache(), "Maven jar cache")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tasks, err := agenteval.LoadTasks(*tasksDir)
	if err != nil {
		return err
	}
	e, err := openEnv(ctx, *indexDir, *cache)
	if err != nil {
		return err
	}
	defer e.close()
	bad := 0
	for _, t := range tasks {
		ref := filepath.Join(*tasksDir, "reference", t.ID)
		if _, err := os.Stat(ref); err != nil {
			fmt.Printf("MISSING %s\n", t.ID)
			bad++
			continue
		}
		s := agenteval.Seasons()[t.Season]
		vds, err := e.vendordeps(t.Season, t.Vendordeps)
		if err != nil {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		cp, proc, err := e.classpath(ctx, s, vds)
		if err != nil {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		r := agenteval.Compile(ctx, e.javac, s.Release, cp, proc, ref, t.Files)
		status := "PASS"
		if !r.Pass {
			status, bad = "FAIL", bad+1
			log.Error("reference does not compile", "task", t.ID, "missing", r.Missing, "output", r.Output)
		}
		fmt.Printf("%s %s season=%s errors=%d files=%d\n", status, t.ID, t.Season, r.Errors, r.Files)
	}
	if bad > 0 {
		return failure(fmt.Sprintf("%d of %d references failed", bad, len(tasks)))
	}
	fmt.Printf("all %d references compile\n", len(tasks))
	return nil
}
