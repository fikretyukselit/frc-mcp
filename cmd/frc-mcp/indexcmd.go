package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func indexCmd(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return errors.New(`usage: frc-mcp index build --chunks F [--symbols F] --out SHARD.sqlite [--name N]`)
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
