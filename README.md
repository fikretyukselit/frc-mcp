# frc-mcp

**FRC knowledge for AI coding agents, correct for the version you use.** frc-mcp is a Model Context Protocol server.
It gives Claude Code, Cursor, VS Code Copilot and other MCP clients current FIRST Robotics Competition software
knowledge, pinned to the versions your robot project actually uses. It covers:
- WPILib docs and the Java/C++/Python APIs;
- vendor libraries such as CTRE Phoenix 6, REVLib, PhotonVision, Limelight, PathPlanner, Choreo, AdvantageKit and YAGSL;
- vendordeps and release notes.

> **Status:** M0 (skeleton and contracts) is done. The server runs end to end on a synthetic fixture index. Real WPILib
> ingestion arrives in M1. See [`CLAUDE.md`](CLAUDE.md) §9 for the roadmap.

## Why

LLMs write robot code against last season's API. frc-mcp pins every answer to one FRC season and never blends
seasons. When a confident answer exists only in another season (for example `CANSparkMax` in 2026, or
`edu.wpi.first` packages in 2027), it returns `version_mismatch` and points to where that answer lives. Every result
carries a citation and a trust tier. Forum text is fenced off as untrusted data.

## Quick start (fixture index)

```sh
make fixture          # builds bin/frc-mcp and .shards/fixture.sqlite
make doctor           # index status and search latency on this machine
bin/frc-mcp serve --index .shards      # stdio MCP server
```

Client config (use a pinned path or version, never `@latest`):

```json
{ "mcpServers": { "frc": { "command": "/path/to/frc-mcp", "args": ["serve", "--index", "/path/to/shards"] } } }
```

## Tools (M0)

| Tool | Purpose |
|---|---|
| `frc_search` | Hybrid search pinned to one season: BM25 plus exact symbol lookup, RRF, boosts, confidence and abstention |
| `frc_fetch` | Full section by id (search → fetch progressive disclosure), paged by token budget |
| `frc_api` | Exact API symbol lookup: signatures, deprecated/removed status, replacements, other-season locations |

The v0.2 surface grows to 9 tools: `frc_context`, `frc_verify_code`, `frc_migrate`, `frc_vendordep`, `frc_whats_new`
and `frc_hardware` are added. See [MCP surface](docs/mcp-surface.md).

## Performance (Apple M2, fixture corpus)

| Measurement | Result |
|---|---|
| `frc_search` p95 | 0.43 ms |
| Cold launch to first `tools/list` | 31 ms |
| Exact vector scan of 100k vectors | 2 ms |

More numbers are in [benchmarks](docs/benchmarks.md).

## Development

`make test` · `make race` · `make lint` · `make bench` · `make fuzz` · `make golden`. The build uses pure Go only,
with `CGO_ENABLED=0`.

## Docs

- [Architecture](docs/architecture.md)
- [Retrieval](docs/retrieval.md)
- [Ingestion](docs/ingestion.md)
- [MCP surface](docs/mcp-surface.md)
- [Security](docs/security.md)
- [Source catalog](docs/sources.md)
- [Benchmarks](docs/benchmarks.md)
- [ADRs](docs/adr/)
- [Reviews](docs/reviews/)

Maintained by the [Fikret Yüksel Foundation](https://github.com/fikretyukselit).
