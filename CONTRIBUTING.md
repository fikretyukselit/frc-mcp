# Contributing to frc-mcp

Thanks for helping FRC teams get correct robot code from their AI tools. This page explains how to get started.
The authoritative project context lives in [`CLAUDE.md`](CLAUDE.md). It is written for humans as well as coding
agents, and it lists what must stay true.

## 1. Read first (about 20 minutes)

| Read | For |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) §1–§5 | Mission, system shape, pinned stack, **invariants** (never break these) |
| [`docs/architecture.md`](docs/architecture.md) | Components, decision models, shard schema |
| [`docs/mcp-surface.md`](docs/mcp-surface.md) | The tool contract (inputs, outputs, errors) |
| [`docs/security.md`](docs/security.md) | Trust tiers, fencing, egress allowlist: required reading for anything touching ingestion or output |
| [`docs/adr/`](docs/adr/) | Why things are the way they are. Change a decision only through a new ADR. |
| [`docs/reviews/`](docs/reviews/) | Decision logs (English summaries of the Turkish research in `docs/research/`) |

## 2. What exists today vs. what is planned

The docs describe the **target** design. The code implements milestones **M0, M1 and M2**:

| Area | Status |
|---|---|
| Shard format, FTS5 + symbol tables (`internal/index`) | ✅ implemented |
| Retrieval engine, router, abstention (`internal/retrieve`, `internal/router`) | ✅ implemented (confidence coefficients are placeholders until the M1 eval) |
| Render contract (`internal/render`) | ✅ implemented, golden-tested |
| Vector layer (`internal/vec`) + pure-Go model2vec (`internal/embed/m2v`, golden-tested against Python) | ✅ implemented, dense retrieval on by default |
| Ingestion: `data/sources.yaml`, conditional-GET fetch, Sphinx + Javadoc adapters, cross-season symbol diff (`internal/ingest/*`, `internal/apisym`) | ✅ WPILib docs + Java API for 2026 and 2027-alpha |
| Vendor docs: `github-markdown`, `gitbook-llms` adapters, 5-dialect Markdown normalizer (`internal/ingest/source/{repomd,gitbook,markdown}`) | ✅ Phoenix 6, REVLib, PhotonVision, PathPlannerLib, Choreo, AdvantageKit, YAGSL (M3) |
| Project detection + pin handles (`internal/project`) | ✅ implemented |
| Eval harness + judged queries + CI gate (`internal/eval`, `eval/`) | ✅ 184 queries; a human-written holdout is still needed |
| Egress guard, sanitizer (`internal/netguard`, `internal/ingest/sanitize`) | ✅ implemented |
| Tools `frc_search`, `frc_fetch`, `frc_api`, `frc_context`, `frc_vendordep`, `frc_verify_code` (Java) | ✅ implemented |
| Vendordep catalog facts (`internal/facts`, `source/vendordeps`) | ✅ WPILib vendor-json-repo, 2026 + 2027-alpha |
| Verifier (`internal/verify`, `frc-mcp verify`) | ✅ Java, WPILib + 7 vendor libraries (checked only when that season's table is indexed; version skew caps findings at warning): 0 false errors on 128k LOC of public 2026 team code |
| Vendor Java APIs (Javadoc jars from vendor Maven repos) | ✅ Phoenix 6, REVLib, PhotonLib, PathPlannerLib, ChoreoLib, AdvantageKit, YAGSL (2026 + 2027-alpha where released) |
| Signed distribution (`internal/dist`, `frc-mcp sync / index publish / index keygen`, `.github/workflows/index.yml`) | ✅ code + tests; the first publish is waiting on the production key (ADR-0006) |
| `frc_migrate`, `frc_whats_new`, `frc_hardware`, C++/Python symbol tables | ⏳ M3–M4 |

The corpus in `testdata/fixture/` is **synthetic and illustrative**. Never treat it as FRC truth.

## 3. Setup

```sh
# Go 1.27+ (go.mod pins toolchain go1.27.1; GOTOOLCHAIN=auto fetches it)
make test       # all tests, CGO disabled (as shipped)
make race       # race detector
make lint       # go vet, gofmt, golangci-lint v2
make index      # fetch + build the real WPILib index into .shards (≈15 s; conditional GET afterwards)
make eval       # retrieval metrics + regression gate vs eval/baseline.json
make doctor     # measure latency on your machine
make serve      # run the MCP server over stdio on .shards
make fixture    # synthetic fixture shard (offline tests/demo) into .fixture
```

For an interactive check, point any MCP client (or a pinned, patched MCP Inspector) at
`bin/frc-mcp serve --index .shards`.

## 4. Rules for changes

- **Invariants** (`CLAUDE.md` §5) are enforced by tests. If a test guards one, fix your change, not the test.
- **Contracts:** if you change a tool schema, the chunk/shard schema or the render output, update
  `docs/mcp-surface.md` / `docs/architecture.md`, then run `make golden` and review the diff in the same PR.
- **Performance:** a PR touching `internal/retrieve`, `internal/index`, `internal/vec` or `internal/embed` must
  include `benchstat` output (`make bench` before and after). Budgets are in `CLAUDE.md` §6; measured numbers are in
  `docs/benchmarks.md`.
- **No cgo** in the default build. Prefer the standard library, and justify every new module in the PR.
- **Upstream facts** (URLs, API names, versions) must be verified against the live source or marked
  `verified: false`. Never guess them.
- **Security:** never add a tool argument that takes arbitrary URLs or file paths without going through
  `internal/netguard` or the project-root scoping rules in `docs/security.md` §2.3.
- **Style:** Conventional Commits, small PRs, code and docs in English, `log/slog` to stderr only (stdout is the
  stdio transport).

## 5. License

By contributing, you agree that your contributions are licensed under the project's [MIT License](LICENSE).

## 6. Good first contributions

- Add adversarial cases to the router tests (`internal/router/router_test.go`) or the sanitizer corpus.
- **Verifier false-positive reports:** run `frc-mcp verify path/to/robot` on your team's code. Every `error` on code
  that compiles is a bug; please open an issue with the snippet.
- **Most wanted:** human-written eval queries (`eval/queries.jsonl`, `"author": "human"`, `"split": "holdout"`).
  These should be real questions students asked, mapped to the WPILib page that answers them. The current 184
  queries were written by the plan author and are biased toward the docs' own vocabulary.
- Verify a `⚠`-marked endpoint in `docs/sources.md` and record the evidence.

## 7. Adding a source or adapter

Most M3 work is "teach the indexer a new upstream". The steps:

1. **Probe first.** Verify the endpoint by hand (license, robots.txt, whether it serves a bulk artifact such as a zip or
   a Maven documentation jar) and record the evidence in `docs/sources.md`. Prefer one bulk download over crawling pages.
2. **Declare it** in `data/sources.yaml`: `id`, `adapter`, `url`, `library`, `season`, `channel`, `version`,
   `license`, `trust` (`official` / `vendor` / `community`), `shard`, `verified`. That file is the only place endpoints
   live; the egress allowlist is derived from it. A redirect to another host needs an `extra_hosts` entry with a comment.
3. **Reuse an adapter if you can.**
   - `sphinx-htmlzip` handles any Sphinx site (RTD and Furo themes).
   - `javadoc-zip` handles any Javadoc zip. Vendor Java APIs are published this way on their Maven repos.
   - `github-markdown` handles Markdown docs in a GitHub repository (MyST, Docusaurus, MkDocs Material, Writerside).
     Pin a release tag, never `main`.
   - `gitbook-llms` handles GitBook sites that publish `llms.txt`.
   - Record the **docs** license. If the vendor publishes none, use `LicenseRef-<Vendor>-Docs-NoLicense`, and the
     shard will not be redistributed (`docs/sources.md` §0.2).
4. **New adapter:** add a package under `internal/ingest/source/<name>/` with a
   `Parse(path, src, rev, retrieved, emit...)` function (see `sphinx.Parse`), register the name in
   `internal/sources/sources.go` (`Adapters`) and the switch in `internal/ingest/build/build.go`. Emitted chunks must set
   library, season, version, trust and a stable `doc_id`. Run all text through `internal/ingest/sanitize`.
5. **Test offline:** a small recorded input under the adapter's `testdata/` plus a table test. CI never hits the network
   in unit tests.
6. **Prove it helps:** `make index && make eval`. Add a few judged queries for the new library to
   `eval/queries.jsonl`; the gate in `eval/baseline.json` must not regress (update the baseline in the same PR only when
   metrics improve).

## 8. Open work (pick one, open an issue first so two people don't take the same item)

| Item | Size | Where |
|---|---|---|
| Human-written eval holdout (≈ 50 real student questions) | S, no Go needed | `eval/queries.jsonl` |
| Run `frc-mcp verify` on your team's code, report false errors | S | issues |
| More vendor docs: ReduxLib, Studica, Limelight, maple-sim; CTRE 2027 docs | S–M each | §7, `data/sources.yaml` |
| Ask CTRE, REV and YAGSL for permission to redistribute doc/API excerpts (`docs/sources.md` §0.2) | S, no code | email |
| C++ symbols (Doxygen XML) and Python symbols (`.pyi` from robotpy) | L | new adapters |
| Injection corpus + suspect detection (recall ≥ 0.95) | M | `eval/security/injection/`, `internal/ingest/sanitize` |
| `frc_whats_new` (release notes, fenced as untrusted) and `frc_hardware` | L | `internal/mcpserver`, `docs/mcp-surface.md` |
| MCP conformance suite and goreleaser snapshot in CI; sync check on all 3 OSes | M | `.github/workflows/` |
| Packaging: Homebrew, Scoop, Winget | M | `.goreleaser.yaml` |
| M4: `frc_migrate`, Java member-level verification, agent compile-pass eval | L | `CLAUDE.md` §9 |
