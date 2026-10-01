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

## Quick start: the hosted server

No install, nothing to keep updated. Add `https://mrkaynak.com/frc/mcp` to your agent as a remote (Streamable HTTP)
MCP server:

| Client | How |
|---|---|
| Claude Code | `claude mcp add --transport http frc https://mrkaynak.com/frc/mcp` |
| Codex CLI | `codex mcp add frc --url https://mrkaynak.com/frc/mcp` |
| Cursor | `~/.cursor/mcp.json`: `{ "mcpServers": { "frc": { "url": "https://mrkaynak.com/frc/mcp" } } }` |
| VS Code (Copilot) | `.vscode/mcp.json`: `{ "servers": { "frc": { "type": "http", "url": "https://mrkaynak.com/frc/mcp" } } }` |
| Claude Desktop / claude.ai | Settings → Connectors → Add custom connector → `https://mrkaynak.com/frc/mcp` |

Then ask your agent something season-specific, for example:

> "This is a 2027 project. Write a swerve subsystem with Phoenix 6 TalonFX drive motors and check that it compiles."

The agent calls `frc_context` to pin the season, looks APIs up with `frc_api`, and runs `frc_verify_code` before
it says it is done. The hosted server keeps no request content (aggregate counters only) and is rate-limited per
client. It cannot read your files, so code goes in inline; to check a whole project, install locally.

## Install locally

A local server works offline, reads your project to detect its season and vendordeps, and checks every file.

| Platform | Command |
|---|---|
| macOS / Linux (Homebrew) | `brew install fikretyukselit/tap/frc-mcp` |
| Windows (Scoop) | `scoop bucket add fikretyukselit https://github.com/fikretyukselit/scoop-bucket` then `scoop install frc-mcp` |
| Windows (winget) | `winget install FikretYukselFoundation.frc-mcp` |
| Any, with Go 1.27+ | `go install github.com/fikretyukselit/frc-mcp/cmd/frc-mcp@latest` |
| Manual | download the archive for your OS from the [latest release](https://github.com/fikretyukselit/frc-mcp/releases/latest) and put `frc-mcp` on your `PATH` |

Then add it to your agent:

| Client | How |
|---|---|
| Claude Code | `claude mcp add frc -- frc-mcp serve` |
| Codex CLI | `codex mcp add frc -- frc-mcp serve` |
| Cursor, Claude Desktop | `{ "mcpServers": { "frc": { "command": "frc-mcp", "args": ["serve"] } } }` |
| VS Code (Copilot) | `.vscode/mcp.json`: `{ "servers": { "frc": { "type": "stdio", "command": "frc-mcp", "args": ["serve"] } } }` |

If your client cannot find `frc-mcp`, use the full path (`which frc-mcp`, or `where frc-mcp` on Windows).

**First run.** The server starts right away and downloads the signed index (about 55 MB, 184 MB on disk) in the
background; tools answer `syncing` until it is in. It re-checks every 6 hours. Every download is verified with the
Foundation's ed25519 key before use, so a mirror or a network in the middle cannot change what your agent reads.

| Command | What it does |
|---|---|
| `frc-mcp doctor` | shows the installed index: every shard, its seasons and build date |
| `frc-mcp sync` | downloads or updates the index now (`--verify` re-checks the installed one) |
| `frc-mcp serve --offline` | never touches the network; serves what is installed |

The index lives in `~/Library/Caches/frc-mcp/shards` (macOS), `$XDG_CACHE_HOME/frc-mcp/shards` (Linux) or
`%LocalAppData%\frc-mcp\shards` (Windows); `--index DIR` picks another one.

**Verify a download** (optional): every release ships `checksums.txt` with a Sigstore bundle and SLSA provenance.

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/fikretyukselit/frc-mcp/.github/workflows/release.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --check --ignore-missing checksums.txt
gh attestation verify frc-mcp_*_darwin_arm64.tar.gz -R fikretyukselit/frc-mcp   # or your archive
```

The binaries are not code-signed for macOS or Windows yet. Homebrew clears the macOS quarantine flag on install; for
a manual download on macOS, run `xattr -d com.apple.quarantine frc-mcp` once.

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
