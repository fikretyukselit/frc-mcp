# Architecture

Status: **Draft v0.1** (2026-09-28). Owner: frc-mcp maintainers. Supersedes: none.

## 1. Problem statement

LLM coding agents write FRC robot code from stale training data. The FRC ecosystem breaks APIs every season and is
mid-way through the largest break in a decade (roboRIO → Systemcore, WPILib 2027: `edu.wpi.first` → `org.wpilib`,
`frc::` → `wpi::`, SmartDashboard/Sendable → Telemetry/Tunables, AprilTag/CameraServer moved to vendordeps, vendor
libraries must be re-imported). Vendors ship on independent cadences (Phoenix 6 26.x, REVLib 2026/2027-alpha,
PathPlanner, Choreo v2-only vs Commands v3). The result: code that does not compile, or worse, compiles against the
wrong season and fails on the robot.

**Goal:** give the agent, at the moment it writes code, the exact API surface and documentation for the versions the
team actually uses, with citations, in under 50 ms, on a student laptop, offline.

**Non-goals (v1):** match/event analytics (TBA/Statbotics/FRC Events are optional adapters, not core), log analysis
(wpilog-mcp already does this well — interoperate, don't duplicate), code generation on the server, hosting user data.

## 2. Requirements

| # | Requirement | Type |
|---|---|---|
| R1 | Answers are scoped to the project's pinned versions (auto-detected) | Functional |
| R2 | Every result carries provenance (URL, version, upstream revision, retrieved-at) | Functional |
| R3 | Detects wrong-season / unknown / deprecated symbols in code | Functional |
| R4 | Index freshness: stable-channel changes visible ≤ 24 h; kickoff/alpha-release changes ≤ 6 h | Operational |
| R5 | p95 search ≤ 50 ms, cold start ≤ 150 ms, RSS ≤ 150 MB | Performance |
| R6 | Single static binary, `CGO_ENABLED=0`, macOS/Linux/Windows × amd64/arm64 | Distribution |
| R7 | Works fully offline after first sync | Availability |
| R8 | Polite to upstreams (conditional GET, per-host limits, robots, ToS) | Compliance |
| R9 | Supply chain: signed binaries and signed index shards, SBOM, provenance | Security |

## 3. Components

### 3.1 Ingestion plane (runs in GitHub Actions on a schedule; also `frc-mcp index` locally)

1. **Source registry** (`data/sources.yaml`): declarative list of sources — adapter type, endpoint(s), library, season
   mapping, channel, license, politeness overrides, prior change rate, `verified` flag. See `docs/sources.md`.
2. **Scheduler** (`internal/ingest/sched`): decides *which* sources to poll *now* under a per-host budget using a
   Gamma–Poisson change-rate model with a season-calendar prior and Thompson sampling (`docs/ingestion.md §3`).
3. **Fetcher** (`internal/ingest/fetch`): conditional GET (ETag / Last-Modified), validator store, per-host token bucket,
   robots.txt, `Retry-After`, exponential backoff with jitter, response size caps, identifying User-Agent.
4. **Adapters** (`internal/ingest/source/*`): turn raw payloads into `Document`s: RTD/Sphinx, GitBook (`llms.txt` /
   `.md`), Javadoc, Doxygen, vendordep JSON, Maven metadata, PyPI JSON, GitHub releases (GraphQL batch + Atom fallback),
   RSS/Atom (Chief Delphi, YouTube), PDF (Game Manual/Team Updates).
5. **Normalizer** (`internal/ingest/normalize`): canonical Markdown; strips chrome (nav, "Edit on GitHub", build IDs,
   timestamps); computes `raw_hash` and `norm_hash`. Only a `norm_hash` change triggers re-chunking.
6. **Chunker** (`internal/ingest/chunk`): structure-aware (heading path for prose; one chunk per API symbol; one per
   release-note item; one per forum post). Emits a deterministic **context prefix**
   (`library › version › page title › section path › symbol signature`) that is indexed with the chunk.
7. **Symbol extractor** (`internal/apisym`): builds per-`(library, version, language)` symbol tables from Javadoc
   (`element-list` + class pages), Doxygen (tagfiles / XML), Python stubs; records `since`, `deprecated`, `removed_in`.
8. **Embedder**: model2vec potion-code-16M-v2 (the same model used at query time — mandatory for vector compatibility).
9. **Eval gate**: runs `eval/qrels` against the candidate shard set; blocks publish on regression (`docs/retrieval.md §6`).
10. **Publisher** (`internal/dist`): writes shards, zstd-compresses, pushes to `ghcr.io/fikretyukselit/frc-mcp-index`
    via oras-go with a custom `artifactType`, signs digests with cosign keyless (GitHub OIDC), updates the manifest tag.

### 3.2 Serving plane (user machine)

1. **Transport:** stdio (default, what Claude Code/Cursor/VS Code launch) and stateless Streamable HTTP (for shared
   team/school deployments; any instance serves any request per the 2026-07-28 stateless core).
2. **Shard manager:** opens shards read-only/immutable, mmaps vector blobs, verifies signatures at sync time (not at
   query time), hot-swaps atomically on sync.
3. **Project detector** (`internal/project`): parses `build.gradle(.kts)` GradleRIO version and `vendordeps/*.json`
   (`name`, `version`, `frcYear`, `uuid`) into a **Pin set**. Exposed via `frc_context`; results are an opaque `pin`
   handle (server-minted, stateless — encodes the pin set, so any instance can decode it).
4. **Router** (`internal/router`): deterministic decision model; see §4.
5. **Retriever** (`internal/retrieve`): parallel BM25 (FTS5), int8 flat vector, exact-symbol lookups; RRF; boosts;
   abstention. See `docs/retrieval.md`.
6. **Verifier** (`internal/apisym`): lightweight lexers for Java/C++/Python extract imports + qualified identifiers +
   member calls; resolves against the pinned symbol tables; emits findings with fixes (from `internal/migrate`).
7. **Freshness probe** (optional, `--live`): for `frc_whats_new` / `frc_vendordep`, ETag-cached GETs to the vendordep
   `jsonUrl` and GitHub Atom feeds so "latest" answers are correct even between shard publishes. Bounded to 300 ms;
   on timeout the shard answer is returned with `freshness: "shard"`.
8. **Renderer** (`internal/mcpserver`): `structuredContent` (typed, schema'd) + compact Markdown `content` for clients
   that only read text; enforces token budgets; emits `resource_link`s instead of inlining full pages.

## 4. Decision models

We use small, explainable decision models — not an LLM — everywhere a decision is on the hot path.

### 4.1 Query router (serving)

Input: tool args + pin set. Output: `{intent, libraries[], version, language, retrievers[]}`.

| Signal | Rule |
|---|---|
| Identifier-shaped query (`CamelCase`, `a.b.C`, `ns::Type`, `method(`) | intent=`symbol`; exact symbol retriever first, BM25 second; dense disabled or down-weighted |
| Library mention / import prefix (`com.ctre.phoenix6`, `com.revrobotics`, `org.photonvision`, `rev::spark`) | restrict `libraries` |
| Version tokens (`2026`, `2027`, `alpha-7`, `Phoenix 5`, `v6`) | override pin for this query; flag `pin_override: true` |
| Language evidence (`#include`, `::`, `def `, `self.`, `import edu.`) | set `language` |
| "how do I / example / tutorial" phrasing | intent=`howto`; prose + code-sample chunks boosted |
| "error / exception / doesn't work / known issue" | intent=`troubleshoot`; known-issues + release notes + forum boosted |
| Rule-ish terms (`R501`, `G204`, "frame perimeter", "bumper") | intent=`rules`; Game Manual/Q&A shard only |

Rules live in data (`data/router.yaml`) with unit tests per rule; precedence is explicit. Unknown → `general` (all retrievers).

### 4.2 Abstention / confidence (serving)

Confidence = logistic model over cheap features: top RRF score, gap top1–top2, BM25/dense agreement (rank overlap),
exact-symbol hit, version match, chunk authority (official doc > vendor doc > release note > forum). Coefficients are
fit offline on `eval/qrels` (train split) and committed as `data/abstain.json`; threshold chosen for ≥ 0.9 precision on
the holdout. Output: `status ∈ {ok, low_confidence, no_match, version_mismatch}` + `confidence ∈ [0,1]`.

### 4.3 Recrawl scheduler (ingestion)

Bayesian change-rate estimation with season-aware priors and Thompson sampling. Full spec in `docs/ingestion.md §3`.

### 4.4 Version resolution (serving)

Precedence: explicit tool arg > query tokens > `pin` handle > auto-detected project > server default
(`--default-season`, defaults to the current stable season). The chosen source of truth is echoed in every response
(`pin_source`) so the agent knows why it got 2026 vs 2027 results.

## 5. Data model

### 5.1 Shard

A shard is an immutable SQLite file (+ sidecar `.vec` int8 blob) scoped to `(source_group, season)`, e.g.
`wpilib-docs@2026`, `ctre@2026`, `wpilib-api-java@2027a7`, `forum@rolling`. Manifest (`manifest.json`, signed) lists
shard name, schema version, digest, byte size, chunk count, embed model id + dims, built-at, upstream revisions.

```sql
-- schema v1 (abridged)
CREATE TABLE chunk (
  id INTEGER PRIMARY KEY, doc_id TEXT NOT NULL, ord INTEGER NOT NULL,
  library TEXT NOT NULL, version_lo TEXT, version_hi TEXT, season TEXT NOT NULL, channel TEXT NOT NULL,
  language TEXT NOT NULL, kind TEXT NOT NULL,          -- prose|code|api|release|rule|forum
  title TEXT, heading_path TEXT, symbol TEXT, prefix TEXT NOT NULL, body TEXT NOT NULL,
  source_url TEXT NOT NULL, anchor TEXT, upstream_rev TEXT NOT NULL, retrieved_at INTEGER NOT NULL,
  license TEXT NOT NULL, authority INTEGER NOT NULL, tokens INTEGER NOT NULL, vec_off INTEGER NOT NULL
);
CREATE VIRTUAL TABLE chunk_fts USING fts5(symbol, title, heading_path, prefix, body,
  content='chunk', content_rowid='id', tokenize="unicode61 tokenchars '_.:'");
CREATE TABLE symbol (
  fqn TEXT, library TEXT, version TEXT, language TEXT, kind TEXT, signature TEXT,
  since TEXT, deprecated_in TEXT, removed_in TEXT, replacement TEXT, chunk_id INTEGER,
  PRIMARY KEY (library, version, language, fqn)
) WITHOUT ROWID;
CREATE TABLE edge (src INTEGER, dst INTEGER, kind TEXT);  -- doc→symbol mentions, symbol→parent, see-also
```

FTS5 column weights at query time: `bm25(chunk_fts, 8.0, 4.0, 2.0, 1.5, 1.0)` (symbol ≫ title > heading > prefix > body).
Tokenizer keeps `_ . :` inside tokens so `SwerveDriveKinematics.toSwerveModuleStates` and `frc::DCMotor` survive.

### 5.2 Pin set

```json
{ "season": "2026", "channel": "stable", "language": "java",
  "libs": { "wpilib": "2026.2.1", "phoenix6": "26.1.0", "revlib": "2026.0.0", "pathplannerlib": "2026.1.2" } }
```

Encoded as a compact, versioned, base64url token for the `pin` handle (no server state).

## 6. Distribution & update flow

1. Binary: goreleaser → GitHub Releases, Homebrew cask, Scoop, Winget, `go install`. Cosign-signed checksums,
   SLSA provenance attestation, Syft SBOM.
2. Index: `frc-mcp sync` resolves `ghcr.io/fikretyukselit/frc-mcp-index:<channel>` → manifest digest; downloads only
   shards whose digest changed (content-addressed = free delta); verifies cosign signature against the repo's GitHub
   OIDC identity; decompresses to a temp dir; atomic rename. First run of `serve` triggers sync if no shards exist.
3. Background: `serve` checks the manifest tag at most every 6 h (configurable, `--offline` disables) and hot-swaps.

## 7. Security & compliance

- Read-only tools; no filesystem writes outside the cache dir; `frc_context` reads only `build.gradle*`, `settings.gradle*`,
  `vendordeps/*.json` under the given project root (path-traversal guarded, size-capped).
- No tokens on the serving plane. Ingestion tokens (GitHub, optional TBA/FRC Events) live in CI secrets only.
- Licensing: store snippets + links; full-page `frc_read` only for sources whose license permits redistribution
  (recorded per source; unknown → snippet-only + link). FRC Events data is non-commercial only.
- HTTP mode: bind localhost by default; optional bearer token; DNS-rebinding protection (Origin check).

## 8. Observability

`log/slog` JSON to stderr; OpenTelemetry traces (W3C trace context propagated from `_meta` per SEP-414) in HTTP mode;
`frc-mcp doctor` prints shard ages, freshness vs upstream, benchmark smoke, config. Ingestion publishes a
`freshness.json` report (per source: last change seen, last poll, estimated λ, next poll) as a release asset.

## 9. Key risks

| Risk | Mitigation |
|---|---|
| 2027 alpha churn makes symbol tables stale weekly | Alpha channel shards on 6 h cadence; `frc_whats_new --live` |
| Static embeddings too weak for prose | Hybrid with BM25 (lexical carries identifiers); optional rerank; v2: domain-distilled model2vec |
| Upstream URL rot / unverified endpoints | `make probe` nightly; `verified` flag; alerts on 404/410 |
| Licensing of vendor docs | Per-source license field; snippet-only fallback |
| Scraping fragility (Sphinx/GitBook theme changes) | Prefer source repos (RST/MD in GitHub) over rendered HTML; golden tests per adapter |
| go-sdk / go-github API churn | Wrap behind internal interfaces; pin versions; Renovate |
