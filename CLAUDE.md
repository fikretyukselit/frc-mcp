# CLAUDE.md — frc-mcp

This file is the operating manual for any coding agent working in this repository. Read it fully before
changing code. Design rationale lives in `docs/`; this file states **what is true and what must stay true**.

---

## 1. Mission

`frc-mcp` is a Model Context Protocol server that grounds LLM coding agents in **current, version-correct**
FIRST Robotics Competition (FRC) software knowledge: WPILib (docs + Java/C++/Python API), vendor libraries
(CTRE Phoenix 6, REVLib, ReduxLib, Studica, PhotonVision, Limelight, PathPlanner, Choreo, AdvantageKit, YAGSL,
maple-sim, …), vendordep metadata, release notes, and the season rulebook.

The single failure mode we exist to eliminate: **an agent writing robot code against the wrong season's API**
(Phoenix 5 vs 6, `CANSparkMax` vs `SparkMax`, 2026 `edu.wpi.first` vs 2027 `org.wpilib`, `frc::` vs `wpi::`).
Every design decision is judged against that.

Differentiators versus existing FRC MCPs (DugboTek/FRCDocsMCP, ramalamadingdong/frc-rag-mcpserver,
rylero/chiefdelphi-mcp, TripleHelixProgramming/wpilog-mcp — see `docs/research/`):

1. **Version pinning is a first-class dimension**, auto-detected from the user's `build.gradle` + `vendordeps/*.json`.
2. **Symbol-level API truth** (Javadoc/Doxygen/Python stubs per version) powering `frc_verify_code` and `frc_migrate`.
3. **Continuously fresh**: an adaptive, season-aware ingestion plane ships signed index shards; nothing is frozen into an npm tarball.
4. **Provenance on every byte returned**: source URL, library, version, commit/ETag, retrieved-at.
5. **Local-first, single static binary**, sub-50 ms p95 search, no Python, no GPU, no network required at query time.

---

## 2. System shape (two planes)

```
                ┌──────────────────────── Ingestion plane (CI / scheduled) ─────────────────────────┐
 sources.yaml → │ scheduler (Gamma–Poisson + season prior) → fetcher (conditional GET, per-host rate)│
                │ → normalize → structure-aware chunk → context prefix → embed (potion) → symbol tbl │
                │ → eval gate → shard writer (SQLite) → zstd → OCI push (ghcr) → cosign sign         │
                └────────────────────────────────────────┬──────────────────────────────────────────┘
                                                         │ signed, content-addressed shards
                ┌──────────────────────── Serving plane (user machine) ─────────▼───────────────────┐
 agent ⇄ stdio →│ MCP server (go-sdk) → router (deterministic decision model) → retriever           │
 (or HTTP)      │ (FTS5 BM25 ∥ int8 flat vector ∥ exact symbol) → RRF → boosts → abstention → render │
                │ + optional live freshness probe (release feeds / vendordep jsonUrl, ETag-cached)   │
                └───────────────────────────────────────────────────────────────────────────────────┘
```

- One binary, subcommands: `frc-mcp serve` (default; `--transport stdio|http`), `frc-mcp sync` (pull/verify shards),
  `frc-mcp index` (ingestion plane; used by CI, runnable locally), `frc-mcp eval`, `frc-mcp doctor`.
- The **serving plane never crawls**. Its only network calls are shard sync and optional ETag-cached freshness probes.
  It must be fully functional offline against the last synced shards.

Full detail: `docs/architecture.md`, `docs/retrieval.md`, `docs/ingestion.md`, `docs/mcp-surface.md`, `docs/sources.md`.

---

## 3. Tech stack (pinned decisions — change only via an ADR in `docs/adr/`)

| Concern | Choice | Why |
|---|---|---|
| Language | **Go 1.27** (`go 1.27` in go.mod) | Tier-1 MCP SDK, static binaries, json/v2 GA, Green Tea GC default |
| MCP | `github.com/modelcontextprotocol/go-sdk` **v1.8.x**, spec **2026-07-28** | Official; stateless core, cacheable lists, MRTR |
| CGO | **`CGO_ENABLED=0` always** for the shipped binary | `go install` + goreleaser cross-compile must just work |
| Storage + FTS | `modernc.org/sqlite` (FTS5, read-only/immutable at serve time) | Pure Go; one file holds text, metadata, vectors, symbols |
| Dense vectors | int8-quantized, flat (exact) scan over mmapped blob | Corpus is ~50–150k chunks; ANN is unnecessary complexity |
| Query embedding | model2vec **potion-code-16M-v2** (MIT), in-repo pure-Go inference (`internal/embed/m2v`) | <1 ms/query, no ONNX runtime |
| Fusion | Reciprocal Rank Fusion (k=60) + deterministic boosts | Robust, tuning-free baseline that beats either retriever alone |
| Rerank (optional) | Ettin-17M cross-encoder via `hugot` behind build tag `rerank` | Off by default; breaks p95 budget on CPU |
| Feeds | `github.com/mmcdole/gofeed` | Mature RSS/Atom |
| HTML→MD | `github.com/JohannesKaufmann/html-to-markdown/v2` | Sphinx/Javadoc/GitBook pages |
| GitHub | `google/go-github` (pinned major, wrapped behind `internal/ingest/gh`) + hand-rolled GraphQL batch | Majors churn monthly |
| Rate limit / concurrency | `golang.org/x/time/rate`, `golang.org/x/sync/errgroup` | stdlib-adjacent |
| PDF | `klippa-app/go-pdfium` (WebAssembly mode), fallback `ledongthuc/pdf` | Game Manual; no cgo |
| Distribution | goreleaser v2 (`homebrew_casks`, scoop, winget), cosign keyless, SLSA attestations, Syft SBOM; index via `oras-go/v2` on GHCR | Supply-chain hygiene for a student-facing tool |
| Testing | stdlib `testing`, `testing/synctest`, `go-cmp`, golden files, `gopkg.in/dnaeon/go-vcr.v4`, native fuzzing, `b.Loop()` | |

**Forbidden without an ADR:** cgo in the default build, a Python sidecar, a hosted vector DB, LLM calls on the
query path, MCP Sampling/Logging/Roots (deprecated in 2026-07-28), mutable global state.

---

## 4. Repository layout (target)

```
cmd/frc-mcp/            main; subcommand wiring only (no logic)
internal/
  mcpserver/            tool/resource/prompt registration, request → service mapping, rendering
  router/               deterministic query decision model (intent, library, version, language)
  retrieve/             fts.go, vector.go, symbol.go, fuse.go (RRF), boost.go, abstain.go
  embed/m2v/            model2vec tokenizer + table lookup + mean-pool + normalize (pure Go)
  index/                shard schema, reader (read-only, mmap), writer, manifest, schema versioning
  apisym/               API symbol tables per (library, version, language); verify engine
  migrate/              curated migration rules loader + matcher
  project/              build.gradle / vendordeps detection → Pin set
  ingest/
    sched/              Gamma–Poisson/Thompson scheduler, season calendar
    fetch/              conditional GET client, validator store, per-host limiter, robots
    gh/                 GitHub REST+GraphQL wrapper
    source/             one package per adapter: rtd, gitbook, javadoc, doxygen, vendordep, maven, pypi, rss, atom, pdf
    normalize/          HTML/RST → canonical Markdown, normalized hash
    chunk/              structure-aware chunkers + context prefix builder
  dist/                 OCI pull, digest/cosign verification, atomic shard swap
  obs/                  slog + OpenTelemetry hooks, pprof endpoint (http mode only)
data/
  sources.yaml          source registry (declarative; the ONLY place endpoints live)
  season.yaml           FRC calendar multipliers for the scheduler
  migrations/*.yaml     curated API migration rules with citations
eval/
  qrels/                retrieval judgments (TREC format), split train/holdout
  tasks/                agent-level coding tasks + rubrics
  baseline.json         committed metric baseline used by the CI gate
docs/                   architecture, ADRs, research (Turkish originals in docs/research/)
```

---

## 5. Invariants (never break these)

1. **Every chunk carries** `library`, `version_range`, `season`, `channel (stable|beta|alpha)`, `language (java|cpp|python|any)`,
   `source_url`, `anchor`, `upstream_rev` (commit SHA / ETag / version), `retrieved_at`, `license`.
   A chunk without these fields must fail the writer, not be silently indexed.
2. **Never mix seasons silently.** If the pinned season has no hit, return `status: "version_mismatch"` with the
   nearest-season hits labeled as such. Do not blend them into the main result list.
3. **Abstain rather than pad.** Below the calibrated confidence floor, return `status: "no_match" | "low_confidence"`
   plus suggested next calls. Never fill `k` with weak hits.
4. **Provenance is mandatory in output.** Every returned snippet includes its citation fields in `structuredContent`.
5. **Serving is read-only.** Shards are opened `mode=ro&immutable=1`. Shard updates are atomic directory swaps.
6. **No secrets in shards, logs, or tool output.** FRC Events / TBA / GitHub tokens are env-only and are redacted in go-vcr cassettes.
7. **Deterministic tool listing order** and stable tool names (prompt-cache friendliness; spec SHOULD).
8. **Endpoints live in `data/sources.yaml` only.** Adapters are generic; no hard-coded URLs in Go code except tests.
9. **Respect upstream policy:** robots.txt, `Cache-Control: max-age`, `Retry-After`, feed `<ttl>`, RTD < 4 req/s, identifying
   `User-Agent: frc-mcp/<ver> (+https://github.com/fikretyukselit/frc-mcp)`. Chief Delphi via RSS only (JSON is Cloudflare-blocked).
10. **Unverified endpoints are flagged** (`verified: false` in sources.yaml) until a probe run confirms them; the probe
    result is recorded, not assumed.

---

## 6. Performance budgets (enforced by benchmarks in CI)

| Metric | Budget | Measured by |
|---|---|---|
| `frc_search` p50 / p95 (warm, 100k chunks, laptop-class CPU) | ≤ 8 ms / ≤ 50 ms | `BenchmarkSearch*` + eval harness |
| `frc_api_lookup` p95 | ≤ 2 ms | `BenchmarkSymbol*` |
| Query embedding | ≤ 1 ms | `BenchmarkEmbed` |
| Cold start to first `tools/list` | ≤ 150 ms | `frc-mcp doctor --bench` |
| RSS at steady state | ≤ 150 MB | eval harness |
| Allocations per search | ≤ 200 | `-benchmem` |
| Binary size (linux/amd64, stripped) | ≤ 40 MB (index excluded) | goreleaser job |
| Tool response size (default `concise`) | ≤ 2,500 tokens; hard cap 20k | render tests |

Performance practices expected in hot paths: prepared statements cached per connection; read-only connection pool sized
to `GOMAXPROCS`; `PRAGMA mmap_size` on shards; int8 dot products in tight, bounds-check-eliminated loops (verify with
`-gcflags=-d=ssa/check_bce`); `sync.Pool` for scratch buffers and top-k heaps; `encoding/json/v2` for rendering; no
reflection or regexp compilation per request; PGO via committed `default.pgo` regenerated from the eval workload.
Any PR touching `internal/retrieve`, `internal/embed`, or `internal/index` must include `benchstat` output.

---

## 7. MCP surface (summary — contract in `docs/mcp-surface.md`)

Tools (all `readOnlyHint: true`, namespaced `frc_`, ≤ 8 total, deterministic order):

| Tool | Purpose |
|---|---|
| `frc_context` | Detect/declare the project pin set (WPILib year, vendordeps + versions, language). Returns an opaque `pin` handle other tools accept. |
| `frc_search` | Hybrid search over docs/API/release notes/rules, filtered by pin; `response_format: concise|detailed`, `max_tokens`, cursor. |
| `frc_read` | Fetch a full page or specific sections by stable ID (search → read progressive disclosure). |
| `frc_api` | Exact symbol lookup: signature, docs, since/deprecated-in, per language, per version. |
| `frc_verify_code` | Static check of a snippet/file against the pinned API symbol table: unknown, deprecated, wrong-season symbols, with fixes + citations. |
| `frc_migrate` | Map a symbol/pattern between versions (Phoenix 5→6, REVLib 2024→2025+, WPILib 2026→2027) from curated rules. |
| `frc_vendordep` | Resolve a vendordep: latest JSON for season, `frcYear` validation, conflicts, install URL. |
| `frc_whats_new` | Release/changelog/known-issues delta for a library since a version or date, with live freshness probe. |

Resources (templates): `frc://docs/{library}/{version}/{path}`, `frc://api/{library}/{version}/{lang}/{symbol}`,
`frc://vendordep/{name}/{season}`, `frc://changelog/{library}/{version}`, `frc://index/manifest`.
Prompts (workflows): `frc-new-subsystem`, `frc-port-season`, `frc-swerve-setup`, `frc-auto-routine`, `frc-debug-vendordep`.

Every list/read result sets `ttlMs` + `cacheScope` per SEP-2549. Errors are instructive (tell the model the exact next call).

---

## 8. Working agreements for agents

- **Before coding:** read the relevant `docs/*.md`. If you change a contract (tool schema, chunk schema, shard schema,
  sources.yaml schema), update the doc and bump the relevant schema version in the same PR.
- **Build / test commands** (once scaffolded):
  - `make build` · `make test` (`go test ./... -race -shuffle=on`) · `make bench` · `make lint` (`golangci-lint run`, `go vet`)
  - `make eval` (retrieval metrics vs `eval/baseline.json`; fails on regression) · `make probe` (verify sources.yaml endpoints)
  - `go run ./cmd/frc-mcp serve --transport stdio --index ./.shards` for local MCP testing (`npx @modelcontextprotocol/inspector`).
- **Tests:** table-driven; golden files for normalizers/chunkers/renderers (`-update` flag); go-vcr cassettes for every
  adapter (strip `Authorization`); `testing/synctest` for scheduler/backoff; fuzz normalizers (idempotence:
  `normalize(normalize(x)) == normalize(x)`), version comparators, and the vendordep parser.
- **Errors:** wrap with `%w`, sentinel errors per package, no panics outside `main`. Context propagation everywhere; every
  outbound call has a deadline.
- **Logging:** `log/slog` to **stderr only** (stdout is the stdio transport — writing to it corrupts the protocol).
- **Dependencies:** justify any new module in the PR description; prefer stdlib. No module with cgo in the default build graph.
- **Commits:** Conventional Commits (`feat(retrieve): …`). Small PRs. Never commit tokens, cassettes with secrets, or shards.
- **Language:** code, comments, docs, and commit messages in English. Research originals in `docs/research/` stay Turkish.
- **When unsure about an upstream fact** (URL, API name, version), verify against the live source or mark it `verified: false` — never invent it.

---

## 9. Roadmap (milestones)

- **M0 — Skeleton:** go.mod, `serve` over stdio with `frc_search` on a hand-built fixture shard; CI (lint, test, race); goreleaser snapshot.
- **M1 — WPILib vertical slice:** frc-docs (stable + latest) + allwpilib Javadoc/Doxygen for 2026 and 2027-alpha; FTS5 + m2v + RRF; `frc_read`, `frc_api`, `frc_context`; eval set v0 (≥150 qrels).
- **M2 — Vendors + vendordeps:** vendor-json-repo, CTRE, REV, PhotonVision, PathPlanner, Choreo, AdvantageKit, YAGSL; `frc_vendordep`, `frc_whats_new`.
- **M3 — Ingestion plane in CI:** scheduler, conditional fetch, shard publishing to GHCR with cosign; `frc-mcp sync`.
- **M4 — Correctness tools:** `frc_verify_code`, `frc_migrate` with curated migration tables; agent-level eval.
- **M5 — Hardening + launch:** Streamable HTTP (stateless), public registry entry, Homebrew/Scoop/Winget, docs site, make repo public.
