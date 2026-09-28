package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/fikretyukselit/frc-mcp/internal/embed/m2v"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// openEngine loads every shard in dir and, when present, the lite embedding
// model from dir/models/. Missing pieces degrade (logged), never fail.
func openEngine(ctx context.Context, log *slog.Logger, dir, season string, dense bool) (*retrieve.Engine, []*index.Reader) {
	shards, errs := index.OpenDir(ctx, dir)
	for _, err := range errs {
		log.Warn("shard skipped", "err", err)
	}
	if len(shards) == 0 {
		return nil, nil
	}
	opt := retrieve.Options{DefaultSeason: season, DisableDense: !dense}
	if dense {
		md := index.ModelDir(dir, index.DefaultEmbedModel)
		if _, err := os.Stat(md); err == nil {
			m, err := m2v.Load(md, index.DefaultEmbedModel)
			if err != nil {
				log.Warn("embedding model unusable; dense retrieval off", "err", err)
			} else {
				opt.Model = m
			}
		}
	}
	e := retrieve.New(shards, opt)
	for _, w := range e.Warnings() {
		log.Warn("degraded", "what", w)
	}
	return e, shards
}
