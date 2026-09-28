# frc-mcp

**Version-correct FRC knowledge for AI coding agents.** A Model Context Protocol server that grounds Claude Code,
Cursor, VS Code Copilot, and other MCP clients in up-to-date FIRST Robotics Competition software knowledge — WPILib
docs and Java/C++/Python API, vendor libraries (CTRE Phoenix 6, REVLib, PhotonVision, Limelight, PathPlanner, Choreo,
AdvantageKit, YAGSL, …), vendordeps, release notes, and the season rulebook — pinned to the versions your robot
project actually uses.

> Status: **design phase**. See [`CLAUDE.md`](CLAUDE.md) and [`docs/`](docs/).

## Why
LLMs write robot code against last season's API. frc-mcp detects your WPILib + vendordep versions and answers only
from matching docs and API symbol tables — with citations — and can check your code for wrong-season or deprecated
calls before you deploy.

## Highlights (planned)
- `frc_search`, `frc_read`, `frc_api`, `frc_verify_code`, `frc_migrate`, `frc_vendordep`, `frc_whats_new`, `frc_context`
- Hybrid retrieval (BM25 + static embeddings + exact symbol lookup), < 50 ms p95, fully offline after sync
- Continuously refreshed, signed index shards; single static Go binary for macOS / Linux / Windows

## Docs
- [Architecture](docs/architecture.md) · [Retrieval](docs/retrieval.md) · [Ingestion](docs/ingestion.md)
- [MCP surface](docs/mcp-surface.md) · [Source catalog](docs/sources.md) · [ADRs](docs/adr/)

Maintained by the [Fikret Yüksel Foundation](https://github.com/fikretyukselit).
