package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/agenteval"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// env is what every subcommand needs: the index (for vendordep JSON and the
// verifier), the jar cache and javac.
type env struct {
	engine *retrieve.Engine
	shards []*index.Reader
	fetch  *agenteval.Fetcher
	javac  string
}

func openEnv(ctx context.Context, indexDir, cache string) (*env, error) {
	shards, errs := index.OpenDir(ctx, indexDir)
	if len(shards) == 0 {
		return nil, fmt.Errorf("no shards in %s: %v", indexDir, errs)
	}
	// Jar paths go to javac, which runs in the task workspace.
	if abs, err := filepath.Abs(cache); err == nil {
		cache = abs
	}
	javac, err := exec.LookPath("javac")
	if err != nil {
		return nil, errors.New("javac not found on PATH (JDK 25+ compiles both 2026 and 2027 code)")
	}
	return &env{engine: retrieve.New(shards, retrieve.Options{}), shards: shards,
		fetch: &agenteval.Fetcher{Dir: cache}, javac: javac}, nil
}

func (e *env) close() {
	for _, s := range e.shards {
		s.Close()
	}
}

// vendordeps resolves catalog names to the newest catalog version of the
// season (e.g. "REVLib" → REVLib 2027.0.0-alpha-7).
func (e *env) vendordeps(season string, names []string) ([]agenteval.Vendordep, error) {
	var out []agenteval.Vendordep
	for _, n := range names {
		m, cands := e.engine.Catalog().Resolve(n, season)
		if m == nil {
			return nil, fmt.Errorf("vendordep %q has no unique %s entry in the catalog (candidates: %v)", n, season, cands)
		}
		out = append(out, agenteval.Vendordep{Name: m.Latest.Name, FileName: m.Latest.FileName, Raw: m.Latest.Raw})
	}
	return out, nil
}

// classpath returns the season jars plus the vendordeps' Java jars, and the
// annotation processor path.
func (e *env) classpath(ctx context.Context, s agenteval.Season, deps []agenteval.Vendordep) (cp, proc []string, err error) {
	arts := append([]agenteval.Artifact{}, s.Artifacts...)
	for _, d := range deps {
		va, err := agenteval.VendordepArtifacts(d.Raw)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range va {
			for _, r := range append([]string{a.Repo}, a.Fallbacks...) {
				if host := hostOf(r); host != "" {
					e.fetch.AllowHost(host)
				}
			}
		}
		arts = append(arts, va...)
	}
	if cp, err = e.fetch.Classpath(ctx, arts); err != nil {
		return nil, nil, err
	}
	proc, err = e.fetch.Classpath(ctx, s.Processors)
	return cp, proc, err
}

func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	h, _, _ := strings.Cut(u, "/")
	return h
}

func defaultCache() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "frc-mcp", "agenteval", "maven")
}
