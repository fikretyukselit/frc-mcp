package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/project"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

// verifyCmd checks every Java file of a robot project against the season
// detected from its build files (or --season). It is the CLI twin of
// frc_verify_code and the harness for the false-positive gate.
func verifyCmd(ctx context.Context, args []string) error {
	fset := flag.NewFlagSet("verify", flag.ContinueOnError)
	dir := fset.String("index", index.DefaultDir(), "shard directory")
	season := fset.String("season", "", "FRC season (default: detected from the project)")
	quiet := fset.Bool("summary", false, "print only the summary line")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		return errors.New("usage: frc-mcp verify [--season 2026] PROJECT_DIR")
	}
	root := fset.Arg(0)
	if *season == "" {
		p, err := project.Detect(root)
		if err != nil {
			return fmt.Errorf("%w (pass --season)", err)
		}
		*season = p.Season
	}
	e, shards := openEngine(ctx, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), *dir, "", false)
	if e == nil {
		return errors.New("no shards in " + *dir)
	}
	defer func() {
		for _, s := range shards {
			s.Close()
		}
	}()
	var files, lines, errs, warns, infos int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "build" || d.Name() == ".git" || d.Name() == "bin" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // G122: CLI walks the operator's own project directory
		if err != nil {
			return err
		}
		files++
		lines += strings.Count(string(b), "\n") + 1
		r := verify.Java(ctx, e, string(b), *season)
		rel, _ := filepath.Rel(root, path)
		for _, f := range r.Findings {
			switch f.Severity {
			case "error":
				errs++
			case "warning":
				warns++
			default:
				infos++
			}
			if !*quiet && f.Severity != "info" {
				fmt.Printf("%s:%d:%d: %s [%s] %s", rel, f.Line, f.Col, f.Severity, f.Kind, f.Message)
				if f.Fix != "" {
					fmt.Printf(" → %s", f.Fix)
				}
				fmt.Println()
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("SUMMARY %s season=%s files=%d lines=%d errors=%d warnings=%d info=%d errors/kLOC=%.3f\n",
		root, *season, files, lines, errs, warns, infos, float64(errs)*1000/float64(max(lines, 1)))
	return nil
}
