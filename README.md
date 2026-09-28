# frc-mcp

**FRC knowledge for AI coding agents, correct for the version you use.** frc-mcp is a Model Context Protocol server.
It gives Claude Code, Cursor, VS Code Copilot and other MCP clients current FIRST Robotics Competition software
knowledge, pinned to the versions your robot project actually uses. It covers:
- WPILib docs and the Java/C++/Python APIs;
- vendor libraries such as CTRE Phoenix 6, REVLib, PhotonVision, Limelight, PathPlanner, Choreo, AdvantageKit and YAGSL;
- vendordeps and release notes.

> **Status:** M1 is done. The server indexes the real WPILib docs and Java API for 2026 and 2027-alpha:
> - 7.8k chunks and 35k API symbols;
> - retrieval quality of Recall@10 0.976 and nDCG@10 0.857, with **0 wrong-season results**;
> - search p95 of about 5 ms.
>
> Vendor libraries arrive in M3. See [`CLAUDE.md`](CLAUDE.md) §9 for the roadmap.

## Why

LLMs write robot code against last season's API. frc-mcp pins every answer to one FRC season and never blends
seasons. When a confident answer exists only in another season (for example `CANSparkMax` in 2026, or
`edu.wpi.first` packages in 2027), it returns `version_mismatch` and points to where that answer lives. Every result
carries a citation and a trust tier. Forum text is fenced off as untrusted data.

## Quick start

```sh
make index            # fetch + build the WPILib index into .shards (~25 s cold, conditional GET afterwards)
make doctor           # index status and search latency on this machine
make eval             # retrieval metrics vs the committed baseline
bin/frc-mcp serve --index .shards      # stdio MCP server
```

Client config (use a pinned path or version, never `@latest`):

```json
{ "mcpServers": { "frc": { "command": "/path/to/frc-mcp", "args": ["serve", "--index", "/path/to/shards"] } } }
```

## Tools (M1)

| Tool | Purpose |
|---|---|
| `frc_context` | Detects the project's season, language, WPILib and vendordep versions (from `build.gradle`, `vendordeps/`, `pyproject.toml`) and returns a pin handle |
| `frc_search` | Hybrid search pinned to one season: BM25, exact symbol lookup and dense (model2vec) results combined with RRF, then boosts, confidence and abstention |
| `frc_fetch` | Returns a full section by id (search → fetch), paged by token budget |
| `frc_api` | Exact lookup across 35k API symbols: signatures, deprecated/removed status, and replacements, including the generated `edu.wpi.first` → `org.wpilib` map |

The v0.2 surface grows to 9 tools: `frc_verify_code`, `frc_migrate`, `frc_vendordep`, `frc_whats_new` and
`frc_hardware` are added. See [MCP surface](docs/mcp-surface.md).

## Performance (Apple M2, real WPILib index)

| Measurement | Result |
|---|---|
| `frc_search` p95 (hybrid) | ~5 ms |
| Warm launch to first `tools/list` | 19 ms |
| Query embedding | 4.7 µs |

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

## License

The frc-mcp source code is released under the [MIT License](LICENSE). Content served from index shards (WPILib and vendor
documentation, API references, forum posts) keeps its **upstream license**. Every result carries it in
`citation.license`, and full-page redistribution follows the per-source rules in [`docs/security.md`](docs/security.md)
and [`docs/sources.md`](docs/sources.md).

Maintained by the [Fikret Yüksel Foundation](https://github.com/fikretyukselit).
