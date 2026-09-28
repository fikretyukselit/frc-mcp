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
| Project detection + pin handles (`internal/project`) | ✅ implemented |
| Eval harness + judged queries + CI gate (`internal/eval`, `eval/`) | ✅ 184 queries; a human-written holdout is still needed |
| Egress guard, sanitizer (`internal/netguard`, `internal/ingest/sanitize`) | ✅ implemented |
| Tools `frc_search`, `frc_fetch`, `frc_api`, `frc_context`, `frc_vendordep`, `frc_verify_code` (Java) | ✅ implemented |
| Vendordep catalog facts (`internal/facts`, `source/vendordeps`) | ✅ WPILib vendor-json-repo, 2026 + 2027-alpha |
| Verifier (`internal/verify`, `frc-mcp verify`) | ✅ Java: 0 false errors on 128k LOC of public 2026 team code |
| Signed distribution (`internal/dist`, `frc-mcp sync / index publish / index keygen`, `.github/workflows/index.yml`) | ✅ code + tests; the first publish is waiting on the production key (ADR-0006) |
| `frc_migrate`, `frc_whats_new`, `frc_hardware`, vendor docs/APIs, C++/Python symbol tables | ⏳ M3–M4 |

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
