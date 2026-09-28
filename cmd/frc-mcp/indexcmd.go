package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/build"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// indexRun is the ingestion plane: fetch every registered source, parse,
// diff, embed and write shards.
func indexRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index run", flag.ContinueOnError)
	srcPath := fs.String("sources", "data/sources.yaml", "source registry")
	out := fs.String("out", index.DefaultDir(), "shard output directory")
	cache := fs.String("cache", ".cache/fetch", "fetch cache directory (conditional GET state)")
	only := fs.String("only", "", "comma-separated source ids (default: all)")
	noEmbed := fs.Bool("no-embed", false, "skip vector layers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := sources.Load(*srcPath)
	if err != nil {
		return err
	}
	var ids []string
	if *only != "" {
		ids = strings.Split(*only, ",")
	}
	rep, err := build.Run(ctx, build.Options{Registry: reg, OutDir: *out, CacheDir: *cache, Version: buildVersion(),
		SourceIDs: ids, Embed: !*noEmbed, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func indexCmd(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return indexRun(ctx, args[1:])
		case "publish":
			return indexPublish(ctx, args[1:])
		case "keygen":
			return indexKeygen()
		}
	}
	if len(args) == 0 || args[0] != "build" {
		return errors.New("usage:\n  frc-mcp index run   [--sources data/sources.yaml] [--out DIR] [--only id,…] [--no-embed]\n  frc-mcp index build --chunks F [--symbols F] --out SHARD.sqlite [--name N]")
	}
	fs := flag.NewFlagSet("index build", flag.ContinueOnError)
	chunks := fs.String("chunks", "", "JSONL file of chunks")
	symbols := fs.String("symbols", "", "JSONL file of API symbols")
	out := fs.String("out", "", "output shard path (*.sqlite)")
	name := fs.String("name", "", "shard name (default: file name)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *out == "" || (*chunks == "" && *symbols == "") {
		return errors.New("--out and at least one of --chunks / --symbols are required")
	}
	if *name == "" {
		*name = strings.TrimSuffix(filepath.Base(*out), filepath.Ext(*out))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	meta, err := index.BuildFromJSONL(ctx, *out, *name, *chunks, *symbols)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(meta)
}
