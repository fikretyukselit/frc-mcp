# frc-mcp

**FRC knowledge for AI coding agents, correct for the season you use.**

MCP (Model Context Protocol) is how AI coding tools such as Claude Code, Cursor, VS Code Copilot and Codex plug in
extra knowledge. Add frc-mcp once, and your agent looks up the real WPILib and vendor APIs for your season instead of
guessing from memory:

- WPILib docs and the Java, C++ and Python (RobotPy) APIs, for 2026 and the 2027 alpha;
- vendor libraries: CTRE Phoenix 6, REVLib, PhotonVision, PathPlanner, Choreo, AdvantageKit, YAGSL;
- the vendordep catalog, release notes, and motor and swerve-module specs.

Agents trained on older seasons write code that no longer compiles (2027 moved WPILib to `org.wpilib.*`, renamed
`ChassisSpeeds`, changed vendor constructors). In our benchmark, Sonnet's Java solutions compiled 98.7% of the time
with frc-mcp and 38.5% without it ([details](docs/benchmarks.md)).

## Quick start (hosted, nothing to install)

You need one of the AI tools below, installed and signed in. frc-mcp itself needs no install, account or key.

| Your tool | Do this |
|---|---|
| Claude Code | In a terminal: `claude mcp add -s user --transport http frc https://mrkaynak.com/frc/mcp` |
| Codex CLI | In a terminal: `codex mcp add frc --url https://mrkaynak.com/frc/mcp` |
| Cursor | In Cursor Settings, open the MCP section and add a server, or put this in `~/.cursor/mcp.json` (Windows: `%USERPROFILE%\.cursor\mcp.json`): `{ "mcpServers": { "frc": { "url": "https://mrkaynak.com/frc/mcp" } } }` |
| VS Code (Copilot) | Create `.vscode/mcp.json` in your robot project: `{ "servers": { "frc": { "type": "http", "url": "https://mrkaynak.com/frc/mcp" } } }`, then use Copilot in Agent mode |
| Claude Desktop / claude.ai | Settings → Connectors → Add custom connector, name `frc`, URL `https://mrkaynak.com/frc/mcp` (needs a plan that offers custom connectors) |

`-s user` makes the server available in every folder for Claude Code; without it, only in the folder you ran the
command in. If you already have an entry called `frc` (for example a local install), replace it.

### Check that it works

1. Restart or reload your tool, then look for `frc` in its server list: `/mcp` in Claude Code, `codex mcp list` in
   Codex, Settings → MCP in Cursor, "MCP: List Servers" in the VS Code command palette.
2. Ask: **"Use frc-mcp to look up the Java class TimedRobot for season 2026, with its source."** You should see a
   tool call such as `frc_api` and an answer that cites docs.wpilib.org or the WPILib API docs.

### What to ask

Say which season and language you use; the hosted server cannot see your files.

- "This is a 2026 Java project. Write an arm subsystem with a REV SPARK MAX and check the code with frc-mcp."
- "Port this 2026 subsystem to 2027 and list every API that changed, with sources." (paste the file)
- "This is a 2026 RobotPy project. Why does `rev.CANSparkMax` fail to import?"
- "Which REVLib vendordep goes with WPILib 2026.2.2, and does my vendordeps folder have conflicts?"
- "Kraken X60 specs and the matching WPILib DCMotor sim factory."
- "What changed in PathPlanner since 2026.1.0?"

The agent checks its code with `frc_verify_code`, which catches wrong-season names, calls no overload accepts and
deprecated APIs, each with the fix. It does not compile or test your robot program: run your normal build too.
frc-mcp does not cover the Game Manual or competition rules.

## Install locally

A local server also reads your robot project: it detects the season and vendordeps from `build.gradle` or
`pyproject.toml`, and keeps working without internet once the index is downloaded.

**1. Install** (macOS Terminal or Windows PowerShell):

| Platform | Command |
|---|---|
| macOS / Linux, with [Homebrew](https://brew.sh) | `brew install fikretyukselit/tap/frc-mcp` |
| Windows, with [Scoop](https://scoop.sh) (`irm get.scoop.sh \| iex` installs it) | `scoop bucket add fikretyukselit https://github.com/fikretyukselit/scoop-bucket` and then `scoop install frc-mcp` |
| Any, with Go 1.27+ | `go install github.com/fikretyukselit/frc-mcp/cmd/frc-mcp@latest` |
| Manual | Download your OS's archive from the [latest release](https://github.com/fikretyukselit/frc-mcp/releases/latest) and unpack it anywhere |

**2. Find the full path** of the program: `which frc-mcp` on macOS/Linux, `(Get-Command frc-mcp).Source` in
PowerShell (or the folder you unpacked it to). Desktop apps such as Claude Desktop, Cursor and VS Code often do not
see your terminal's `PATH`, so use the full path in their config. In JSON on Windows, double the backslashes:
`"C:\\Users\\you\\scoop\\shims\\frc-mcp.exe"`.

**3. Add it to your tool** (replace `/full/path/to/frc-mcp`):

| Your tool | Do this |
|---|---|
| Claude Code | `claude mcp add -s user frc -- frc-mcp serve` |
| Codex CLI | `codex mcp add frc -- frc-mcp serve` |
| Cursor | `~/.cursor/mcp.json`: `{ "mcpServers": { "frc": { "command": "/full/path/to/frc-mcp", "args": ["serve"] } } }` |
| VS Code (Copilot) | `.vscode/mcp.json` in your robot project: `{ "servers": { "frc": { "type": "stdio", "command": "/full/path/to/frc-mcp", "args": ["serve"] } } }` |
| Claude Desktop | Settings → Developer → Edit Config (`~/Library/Application Support/Claude/claude_desktop_config.json` or `%APPDATA%\Claude\claude_desktop_config.json`); add `"frc": { "command": "/full/path/to/frc-mcp", "args": ["serve"] }` inside `"mcpServers"`, keeping any servers already there, then restart Claude Desktop |

**4. Check it** the same way as above, and run `frc-mcp doctor` in a terminal: it lists the installed index.

On first start frc-mcp downloads its index (about 55 MB, from github.com) in the background; until it is in, tools
answer `syncing`. It checks for updates every 6 hours. Each download is verified with the Fikret Yüksel Foundation's
signing key before use.

To check a whole project from the terminal, without an agent:

```sh
frc-mcp verify path/to/robot            # every Java/C++/Python file against the project's season
frc-mcp migrate --to 2027 path/to/robot # what a 2026 project must change for 2027
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| The tool does not list `frc`, or it fails to start | Use the full path to `frc-mcp` in the config (step 2), then restart the tool. For Claude Code, re-add with `-s user` |
| Every answer says `syncing` | The first download is still running. Run `frc-mcp sync` in a terminal to see it, then `frc-mcp doctor` |
| `frc-mcp sync` cannot reach github.com (school or event network) | Sync once on another network (the index stays on disk), use the hosted server, which only needs `mrkaynak.com`, or run `frc-mcp serve --offline` with an index you already have |
| The hosted server answers `429` (rate limited) | It allows about one request per second per network address, so a team behind one school network shares it. Wait a few seconds, or install locally |
| Answers are for the wrong season | Say the season and language in your request ("this is a 2027 C++ project") |
| Windows shows a SmartScreen warning on a manual download | The binaries are not code-signed yet: More info → Run anyway, or install with Scoop. On macOS, Homebrew clears the warning; for a manual download run `xattr -d com.apple.quarantine frc-mcp` once |

The index lives in `~/Library/Caches/frc-mcp/shards` (macOS), `~/.cache/frc-mcp/shards` (Linux) or
`%LocalAppData%\frc-mcp\shards` (Windows), about 180 MB on disk; `--index DIR` picks another folder.

## For mentors

- **Free and open source** (MIT). No account, API key or telemetry. The hosted server stores no request content,
  only aggregate counters, and is rate-limited.
- **Sources**: WPILib and vendor documentation and APIs only, each answer citing its source and license. Forum posts
  are never published or served (see [Content and licenses](#content-and-licenses)).
- **Limits**: it checks API usage, not robot logic, and does not compile code; the benchmark compiled Java only. With
  frc-mcp an agent uses more turns: about 2.2× the API cost per task for Sonnet ([benchmarks](docs/benchmarks.md)).

<details>
<summary>Verify a download (optional)</summary>

Every release ships `checksums.txt` with a Sigstore bundle and SLSA provenance:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/fikretyukselit/frc-mcp/.github/workflows/release.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --check --ignore-missing checksums.txt
gh attestation verify frc-mcp_*_darwin_arm64.tar.gz -R fikretyukselit/frc-mcp   # or your archive
```

</details>

## Tools

| Tool | What it does |
|---|---|
| `frc_context` | Detects the project's season, language, WPILib and vendordep versions and returns a pin for the other tools |
| `frc_search` | Searches the docs and APIs of one season, with citations |
| `frc_fetch` | Returns a full doc section by id |
| `frc_api` | Exact symbol lookup: signatures, deprecations, replacements, other seasons |
| `frc_verify_code` | Checks Java, C++ or Python robot code against the season's real API: wrong-season names, calls that cannot compile, deprecations, each with the fix |
| `frc_migrate` | Maps symbols or a whole file to another season (2026 → 2027) from 238 cited rules for that transition plus generated package moves |
| `frc_vendordep` | Resolves vendordeps from the official catalog and checks an installed set for conflicts and wrong years |
| `frc_whats_new` | Release notes of WPILib and the vendors |
| `frc_hardware` | Motor, encoder, IMU and swerve module specs, each source labeled and never merged, with WPILib sim factories |

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
