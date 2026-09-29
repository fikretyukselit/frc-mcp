# frc-mcp

**FRC knowledge for AI coding agents, correct for the season you use.** frc-mcp is a Model Context Protocol server.
It gives Claude Code, Cursor, VS Code Copilot and other MCP clients current FIRST Robotics Competition software
knowledge, pinned to the versions your robot project uses:
- WPILib docs and the Java, C++ and Python APIs (2026 and the 2027 alpha);
- vendor libraries: CTRE Phoenix 6, REVLib, PhotonVision, PathPlanner, Choreo, AdvantageKit, YAGSL;
- the vendordep catalog, release notes and motor/hardware specs.

## Why

Coding agents write robot code against whatever season they saw most in training. With the 2027 season's API
changes (`org.wpilib.*` packages, `ChassisVelocities`, Telemetry/Tunables, new Phoenix 6 constructors), that code
does not compile. frc-mcp answers every question for one season, never blends seasons, maps old APIs to new ones
with cited sources, and checks code against the real API before the agent says it is done.

**Measured** (`docs/benchmarks.md`): the same agent solving 26 robot-code tasks, compiled against the GradleRIO
classpath. With frc-mcp, **98.7%** of Sonnet's solutions compile, against **38.5%** without it; for 2027 tasks alone,
98% against 20%. Haiku improves by 42 points.

## Use the hosted server

No install needed: add `https://mrkaynak.com/frc/mcp` as a Streamable HTTP MCP server. For Claude Code:

```sh
claude mcp add --transport http frc https://mrkaynak.com/frc/mcp
```

The hosted server keeps no request content (aggregate counters only) and is rate-limited per client. It cannot read
your files, so tools that take a `path` want the code inline; for whole-project checks install locally.

## Install

Download the archive for your OS from the [latest release](https://github.com/fikretyukselit/frc-mcp/releases/latest)
and put `frc-mcp` on your `PATH`, or build it with Go 1.27+:

```sh
go install github.com/fikretyukselit/frc-mcp/cmd/frc-mcp@latest
```

Add it to your MCP client. For Claude Code:

```sh
claude mcp add frc -- frc-mcp serve
```

For clients configured with JSON (Cursor, VS Code, Claude Desktop), use a pinned path:

```json
{ "mcpServers": { "frc": { "command": "/usr/local/bin/frc-mcp", "args": ["serve"] } } }
```

On first start the server downloads the signed index (about 50 MB) in the background and keeps it updated. Every
download is verified with the Foundation's ed25519 key before use. `frc-mcp sync` does the same by hand, and
`frc-mcp doctor` shows what is installed.

## Tools

| Tool | What it does |
|---|---|
| `frc_context` | Detects the project's season, language, WPILib and vendordep versions and returns a pin for the other tools |
| `frc_search` | Searches docs and APIs for one season (BM25 + exact symbols + embeddings), with citations |
| `frc_fetch` | Returns a full doc section by id |
| `frc_api` | Exact symbol lookup: signatures, deprecations, replacements, other seasons |
| `frc_verify_code` | Checks Java, C++ or Python robot code against the season's real API: wrong-season names, calls that cannot compile, deprecations, each with the fix |
| `frc_migrate` | Maps symbols or a whole file to another season (2026 → 2027) from 238 cited rules plus generated package moves |
| `frc_vendordep` | Resolves vendordeps from the official catalog and checks an installed set for conflicts and wrong years |
| `frc_whats_new` | Release notes of WPILib and the vendors |
| `frc_hardware` | Motor, encoder, IMU and swerve module specs, each source labeled and never merged, with WPILib sim factories |

The same checks work from the command line:

```sh
frc-mcp verify path/to/robot            # every Java/C++/Python file against the project's season
frc-mcp migrate --to 2027 path/to/robot # what a 2026 project must change for 2027
```

## Content and licenses

The code is MIT. Indexed content keeps its upstream license, and every result carries it in `citation.license`.
Vendor content that states no license (REV docs, the Phoenix 6 and REVLib Java APIs, YAGSL docs, CTRE/REV release
notes) is published with its citation and a link back; a publisher that objects has it removed. Content under a
license that forbids distribution (CTRE's C++ header EULA) is never published or served; build it into a local index
with `frc-mcp index run` (see `docs/sources.md` §0.2). Forum posts are opt-in and local only.

## Running a shared server

`docs/deploy.md` describes the hosted profile: a container behind Caddy with automatic HTTPS, anonymous access with
per-client rate limits, no query logging.

## Development

`make test` · `make race` · `make lint` · `make bench` · `make fuzz` · `make golden`. Pure Go, `CGO_ENABLED=0`.
Start with [`CONTRIBUTING.md`](CONTRIBUTING.md); design docs: [architecture](docs/architecture.md),
[retrieval](docs/retrieval.md), [ingestion](docs/ingestion.md), [MCP surface](docs/mcp-surface.md),
[security](docs/security.md), [sources](docs/sources.md), [benchmarks](docs/benchmarks.md), [ADRs](docs/adr/).

Maintained by the [Fikret Yüksel Foundation](https://github.com/fikretyukselit).
