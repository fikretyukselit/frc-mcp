# MCP surface (contract)

Status: **Draft v0.2** (2026-09-29).
- Target: MCP **2026-07-28**, implemented with `modelcontextprotocol/go-sdk` ≥ v1.7.0 (v1.8.x once confirmed GA).
- Changes since v0.1 are listed in `docs/reviews/2026-09-29-research-03-review.md §4.5`.

## 1. Design rules

- **Tools:**
  - At most 10 workflow-shaped tools, namespaced `frc_`, listed in a deterministic order.
  - Names stay stable across releases.
  - Descriptions and schemas are compiled in and golden-tested (`docs/security.md §2.2`).
- **Typed I/O:**
  - Every tool uses `mcp.AddTool[In, Out]`, with schemas inferred from Go structs.
  - `structuredContent` is the typed source of truth.
  - **`content` has meaning parity with `structuredContent`.** It is generated deterministically from the same `Out`
    value by a single renderer and golden-tested. It must include status, `pin_source`, citations, trust labels, the
    truncation notice and the next-call hint. Many clients show the model only `content`, and the MCP roadmap will
    unify the result shape.
- **Annotations:**
  - Every tool sets `readOnlyHint: true`, `idempotentHint: true`, `destructiveHint: false`.
  - `openWorldHint` is `true` only when a call may perform a live, allowlisted probe (`live: true`).
- **Description standard:** each tool description has four parts:
  1. One-line purpose.
  2. **Use when** …
  3. **Don't use when** … (name the better tool).
  4. One or two example calls.

  Include `inputExamples` where the SDK supports them.
- **Parameter naming:** unambiguous (`frc_season`, not `year`). Enumerations are expressed as JSON Schema `enum`.
- **Errors:**
  - Validation and domain errors are returned as tool results with `isError: true` (go-sdk ≥ 1.5 behavior), never as
    JSON-RPC errors.
  - The text states what was wrong, the valid values, and the exact next call. Example:
    `unknown library "phoenix"; valid: phoenix6, phoenix5, revlib, … — did you mean phoenix6 (2026: 26.1.0)? Call frc_context to pin versions.`
- **Progressive disclosure:** `frc_search` returns IDs and snippets. `frc_fetch` or a resource returns the full content.
  Large bodies are returned as `resource_link` items.
- **Budgets:**
  - `response_format: concise|detailed`, with `concise` as the default.
  - `max_tokens` defaults to 2,500 in concise mode. The hard cap is 20,000.
  - When output is cut, set `truncated: true` and `omitted: N`, and return a `next_cursor`.
- **Cursors are stateless:** a base64url encoding of `{query_hash, shard_set_digest, offset}`. After a shard swap, an old
  cursor returns an instructive `isError` telling the caller to re-run the search.
- **Stateless:** there is no session state. Context that must span calls travels in the opaque `pin` handle.
- **Cacheable:** set `ttlMs` and `cacheScope: "public"` on all list and template results. `resources/read` uses the
  shard's remaining freshness window as its TTL.
- **Formats:**
  - Markdown for prose and code, compact JSON for data.
  - **TOON is not used** (ADR-0004).
- **Not used:**
  - Sampling, Logging and Roots (deprecated in 2026-07-28).
  - Elicitation, except through MRTR in `frc_context` when several Gradle or RobotPy projects are found and no root was
    given.

## 2. Common types

```go
type Pin struct {
    Handle   string            `json:"pin,omitempty"        jsonschema:"opaque pin handle returned by frc_context"`
    Season   string            `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026 or 2027"`
    Channel  string            `json:"channel,omitempty"    jsonschema:"stable | beta | alpha"`
    Language string            `json:"language,omitempty"   jsonschema:"java | cpp | python | any"`
    Libs     map[string]string `json:"libs,omitempty"       jsonschema:"library id -> version overrides"`
}

type Citation struct {
    SourceURL   string `json:"source_url"`
    Library     string `json:"library"`
    Version     string `json:"version"`
    UpstreamRev string `json:"upstream_rev"`  // commit SHA, ETag, or release tag
    RetrievedAt string `json:"retrieved_at"`  // RFC 3339
    License     string `json:"license"`
    Trust       string `json:"trust"`         // official | vendor | community
    Suspect     bool   `json:"suspect,omitempty"`
}

type Envelope struct {
    Status     string   `json:"status"`          // ok | low_confidence | no_match | version_mismatch | syncing
    Confidence float64  `json:"confidence"`
    PinSource  string   `json:"pin_source"`      // arg | query | handle | project | default
    Freshness  string   `json:"freshness"`       // shard | live
    IndexAge   string   `json:"index_age"`       // e.g. "5h12m"
    IndexStale bool     `json:"index_stale,omitempty"` // manifest past expires_at
    Truncated  bool     `json:"truncated"`
    Omitted    int      `json:"omitted,omitempty"`
    NextCursor string   `json:"next_cursor,omitempty"`
    Degraded   []string `json:"degraded,omitempty"`    // e.g. "dense:potion-code-16M-v2", "rerank"
    Next       []string `json:"next,omitempty"`        // suggested next calls
}
```

## 3. Tools (v0.2: 9 tools, in this listing order)

### `frc_context`
Detects or declares the project's pin set.
- **In:**
  - `project_root?`: stdio mode only; defaults to the server's working directory.
  - `declare?`: a `Pin`.
  - `detect` (default `true`).
- **Detection:**
  - `build.gradle(.kts)` GradleRIO version and `vendordeps/*.json` for Java/C++.
  - `pyproject.toml` (the `robotpy` version and its component extras) for Python.
  - Multiple projects under one root trigger an MRTR input request.
- **Out:**
  - The `pin` handle and the resolved `Pin`.
  - The files that were detected.
  - **Compatibility warnings**, e.g. `PathplannerLib frcYear=2025 but WPILib 2026`, or a vendordep missing for an import.
  - **Upgrade hints:** installed vs. latest version for each library, with a citation.

### `frc_search`
- **In:**
  - `query` (required) and the `Pin` fields.
  - `kinds?`: `prose|code|api|release|rule|forum`. **`forum` is excluded unless listed**, or unless the router decides
    intent=troubleshoot.
  - `libraries?`, `k` (default 5, max 20), `response_format`, `max_tokens`, `cursor`.
- **Out:** the Envelope plus `hits[]`. Each hit has `id, title, heading_path, kind, library, version, language, snippet,
  score, citation`. Community snippets are fenced in `content` (`docs/security.md §2.1`).

### `frc_fetch` (renamed from `frc_read`)
- **In:** `id`, or a `uri` (`frc://…` or an allowlisted https URL); `sections?` (anchors); `max_tokens`; `cursor`.
- **Out:**
  - A Markdown body when the licence permits redistribution; otherwise a summary and a link.
  - The page outline, citation and related symbols.

### `frc_api`
- **In:** `symbol` (fully qualified name, simple name, `Class#method`, or `ns::Type`), the `Pin` fields,
  `include_members?`, `include_examples?`.
- **Out:** `matches[]`, each with:
  - `fqn`, `kind`, `signatures[]`, `summary`, `params`, `returns`;
  - `since`, `deprecated_in`, `removed_in`, `replacement`;
  - `other_seasons[]` and a citation.

  If the symbol exists only in another season, the status is `version_mismatch` and the response says where the
  symbol lives.

### `frc_verify_code` (Java M2; C++, Python and call shapes M4)
- **In:** `code`, or `path` (stdio mode only, under the project root: `.java`, `.cpp`/`.h`/…, `.py`); `language?`
  (default: from the path extension, the pin, else detected from the code); the `Pin` fields.
- **Out:**
  - `findings[]`, each with `line, col, symbol, severity, kind, message, fix?, citation`.
  - `coverage` per library: `partial (…)` names what was checked, `none (…)` says why a library was not. Silence
    without coverage is not approval.
  - Summary counts.
- **What is checked:**
  - Java: imports (single, wildcard, static), fully qualified names, members on receivers whose type is declared in the
    same file or imported (static calls), walking the pinned season's supertype hierarchy.
  - Java call shapes (M4): `new T(…)` and method calls on those receivers, when the literal arguments (`"x"`, `1`,
    `1.0`, `true`, `null`, `'c'`) fit no overload of the pinned season. Any other expression matches every parameter
    type, so a finding means the call cannot compile. Methods are checked only when the whole hierarchy resolves.
  - C++ (M4): qualified names in the indexed namespaces (`frc::`, `frc2::`, `wpi::`, `ctre::`, `rev::`, `photon::`, …)
    and `ns::Type::Member`, with nested types tried before members. WPILib's tables come from its Doxygen bundle;
    Phoenix 6, REVLib and PhotonLib tables come from the vendors' public headers (M3), which also record nested types
    as members (`rev::spark::SparkBase::IdleMode`, `ctre::phoenix6::CANBus::CANBusStatus`) and enumerators
    (`rev::REVLibError::kOk`, `ctre::phoenix6::signals::NeutralModeValue::Brake`).
  - Python (M4): `from x import y` (with aliases), `import x[.y] [as z]` attribute chains, and members on variables
    assigned from an imported class, including `self.` attributes.
- **Severity rule:**
  - **`error`** only when a symbol is absent from the pinned table **and** present in another season's table with a
    known pinned-season counterpart (`wrong_season`), or when no pinned overload accepts a call that another
    season's overload accepts.
  - `warning` for deprecated symbols, a wrong-season symbol without a known counterpart, a Python class imported from
    a module path the season does not use (packages re-export, so this is never an error), a call no overload of any
    season accepts with that argument count, and every vendor finding when the project's vendordep version differs
    from the indexed table.
  - `info` for symbols not in any table, which may be team code.

  False positives cost more than misses. Gate: 0 errors over 12 public 2026 team repositories (139k lines, Java and
  C++) pinned to their own season (`docs/benchmarks.md`).

### `frc_migrate` (implemented M4)
- **In:** exactly one of `symbol` (simple name, FQN, `Type#member` or `Type.member`), `code` (a whole Java, C++ or
  Python file) or `path` (stdio only); `from` and `to` as a season (`2026`) or a library version (`2026.2.2`,
  `2027.0.0-alpha-7`); `language?`; `include_members?` (symbol mode); `pin?` (supplies `from`). `to` defaults to the
  next indexed API season after `from`.
- **Out:** `mappings[]`, each with `from, to, from_season, to_season, language, library, kind, notes, confidence,
  source, rule_id?, citation?, line?`; `unresolved[]` with a reason and up to three pointers to target-season docs or
  release notes (`frc_fetch` ids); `not_found` (not a from-season symbol: team code or a typo); `already_in_target`.
  It produces mappings only and never rewrites code.
- **Sources, most authoritative first:**
  1. `curated`: `data/migrations/*.yaml`, one rule per upstream change (`kind` rename, move, removed, signature or
     behavior), each citing the PR, changelog or docs page it comes from. The index build validates every `from`
     against the from-season table and every `to` against the to-season table and fails on a mismatch, so a rule
     cannot drift from the tables it describes. Confidence `high` (`medium` for rules marked `verified: false`).
  2. `upstream`: the library's own deprecation note (`@deprecated Use X`), resolved to a target-season symbol when it
     names one uniquely. Confidence `medium`, or `low` when the note names no indexed symbol.
  3. `generated`: `apisym.Diff`'s same-name moves between consecutive seasons (e.g. `edu.wpi.first.math.geometry.Pose2d`
     → `org.wpilib.math.geometry.Pose2d`). Confidence `high` when the innermost package is unchanged, else `medium`.
     A member whose own row has no mapping is mapped through its type (`Type#m` → `Type'#m` when `Type'` declares or
     inherits `m`), reported as `generated` with the type rule's id and citation as the reason.
- **Code mode:** every API reference found by the same extractor as `frc_verify_code` is mapped once, with the line of
  its first use; the curated member rules of each used type are listed too, so constructor and signature changes
  (Phoenix 6 26.50 removed `TalonFX(int)` and `TalonFX(int, String)`) show up even though no reference names them.
- **Coverage:** a symbol whose library has no target-season table is `unresolved` with `no_table` set, which is a
  coverage gap, not a missing mapping. Measured over 12 public 2026 team repositories: 97.8% of API references map to
  2027, and 251 of the 254 unresolved ones belong to libraries without a 2027 table yet (`docs/benchmarks.md`).

### `frc_vendordep` (absorbs `check_compat`)
- **In:**
  - Either `name` (fuzzy: "rev", "phoenix6", "photon"), or `vendordeps[]` (raw JSON or name@version) together with
    `wpilib_version`, for set validation.
  - `frc_season`, `channel`, `live?`.
- **Out:**
  - Single mode: metadata (name, version, uuid, frcYear, jsonUrl, mavenUrls, conflictsWith), install instructions, a
    compatibility verdict against the pin, and the raw JSON as a `resource_link`.
  - Set mode: a compatibility matrix and fixes.
  - Data comes from the relational `vendordep` and `compat` tables, never from embeddings.
  - `jsonUrl` values come from the signed catalog only.

### `frc_whats_new` (implemented M3)
- **In:**
  - `library`: an id or alias, or `all`;
  - `since`: a version of that library, or a date `YYYY-MM-DD`;
  - `frc_season` or `pin`;
  - `stable_only`;
  - `limit` (default 10, at most 50).
  - `live?` is not implemented yet. Freshness comes from the index cron.
- **Out:** release entries newest first. Each has:
  - library, version, season, channel, date and title;
  - a `breaking` hint when the notes mention removed or renamed APIs;
  - the first lines of the notes;
  - the `chunk_id` of the full notes (for `frc_fetch`);
  - a citation.

  This is an exact fact lookup over the `release` table, in date order with no similarity ranking. With no match it
  returns `no_match` plus the libraries that have release data.
- **Data:** GitHub releases of each project's own repository (`github-releases` adapter; seasons ≥ 2025):
  - allwpilib;
  - Phoenix-Releases;
  - REV-Software-Binaries (`revlib-` tags);
  - photonvision, pathplanner, Choreo, AdvantageKit and YAGSL.

  Seasons come from each version, including CTRE's `26.70.x`, which is the 2027 alpha. Release notes are often
  generated from pull-request titles, so they are sanitized at ingest and rendered as a quote block. Notes from
  non-vendor repositories, if ever added, are `trust: community` and fenced.

### `frc_hardware` (implemented M3)
- **In:**
  - `parts[]`: names or aliases such as "kraken x60 foc", "vortex", "neo550", "mk4i", "maxswerve", "cancoder",
    "through bore" or "pigeon 2". A unique prefix is accepted, then a unique suffix (so a name without the vendor,
    "mk4i", finds `sdsmk4i`);
  - or `category`: `motor`, `encoder`, `imu` or `swerve_module`;
  - `frc_season` or `pin`.
- **Out:** rows from the `hw_spec` table, grouped per part in the order asked:
  - one column per (source, season) with unit-suffixed fields: motors `stall_torque_nm`, `free_speed_rpm`,
    `stall_current_a`, `free_current_a`, `nominal_voltage_v` first, then whatever else a source gives
    (`kv_rpm_per_v`, `peak_power_w`, `motor_weight_lb`, `steer_ratio`, `wheel_diameter_in`, `quadrature_cpr`,
    `absolute_resolution_bits`, `yaw_drift_no_motion_deg_per_hour`, …); ratios are dimensionless;
  - `frc_season: all` for sources that do not depend on the season; they are returned for every season next to
    that season's own rows;
  - the source's own provenance note (e.g. ReCalc's data source, or what a curated number means);
  - a citation (`library` is `wpilib` for DCMotor rows, otherwise the source label);
  - the WPILib `DCMotor` factory in Java, C++ and Python, resolved from the indexed API tables of that season (for
    example 2026 `frc::DCMotor::KrakenX60FOC(numMotors)`, 2027 `wpi::math::DCMotor::…`).

  **Every source is returned labeled and never merged.** WPILib `DCMotor` and ReCalc (CTRE dyno) values for NEO
  differ by about 60% in stall torque.
- **Sources** (`docs/sources.md` §6):
  - `wpilib-dcmotor`: WPILib's simulation constants parsed from `DCMotor.java` at the release tag, 19 motors per
    season;
  - `recalc`: ReCalc's motor table at a pinned commit (MIT), 21 FRC motors, mostly vendor dyno data;
  - `rev-docs`, `wcp-docs`: REV and WCP motor spec pages (GitBook `.md`); no docs license, so these rows are in the
    `hardware-restricted` shard, which is not published;
  - `sds`, `rev`, `wcp`, `ctre`, `redux`: curated rows from `data/hardware/*.yaml`, each number copied from the
    cited vendor page (swerve modules, encoders, IMUs, the CTRE Minion's stated specs).
- Swerve drive ratios are not included: the vendors publish them only as images.

### Optional toolset (HTTP only): `--toolset openai`
Registers literal `search` and `fetch` tools with the output shapes expected by OpenAI connectors and deep research.
They are thin adapters over `frc_search` and `frc_fetch`. The default listing is unchanged.

## 4. Resources (templates)

| URI template | Content |
|---|---|
| `frc://docs/{library}/{version}/{path}` | Doc page (Markdown) |
| `frc://api/{library}/{version}/{lang}/{symbol}` | Symbol card |
| `frc://vendordep/{name}/{season}` | Vendordep JSON (from the catalog) |
| `frc://changelog/{library}/{version}` | Release notes |
| `frc://specs/{part}` | Hardware spec rows (dual-source) |
| `frc://rules/{season}/{rule_id}` | Game Manual rule and its change history (post-M5) |
| `frc://index/manifest` | Shard manifest: serial, expiry, ages, upstream revisions, symbol coverage |

Completions are provided for `library`, `version`, `frc_season`, `name`, `part` and `rule_id`.

## 5. Prompts (workflows)

| Prompt | Args | Chains |
|---|---|---|
| `frc-new-subsystem` | mechanism, motor/controller, sensors, control mode | `frc_context → frc_api → frc_search(kinds=code) → frc_verify_code` |
| `frc-port-season` | from, to | `frc_context → frc_whats_new → frc_migrate → frc_verify_code` |
| `frc-swerve-setup` | modules, motors, encoders, gyro, approach (CTRE generator / YAGSL / AdvantageKit template) | `frc_vendordep → frc_hardware → frc_search → frc_api` |
| `frc-auto-routine` | tool (PathPlanner / Choreo), drivetrain | `frc_vendordep → frc_search → frc_api` |
| `frc-debug-vendordep` | error text | `frc_context → frc_vendordep → frc_whats_new` |

Deferred: `frc_scaffold`, a deterministic composite tool, revisited after M4 once `frc_api` and `frc_vendordep` are
stable.

## 6. Transports & profiles

- **stdio** (local-lite default):
  - Launched by the client. Logs go to stderr only.
  - The first run serves `tools/list` immediately. If no shards are present, tools return `status: syncing` with
    progress while the background sync runs.
- **Streamable HTTP** (`--transport http`; the hosted-full profile, ADR-0005):
  - A stateless handler that implements `server/discover` and honors the `Mcp-Method`/`Mcp-Name` headers.
  - Origin validation, and a bearer token (OAuth 2.1 + CIMD later).
  - Filesystem arguments are disabled.
  - **Serving is redistribution:** shards holding any `LicenseRef-*` content (chunks, symbols, release facts or hardware
    rows: today the CTRE and REV docs/APIs/release notes, and the forum shard) are not loaded unless the operator
    passes `--include-unlicensed`, which is only for when the vendors have granted permission (`docs/sources.md`
    §0.2). stdio serves the user's own local index and loads everything.
  - An optional server card at `/.well-known/mcp/server-card.json` (a draft extension, tracked).

## 7. Example client config (pin the version)

```json
{ "mcpServers": { "frc": { "command": "frc-mcp", "args": ["serve"] } } }
```

Teams should commit MCP configs only with a pinned binary version or path, never `@latest` (`docs/security.md §2.6`).
