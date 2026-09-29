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
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/migrate"
	"github.com/fikretyukselit/frc-mcp/internal/project"
)

// migrateCmd maps a symbol, or every API reference in a robot project, from
// one season to another. It is the CLI twin of frc_migrate: a porting
// checklist, never a rewrite.
func migrateCmd(ctx context.Context, args []string) error {
	fset := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dir := fset.String("index", index.DefaultDir(), "shard directory")
	from := fset.String("from", "", "source season (default: detected from the project)")
	to := fset.String("to", "", "target season (default: the newest indexed season after from)")
	symbol := fset.String("symbol", "", "map one symbol instead of a project")
	lang := fset.String("language", "", "java, cpp or python (symbol mode; default: any)")
	asJSON := fset.Bool("json", false, "print the full result as JSON")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if (*symbol == "") == (fset.NArg() != 1) {
		return errors.New("usage: frc-mcp migrate [--from 2026] [--to 2027] PROJECT_DIR\n       frc-mcp migrate --symbol ChassisSpeeds [--from 2026] [--to 2027]")
	}
	root := fset.Arg(0)
	if *from == "" && root != "" {
		p, err := project.Detect(root)
		if err != nil {
			return fmt.Errorf("%w (pass --from)", err)
		}
		*from = p.Season
	}
	e, shards := openEngine(ctx, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), *dir, false)
	if e == nil {
		return errors.New("no shards in " + *dir)
	}
	defer func() {
		for _, s := range shards {
			s.Close()
		}
	}()
	if *from == "" {
		*from = e.DefaultSeason()
	}
	if *to == "" {
		for _, s := range e.SymbolSeasons() {
			if s > *from {
				*to = s
				break
			}
		}
		if *to == "" {
			return fmt.Errorf("no indexed season after %s (pass --to)", *from)
		}
	}
	if *symbol != "" {
		return printMigration("", migrate.Map(ctx, e, migrate.Query{Symbol: *symbol, Language: *lang, From: *from, To: *to, Members: true}), *asJSON)
	}
	var files, mapped, unresolved int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "build" || d.Name() == ".git" || d.Name() == "bin" || d.Name() == "node_modules" || d.Name() == "venv") {
			return filepath.SkipDir
		}
		l := project.SourceLanguage(path)
		if d.IsDir() || l == "" {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // G122: CLI walks the operator's own project directory
		if err != nil {
			return err
		}
		res := migrate.Map(ctx, e, migrate.Query{Code: string(b), Language: l, From: *from, To: *to})
		var changes []migrate.Mapping
		for _, m := range res.Mappings {
			if m.Kind != migrate.SrcUnchanged || m.Notes != "" {
				changes = append(changes, m)
			}
		}
		if len(changes) == 0 && len(res.Unresolved) == 0 {
			return nil
		}
		files++
		mapped += len(changes)
		unresolved += len(res.Unresolved)
		res.Mappings = changes
		rel, _ := filepath.Rel(root, path)
		return printMigration(rel, res, *asJSON)
	})
	if err != nil {
		return err
	}
	if !*asJSON {
		fmt.Printf("SUMMARY %s %s→%s files_to_change=%d changes=%d unresolved=%d\n", root, *from, *to, files, mapped, unresolved)
	}
	return nil
}

func printMigration(file string, r migrate.Result, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			File string `json:"file,omitempty"`
			migrate.Result
		}{file, r})
	}
	prefix := func(line int) string {
		switch {
		case file != "" && line > 0:
			return fmt.Sprintf("%s:%d: ", file, line)
		case file != "":
			return file + ": "
		}
		return ""
	}
	for _, m := range r.Mappings {
		to := m.To
		if to == "" {
			to = "(removed)"
		}
		fmt.Printf("%s%s → %s [%s, %s, %s]", prefix(m.Line), m.From, to, m.Kind, m.Source, m.Confidence)
		if m.Notes != "" {
			fmt.Printf(" — %s", m.Notes)
		}
		if m.Citation != "" {
			fmt.Printf(" (%s)", m.Citation)
		}
		fmt.Println()
	}
	for _, u := range r.Unresolved {
		fmt.Printf("%sUNRESOLVED %s — %s\n", prefix(u.Line), u.Symbol, u.Reason)
	}
	if file == "" {
		if len(r.NotFound) > 0 {
			fmt.Printf("not a %s symbol: %s\n", r.From, strings.Join(r.NotFound, ", "))
		}
		if len(r.AlreadyIn) > 0 {
			fmt.Printf("already %s: %s\n", r.To, strings.Join(r.AlreadyIn, ", "))
		}
	}
	return nil
}
