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

### `frc_verify_code`
- **In:** `code`, or `path` (stdio mode only, under the project root); `language?`; the `Pin` fields.
- **Out:**
  - `findings[]`, each with `line, col, symbol, severity, kind, message, fix?, citation`.
  - `coverage` per `(library, language)`: `full|partial|none`, stating which checks ran. Silence without coverage is not
    approval.
  - Summary counts.
- **Status:** implemented for Java in M2. False-positive gate: 0 errors in 128k LOC of public 2026 team code
  (`docs/benchmarks.md`). Member checks walk the pinned season's supertype hierarchy; a hierarchy that cannot be
  resolved yields no finding.
- **MVP scope (M2):**
  - Java: imports (including wildcard and static), qualified type names, and `new X(`.
  - C++ (M3): `#include` and `ns::Type`.
  - Python (M3): `from x import y`.
  - Member calls (M4): Java only, and only when the receiver's declared type is visible in the same file.
- **Severity rule:**
  - **`error`** only when a symbol is absent from the pinned table **and** present in another season's table
    (`wrong_season`), or is marked `removed_in` ≤ the pinned version.
  - `warning` for symbols marked deprecated in the pinned version.
  - `info` for symbols not in any table, which may be team code.

  False positives cost more than misses.

### `frc_migrate`
- **In:** `symbol` or `code`; `from` and `to` (a `Pin` or a version string).
- **Out:** `mappings[]`, each with `from, to, notes, confidence, source (curated|generated), citation`, plus unresolved
  items with pointers to changelog chunks. It produces mappings only and never rewrites code.

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

### `frc_whats_new`
- **In:** `library` (or `all`), `since` (a version or a date), the `Pin` fields, `live?`.
- **Out:** release, changelog and known-issue entries in order, with breaking-change flags and citations. Release-note
  bodies from non-vendor repositories are `trust: community` and fenced.

### `frc_hardware` (spec'd v0.2, implemented M3)
- **In:** `parts[]`, or `category` (`motor|controller|encoder|imu|swerve_module|sensor`); `fields?`; `source?`.
- **Out:** rows from the `hw_spec` table. **Every source is returned labeled and never merged.** For example, WPILib
  `DCMotor` and CTRE-dyno/ReCalc values for NEO differ by about 60% in stall torque. Each row also gives which WPILib
  `DCMotor` factory to use in simulation.

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
  - An optional server card at `/.well-known/mcp/server-card.json` (a draft extension, tracked).

## 7. Example client config (pin the version)

```json
{ "mcpServers": { "frc": { "command": "frc-mcp", "args": ["serve"] } } }
```

Teams should commit MCP configs only with a pinned binary version or path, never `@latest` (`docs/security.md §2.6`).
