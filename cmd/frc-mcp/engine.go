package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/fikretyukselit/frc-mcp/internal/dist"
	"github.com/fikretyukselit/frc-mcp/internal/embed/m2v"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// openEngine loads every shard in dir and, when present, the lite embedding
// model from dir/models/. Missing pieces degrade (logged), never fail.
func openEngine(ctx context.Context, log *slog.Logger, dir, season string, dense bool) (*retrieve.Engine, []*index.Reader) {
	return openEngineWith(ctx, log, dir, season, dense, true)
}

// openEngineWith is openEngine that can leave out shards holding content
// without a redistribution license (LicenseRef-*): a server others connect
// to redistributes what it serves (docs/sources.md §0.2).
func openEngineWith(ctx context.Context, log *slog.Logger, dir, season string, dense, unlicensed bool) (*retrieve.Engine, []*index.Reader) {
	shards, errs := index.OpenDir(ctx, dir)
	for _, err := range errs {
		log.Warn("shard skipped", "err", err)
	}
	if !unlicensed {
		var kept []*index.Reader
		for _, s := range shards {
			if lic := unlicensedLicenses(ctx, s); len(lic) > 0 {
				log.Info("shard not served: content without a redistribution license (pass --include-unlicensed once permission exists)",
					"shard", s.Meta().Name, "licenses", lic)
				s.Close()
				continue
			}
			kept = append(kept, s)
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

// unlicensedLicenses lists a shard's LicenseRef-* licenses. A shard whose
// licenses cannot be read is treated as unlicensed (fail closed).
func unlicensedLicenses(ctx context.Context, s *index.Reader) []string {
	lics, err := s.Licenses(ctx)
	if err != nil {
		return []string{"unreadable: " + err.Error()}
	}
	var out []string
	for _, l := range lics {
		if dist.Unlicensed(l) {
			out = append(out, l)
		}
	}
	return out
}
