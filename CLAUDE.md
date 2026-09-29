# CLAUDE.md — frc-mcp

This is the operating manual for any coding agent working in this repository. Read all of it before you change code.
Design rationale lives in `docs/`. This file states **what is true and what must stay true**.

- Plan version: **v0.2** (2026-09-29). Implementation: **M0, M1 and M2 complete**; **M3 in progress** (vendor docs done). M2 publishing is waiting on the production signing key and CI billing; see ADR-0006. Measured numbers are in
  `docs/benchmarks.md`; eval results are in `docs/retrieval.md` §8.
- Change log for this version: `docs/reviews/2026-09-29-research-03-review.md`.

---

## 1. Mission

`frc-mcp` is a Model Context Protocol server. It gives LLM coding agents FIRST Robotics Competition (FRC) software
knowledge that is **current and correct for the version in use**:

- WPILib docs, plus the Java, C++ and Python APIs.
- Vendor libraries: CTRE Phoenix 6, REVLib, ReduxLib, Studica, PhotonVision, Limelight, PathPlanner, Choreo,
  AdvantageKit, YAGSL, maple-sim, and others.
- Vendordep metadata and compatibility.
- Release notes.
- Hardware specs.

The one failure we exist to eliminate is **an agent writing robot code against the wrong season's API**. Typical
mix-ups: Phoenix 5 vs 6, `CANSparkMax` vs `SparkMax`, 2026 `edu.wpi.first` vs 2027 `org.wpilib`, `frc::` vs `wpi::`.
Judge every design decision against that failure.

What sets us apart from existing FRC MCPs (DugboTek/FRCDocsMCP, ramalamadingdong/frc-rag-mcpserver,
rylero/chiefdelphi-mcp, TripleHelixProgramming/wpilog-mcp; see `docs/research/`):

1. **Version pinning is a core dimension.** It is detected automatically from `build.gradle`, `vendordeps/*.json` and
   `pyproject.toml`.
2. **API facts at the symbol level.** Symbol tables are built per version from Javadoc/class files, Doxygen XML and
   `.pyi` stubs. They power `frc_verify_code` and `frc_migrate`.
3. **Exact facts are looked up, not guessed.** Vendordeps, compatibility data, releases and hardware specs sit in
   relational tables.
4. **Always fresh.** An ingestion plane in CI ships signed index shards. Nothing is frozen into a package.
5. **Every returned item carries its provenance and a trust tier.** Community text is fenced off as data.
6. **Local-first.** One static binary, sub-50 ms p95 search, no Python, no GPU, and no network needed at query time.
   A separate hosted profile exists for shared servers (ADR-0005).

---

## 2. System shape (two planes, two profiles)

```
                ┌──────────────────────── Ingestion plane (CI; build ≠ sign jobs) ───────────────────────┐
 sources.yaml → │ fixed cron (M2) → adaptive Gamma–Poisson scheduler (M5) → fetcher (conditional GET,     │
                │ egress allowlist, per-host rate) → sanitize → normalize → chunk + prefix → trust tier →  │
                │ embed (per-model vector layers) → symbol + fact tables → eval gate → shards → OCI + HTTPS │
                │ mirror → cosign (pinned identity), manifest serial + expiry                              │
                └────────────────────────────────────────────┬──────────────────────────────────────────┘
                                                             │ signed, content-addressed shards (identical across profiles)
                ┌──────────────────────── Serving plane ─────▼──────────────────────────────────────────┐
 agent ⇄ stdio →│ MCP server (go-sdk) → router (deterministic) → retrieval: FTS5 BM25 ∥ int8 flat vector ∥ │
 (or HTTP)      │ exact symbol ∥ fact tables → RRF → boosts → conditional rerank → abstention → renderer    │
                │ (content ≡ structuredContent, fencing, budgets) + optional allowlisted freshness probe   │
                └───────────────────────────────────────────────────────────────────────────────────────┘
   local-lite (default): stdio, offline, pure Go          hosted-full: stateless HTTP, open-model sidecar for
                                                          dense + rerank, auth, no filesystem arguments
```

- **One binary with subcommands:**
  - `frc-mcp serve` (the default; `--transport stdio|http`, `--profile lite|full`)
  - `frc-mcp sync`
  - `frc-mcp index` (the ingestion plane; runs in CI and can also run locally)
  - `frc-mcp eval`
  - `frc-mcp doctor`
- **The serving plane never crawls.** Its only network calls are shard sync and bounded, allowlisted freshness probes.
  It must work fully offline against the last synced shards.

For full detail, see:
- `docs/architecture.md`
- `docs/retrieval.md`
- `docs/ingestion.md`
- `docs/mcp-surface.md`
- `docs/security.md`
- `docs/sources.md`
- `docs/adr/`

---

## 3. Tech stack (pinned decisions; change them only through an ADR in `docs/adr/`)

| Concern | Choice | Why |
|---|---|---|
| Language | **Go 1.27** (`go 1.27` in go.mod) | Tier-1 MCP SDK, static binaries, json/v2 is GA, Green Tea GC is the default |
| MCP | `github.com/modelcontextprotocol/go-sdk` **≥ v1.7.0** (move to v1.8.x once GA is confirmed), spec **2026-07-28** | Official SDK; stateless core, cacheable lists, MRTR, `isError` validation results |
| CGO | **`CGO_ENABLED=0` always** for shipped binaries | `go install` and goreleaser cross-compiles must just work |
| Storage + FTS | `modernc.org/sqlite` (FTS5; read-only and immutable at serve time) | Pure Go; one file holds text, fact tables, symbols and vector offsets |
| Dense vectors | int8, flat (exact) scan over mmapped **per-model vector layers** | About 50–150k chunks, so ANN isn't needed; layers stay compatible across profiles |
| Query embedding (lite) | model2vec **potion-code-16M-v2** (MIT), pure-Go inference in the repo (`internal/embed/m2v`) | Under 1 ms per query and no ONNX runtime. It is kept only if eval shows ≥ 1 nDCG point over BM25 alone |
| Fusion | RRF (k=60) plus deterministic boosts | Robust, needs no tuning |
| Rerank | lite: Ettin-17M (to be verified) via `hugot`, behind the `rerank` build tag, **conditional** on a low abstention margin. full: an open cross-encoder in a sidecar, always on | This is the largest single retrieval gain, and making it conditional keeps it inside the p95 budget |
| Feeds / HTML | `mmcdole/gofeed`, `JohannesKaufmann/html-to-markdown/v2`, `PuerkitoBio/goquery` | Mature |
| GitHub | `google/go-github` (pinned major version, wrapped behind `internal/ingest/gh`), plus a hand-rolled GraphQL batch client | Major versions change monthly |
| Rate limiting / concurrency | `golang.org/x/time/rate`, `golang.org/x/sync/errgroup` | Close to the standard library |
| Distribution | Binaries: goreleaser v2 (`homebrew_casks`, scoop, winget), cosign keyless, SLSA, Syft SBOM, **Authenticode + notarization**. Index: **ed25519-signed manifest over HTTPS** (GitHub Releases or any mirror, including LAN mirrors), content-addressed gzip objects (ADR-0006; OCI deferred to the hosted profile) | Offline verification, zero extra dependencies, works on school networks |
| Testing | stdlib `testing`, `testing/synctest`, `go-cmp`, golden files, `gopkg.in/dnaeon/go-vcr.v4`, native fuzzing, `b.Loop()`, the MCP conformance suite | |

**Forbidden without an ADR:**
- cgo in the default build;
- any sidecar in local-lite;
- Postgres, commercial embedding or rerank APIs, or TOON (ADR-0004);
- LLM calls on the query path;
- MCP Sampling, Logging or Roots;
- tool descriptions derived from data;
- mutable global state.

---

## 4. Repository layout (target)

Packages that exist today (M3 in progress):
- `cmd/frc-mcp`
- `internal/{index,retrieve,router,render,vec,netguard,textutil,testfixture,mcpserver,project,apisym,eval,sources,facts,verify,dist}`
- `internal/embed/m2v`
- `internal/ingest/{build,fetch,sanitize,chunking,source/sphinx,source/javadoc,source/vendordeps,source/markdown,source/repomd,source/gitbook}`
- `data/sources.yaml`, `eval/`, `testdata/fixture`

Everything else below is planned; `CONTRIBUTING.md` §2 has the status table.

```
cmd/frc-mcp/            main; subcommand wiring only (no logic)
internal/
  mcpserver/            tool/resource/prompt registration, request → service mapping
    surface/            compiled-in tool & prompt descriptions (golden-tested; never data-driven)
    render/             single renderer: Out → structuredContent + parity Markdown, fencing, budgets, cursors
  router/               deterministic query decision model (intent, library, version, language)
  retrieve/             fts.go, vector.go, symbol.go, facts.go, fuse.go (RRF), boost.go, rerank.go, abstain.go
  embed/m2v/            model2vec tokenizer + table lookup + mean-pool + normalize (pure Go)
  index/                shard schema, reader (read-only, mmap), writer, manifest (serial/expiry), schema versioning
  facts/                vendordep, compat, release, hw_spec, rule tables (relational lookups)
  apisym/               per-(library, version, language) symbol tables; table diffing; verify engine
  migrate/              curated + generated migration rules
  project/              build.gradle / vendordeps / pyproject.toml detection → Pin set
  netguard/             egress allowlist dialer, redirect policy, private-IP blocking, body caps
  ingest/
    sched/              fixed-interval scheduler (M2) → Gamma–Poisson/Thompson (M5), season calendar
    fetch/              conditional GET client, validator store, per-host limiter, robots
    gh/                 GitHub REST+GraphQL wrapper
    source/             adapters: rtd, gitbook, javadoc, doxygen, pyi, vendordep, maven, pypi, rss, atom, pdf
    sanitize/           hidden-text stripping, suspect detection, trust tiering
    normalize/          HTML/RST → canonical Markdown, normalized hash
    chunk/              structure-aware chunkers + context prefix builder
  dist/                 OCI + HTTPS mirror pull, resumable download, cosign verification, atomic swap, retention
  obs/                  slog, local stats ring buffer, OTel hooks (hosted), pprof (http only)
data/
  sources.yaml          source registry (the ONLY place endpoints live; also derives the egress allowlist)
  season.yaml           FRC calendar multipliers for the scheduler
  router.yaml boost.yaml abstain.json   decision-model parameters (data, versioned)
  migrations/*.yaml     curated API migration rules with citations
eval/
  qrels/                retrieval judgments (TREC), train / holdout (holdout human-written)
  tasks/                agent-level coding tasks + rubrics (compile-checked)
  security/             prompt-injection and SSRF corpora
  baseline.json         committed metric baseline used by the CI gate
docs/                   architecture, security, ADRs, reviews, research (Turkish originals in docs/research/)
```

---

## 5. Invariants (never break these)

1. **Every chunk carries these fields:**
   - `library`, `version_range`, `season`, `channel (stable|beta|alpha)`, `language (java|cpp|python|any)`
   - `source_url`, `anchor`, `upstream_rev`, `retrieved_at`, `license`
   - **`trust (official|vendor|community)`**, `suspect`

   The shard writer must fail on a chunk that lacks any of them.
2. **Never mix seasons silently.** If the pinned season has no hit, return `status: "version_mismatch"`. Label the
   nearest-season hits as such and keep them out of the main result list.
3. **Abstain rather than pad.** When confidence falls below the calibrated floor, return `no_match` or `low_confidence`
   together with suggested next calls.
4. **Every output carries provenance.** Every snippet includes its citation in `structuredContent`, and the same
   citation appears in `content`.
5. **`content` must carry the same meaning as `structuredContent`.** A single renderer produces both from one typed
   value, and golden tests check them.
6. **Exact facts come from relational lookups, never from similarity retrieval.** This covers vendordeps, compatibility,
   releases, hardware specs, symbols and rules.
7. **Serving is read-only.** Shards are opened with `mode=ro&immutable=1`, and updates are atomic directory swaps.
8. **No secrets** may appear in shards, logs, tool output or cassettes.
9. **The tool surface is static.** Names, descriptions, schemas and prompts are compiled in and listed in a
   deterministic order. A golden test proves shards cannot change the output of `tools/list` or `prompts/list`.
10. **Endpoints live only in `data/sources.yaml`.** The egress allowlist is derived from it.
11. **Untrusted text is data.** Chunks with `trust: community` are:
    - excluded from default search;
    - sanitized at ingest;
    - fenced inside `content`;
    - down-ranked when they are `suspect`.
12. **Egress is allowlisted.** Only https to allowlisted hosts is permitted. Redirects are re-checked, and private,
    loopback and link-local IPs are blocked at dial time. URL arguments accept only `frc://` or allowlisted https.
13. **The filesystem is scoped.**
    - stdio mode: only project manifests, plus the named file under the project root (symlinks resolved, size capped).
    - HTTP mode: no filesystem arguments at all.
14. **Manifests are signed and carry a monotonic serial and an expiry.** Signatures are ed25519 and verified against
    compiled-in keys (ADR-0006). Rollback is rejected. Expiry surfaces as `index_stale`. Sync fails closed when no
    trusted key is configured.
15. **Respect upstream policy:**
    - robots.txt, `Cache-Control`, `Retry-After`, the feed `<ttl>` value;
    - Read the Docs: under 4 req/s;
    - an identifying User-Agent;
    - Chief Delphi through RSS only.
16. **Unverified endpoints are flagged** (`verified: false`) until a probe confirms them.
17. **No telemetry by default** (the users are minors). Local stats never store query text.

---

## 6. Performance budgets (enforced by benchmarks in CI)

| Metric | Budget | Measured by |
|---|---|---|
| `frc_search` p50 / p95 (warm cache, 100k chunks, laptop-class CPU, lite) | ≤ 8 ms / ≤ 50 ms | `BenchmarkSearch*` + eval harness |
| `frc_search` p95 including conditional rerank (lite) | ≤ 80 ms on the rerank arm; ≤ 50 ms overall | eval harness |
| `frc_api` p95 and fact lookups | ≤ 2 ms | `BenchmarkSymbol*`, `BenchmarkFacts*` |
| Query embedding | ≤ 1 ms | `BenchmarkEmbed` |
| Cold start to first `tools/list` (with or without shards) | ≤ 150 ms | `frc-mcp doctor --bench` |
| RSS at steady state (lite) | ≤ 150 MB | eval harness |
| Allocations per search | ≤ 700 (≈75% are inside the SQLite driver: per row / per text column; ours ≤ 150) | `-benchmem` + alloc profile |
| Binary size (linux/amd64, stripped; index not included) | ≤ 40 MB | goreleaser job |
| Default shard set on disk (current season, one language) | ≤ 300 MB; retention keeps the current season plus the previous one | `doctor` |
| Tool response size (`concise`) | ≤ 2,500 tokens by default; hard cap 20k | render tests |

**Hot-path practices:**
- Cache prepared statements per connection.
- Size the read-only pool to `GOMAXPROCS`.
- Set `PRAGMA mmap_size`.
- Write int8 dot products that eliminate bounds checks, and verify with `-gcflags=-d=ssa/check_bce`.
- Use `sync.Pool` for scratch buffers and heaps.
- Render with `encoding/json/v2`.
- Do no per-request regexp compilation and no reflection in hot paths.
- Build with PGO from a committed `default.pgo`.

A PR that touches `internal/retrieve`, `internal/embed` or `internal/index` must include `benchstat` output.

---

## 7. MCP surface (summary; the contract is `docs/mcp-surface.md`)

There are 9 tools. All of them:
- set `readOnlyHint: true`;
- are namespaced `frc_`;
- are listed in a deterministic order;
- follow the description standard: purpose, use when, don't use when, examples.

| Tool | Purpose |
|---|---|
| `frc_context` | Detects or declares the pin set (Gradle, vendordeps, RobotPy). Returns compatibility warnings, upgrade hints, and an opaque `pin` handle. |
| `frc_search` | Hybrid, pin-filtered search. Forum content is off by default. Every hit carries `trust`. Output is budgeted and paged by cursor. |
| `frc_fetch` | Returns a full page or specific sections by ID or `frc://` URI, for progressive disclosure. |
| `frc_api` | Exact symbol lookup: signatures, since, deprecated and removed versions, and which other seasons contain the symbol. |
| `frc_verify_code` | Static check against the pinned symbol tables. `error` is raised only for `wrong_season` or removed symbols. Reports `coverage`. |
| `frc_migrate` | Maps symbols across versions from curated plus diff-generated rules. Produces mappings only, no rewriting. |
| `frc_vendordep` | Resolves one vendordep, or validates a set against a WPILib version (compatibility matrix plus fixes). |
| `frc_whats_new` | Release, changelog and known-issue changes since a given version or date, with an optional live probe. |
| `frc_hardware` | Motor, controller and sensor specs, labeled per source and never merged. Implemented in M3. |

- **Resources:**
  - `frc://docs/…`, `frc://api/…`, `frc://vendordep/…`
  - `frc://changelog/…`, `frc://specs/{part}`, `frc://index/manifest`
- **Prompts:**
  - `frc-new-subsystem`, `frc-port-season`, `frc-swerve-setup`
  - `frc-auto-routine`, `frc-debug-vendordep`
- **HTTP only, opt in:** `--toolset openai` exposes the literal tools `search` and `fetch`.
- **Envelope fields:** `status`, `confidence`, `pin_source`, `freshness`, `index_age`, `index_stale`, `truncated`,
  `omitted`, `next_cursor`, `degraded`, `next`.
- Errors are returned as instructive `isError` results that list valid values and the next call to make.

---

## 8. Working agreements for agents

- **Before coding:** read the relevant `docs/*.md`. If you change a contract (tool schema, chunk/shard schema,
  `sources.yaml` schema, or the render contract), update the doc and bump the schema version in the same PR.
- **Build and test commands (once scaffolded):**
  - `make build`
  - `make test` (`go test ./... -race -shuffle=on`)
  - `make bench`
  - `make lint` (`golangci-lint run`, `go vet`, `govulncheck`)
  - `make eval` (retrieval and safety metrics against `eval/baseline.json`; fails on regression)
  - `make probe` (checks the endpoints in `sources.yaml`)
  - `make conformance` (the MCP conformance suite)
  - `go run ./cmd/frc-mcp serve --transport stdio --index ./.shards`, then connect with a **pinned, patched** MCP
    Inspector (CVE-2025-49596).
- **Tests:**
  - Write them table-driven.
  - Use golden files for normalizers, chunkers, the renderer and the tool surface (update with the `-update` flag).
  - Record go-vcr cassettes for every adapter, with `Authorization` stripped.
  - Use `testing/synctest` for the scheduler and backoff.
  - Fuzz normalizer idempotence, the sanitizer, the version comparators, the vendordep parser, and the
    safetensors/tokenizer parsers.
  - Run the injection and SSRF corpora in `eval/security/`.
- **Errors:** wrap with `%w`, use sentinel errors per package, and never panic outside `main`. Give every outbound call
  a deadline.
- **Logging:** use `log/slog` to **stderr only**. Stdout is the stdio transport.
- **Dependencies:** justify every new module in the PR. Prefer the standard library. Nothing with cgo may enter the
  default build graph.
- **Commits:** follow Conventional Commits. Keep PRs small. Never commit tokens, cassettes containing secrets, or shards.
- **CI:** pin Actions by SHA. Keep the build and sign jobs separate. Tokens are least-privilege.
- **Language:** code, comments, docs and commits are in English. The research originals in `docs/research/` stay in
  Turkish.
- **When you're unsure about an upstream fact:** verify it against the live source, or mark it `verified: false`.
  Never invent it.

---

## 9. Roadmap (re-scoped in v0.2: ship the differentiator before the 2027 kickoff)

| Milestone | Scope | Exit criteria |
|---|---|---|
| **M0: Skeleton & contracts** ✅ *(done 2026-09-29; open items: MCP conformance suite, goreleaser snapshot run in CI)* | go.mod; `serve` over stdio on a hand-built fixture shard; **render contract** (parity, fencing, truncation, cursors); static-surface golden tests; netguard; CI (lint, race, vuln, conformance); goreleaser snapshot | `frc_search` works end to end on the fixture; every golden test is green |
| **M1: WPILib Java vertical slice** ✅ *(done 2026-09-29: hybrid R@10 0.976 · nDCG@10 0.857 · wrong-season@5 0 · p95 5 ms; dense kept, +0.079 nDCG; open item: a human-written holdout)* | frc-docs (stable + latest) and Java symbol tables (Javadoc search indexes / class files) for 2026 and 2027-alpha; BM25 plus exact symbol lookup, with potion as an experiment; `frc_context`, `frc_search`, `frc_fetch`, `frc_api`; qrels v0 (≥ 150, cross-season bucket, human holdout) | Recall@10 ≥ 0.75, nDCG@10 ≥ 0.55, **wrong-season@5 = 0**, p95 ≤ 50 ms; potion kept only if it adds ≥ 1 nDCG point |
| **M2: Distribution + verifier MVP** ✅ *(done 2026-09-29: verifier 0 errors in 128k LOC of 12 public 2026 team repos; signed sync with rollback/tamper tests; frc_vendordep from the WPILib catalog; open items: production key, first publish, cross-OS sync check in CI)* (target: before January 2027) | Fixed-cron ingestion; OCI + HTTPS mirror; cosign with a pinned identity; serial and expiry; `frc-mcp sync` with background first-run sync; fact tables; `frc_vendordep` (single mode and set mode); **`frc_verify_code` MVP** (Java imports and types, `wrong_season`) | Verifier false positives ≤ 1 per 1k LOC on clean public 2026 team repos; signed shards verified on all 3 operating systems |
| **M3: Vendors & languages** 🚧 *(vendor docs done 2026-09-29: 7 libraries, vendor R@10 0.909 · nDCG@10 0.770; next: vendor Java APIs, C++/Python symbols, injection corpus, `frc_whats_new`, `frc_hardware`)* | CTRE, REV, PhotonVision, PathPlanner, Choreo, AdvantageKit, YAGSL docs and APIs; C++ (Doxygen XML) and Python (`.pyi`) symbol tables; trust tiers, sanitizer, opt-in forum; `frc_whats_new`; `frc_hardware` | Injection corpus: suspect recall ≥ 0.95; per-(library, language) coverage published |
| **M4: Migration & agent eval** | `frc_migrate` (≈ 50 curated rules plus a diff-generated 2027 move table); Java member-level verification; agent-level eval with a GradleRIO compile check | Agent compile-pass rate ≥ +20 pp over the same agent without MCP |
| **M5: Hosted profile & launch** | ADR-0005 hosted-full (dense model and rerank sidecar, auth, aggregate metrics); `--toolset openai`; adaptive scheduler; Homebrew, Scoop and Winget; Authenticode and notarization; registry entry; make the repo public | Launch checklist in `docs/security.md` is green |

Deferred: Game Manual and rules ingestion, `frc_scaffold`, and TOON (rejected; see ADR-0004).
