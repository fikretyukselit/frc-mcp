package main

import (
	"context"
	"log/slog"
	"os"
	"slices"

	"github.com/fikretyukselit/frc-mcp/internal/dist"
	"github.com/fikretyukselit/frc-mcp/internal/embed/m2v"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// openEngine loads every shard in dir and, when present, the lite embedding
// model from dir/models/. Missing pieces degrade (logged), never fail.
func openEngine(ctx context.Context, log *slog.Logger, dir string, dense bool) (*retrieve.Engine, []*index.Reader) {
	return openEngineWith(ctx, log, dir, "", dense, false, true)
}

// openEngineWith is openEngine for a server others connect to (shared):
// it redistributes what it serves (docs/sources.md §0.2), so it never loads
// prohibited content (user content, a license that forbids distribution)
// and loads content without a redistribution license only with unlicensed.
func openEngineWith(ctx context.Context, log *slog.Logger, dir, season string, dense, shared, unlicensed bool) (*retrieve.Engine, []*index.Reader) {
	shards, errs := index.OpenDir(ctx, dir)
	for _, err := range errs {
		log.Warn("shard skipped", "err", err)
	}
	if shared {
		var kept []*index.Reader
		for _, s := range shards {
			lics, err := s.Licenses(ctx)
			if err != nil {
				log.Warn("shard not served: licenses unreadable", "shard", s.Meta().Name, "err", err)
				s.Close()
				continue
			}
			no := slices.DeleteFunc(slices.Clone(lics), func(l string) bool { return !sources.Prohibited(l) })
			unl := slices.DeleteFunc(slices.Clone(lics), func(l string) bool { return !dist.Unlicensed(l) })
			switch {
			case len(no) > 0:
				log.Info("shard not served: content that is never redistributed", "shard", s.Meta().Name, "licenses", no)
			case len(unl) > 0 && !unlicensed:
				log.Info("shard not served: content without a redistribution license (pass --include-unlicensed to serve it)",
					"shard", s.Meta().Name, "licenses", unl)
			default:
				kept = append(kept, s)
				continue
			}
			s.Close()
		}
		shards = kept
	}
	if len(shards) == 0 {
		return nil, nil
	}
	opt := retrieve.Options{DefaultSeason: season, DisableDense: !dense}
	if m, err := dist.ReadLocal(dir); err == nil && m != nil {
		opt.Expires = m.ExpiresAt
	}
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
