# MCP surface (contract)

Status: Draft v0.1 (2026-09-28). Target spec: **MCP 2026-07-28** via `modelcontextprotocol/go-sdk` v1.8.x.

## 1. Design rules

- **Few, workflow-shaped tools** (≤ 8). Namespaced `frc_`. Deterministic listing order. Stable names across releases.
- **Typed I/O:** every tool registered with `mcp.AddTool[In, Out]`; input/output JSON Schemas inferred from Go structs
  (`jsonschema` tags carry descriptions). `structuredContent` is the source of truth; `content` is a compact Markdown
  rendering for text-only clients.
- **Annotations:** `readOnlyHint: true`, `idempotentHint: true`, `destructiveHint: false`; `openWorldHint: false`
  except `frc_whats_new`/`frc_vendordep` when `live=true`.
- **Progressive disclosure:** search returns IDs + snippets; `frc_read` / resources return full content on demand;
  `resource_link` content items instead of inlined pages.
- **Token budgets:** `response_format: concise|detailed` + `max_tokens`; cursor pagination.
- **Instructive errors:** errors say what to call next, e.g.
  `unknown library "phoenix"; did you mean "phoenix6" (2026: 26.1.0) or "phoenix5"? Call frc_context to pin versions.`
- **Stateless:** no session state; cross-call context flows through the opaque `pin` handle.
- **Cacheable lists/reads:** `ttlMs` + `cacheScope: "public"` on `tools/list`, `prompts/list`, `resources/list`,
  `resources/templates/list`; `resources/read` TTL = shard's remaining freshness window.
- **Not used:** Sampling, Logging, Roots (deprecated in 2026-07-28). Elicitation via MRTR only in `frc_context` when
  the project root is ambiguous (multiple Gradle projects).

## 2. Common types

```go
type Pin struct {
    Handle   string            `json:"pin,omitempty"      jsonschema:"opaque pin handle from frc_context"`
    Season   string            `json:"season,omitempty"   jsonschema:"FRC season, e.g. 2026 or 2027"`
    Channel  string            `json:"channel,omitempty"  jsonschema:"stable | beta | alpha"`
    Language string            `json:"language,omitempty" jsonschema:"java | cpp | python | any"`
    Libs     map[string]string `json:"libs,omitempty"     jsonschema:"library -> version overrides"`
}

type Citation struct {
    SourceURL   string `json:"source_url"`
    Library     string `json:"library"`
    Version     string `json:"version"`
    UpstreamRev string `json:"upstream_rev"`   // commit SHA, ETag, or release tag
    RetrievedAt string `json:"retrieved_at"`   // RFC 3339
    License     string `json:"license"`
}

type Envelope struct {
    Status     string   `json:"status"`        // ok | low_confidence | no_match | version_mismatch
    Confidence float64  `json:"confidence"`
    PinSource  string   `json:"pin_source"`    // arg | query | handle | project | default
    Freshness  string   `json:"freshness"`     // shard | live
    IndexAge   string   `json:"index_age"`     // e.g. "5h12m"
    Next       []string `json:"next,omitempty"`// suggested next calls
}
```

## 3. Tools

### `frc_context`
Detect or declare the pin set.
- In: `project_root?` (string, default: server cwd), `declare?` (Pin), `detect: bool` (default true).
- Out: `pin` handle, resolved Pin, detected files, warnings (e.g. `vendordep PathplannerLib frcYear=2025 but WPILib 2026`),
  and **upgrade hints** (installed vs latest per library, with citation).

### `frc_search`
- In: `query` (required), Pin fields, `kinds?` (prose|code|api|release|rule|forum), `libraries?`, `k` (default 5, max 20),
  `response_format` (concise|detailed), `max_tokens`, `cursor`, `rerank?` (only if built with `rerank`).
- Out: Envelope + `hits[]{id, title, heading_path, kind, library, version, language, snippet, score, citation}` + `next_cursor`.

### `frc_read`
- In: `id` or `uri` (`frc://docs/...`), `sections?` (anchors), `max_tokens`.
- Out: Markdown body (license permitting; otherwise summary + link), outline, citation, related symbols.

### `frc_api`
- In: `symbol` (FQN, simple name, or `Class#method`), Pin fields, `include_members?`, `include_examples?`.
- Out: `matches[]{fqn, kind, signature(s), summary, params, returns, since, deprecated_in, removed_in, replacement,
  other_versions[], citation}`. If the symbol exists only in another season → `status: version_mismatch` with where it lives.

### `frc_verify_code`
- In: `code` or `path` (file under project root), `language?` (inferred), Pin fields.
- Out: `findings[]{line, col, symbol, severity (error|warning|info), kind (unknown|deprecated|removed|wrong_season|
  wrong_language|vendordep_missing), message, fix?, citation}`, summary counts.
- Scope: static and conservative — resolves imports, qualified names, constructor and method names against the pinned
  symbol tables; never claims a symbol is wrong without a table entry proving it (false positives are worse than misses).

### `frc_migrate`
- In: `symbol` or `code`, `from` (Pin or version), `to` (Pin or version).
- Out: `mappings[]{from, to, notes, confidence, citation}` from curated rules (`data/migrations/*.yaml`), plus
  unresolved items with pointers to changelog chunks.

### `frc_vendordep`
- In: `name` (fuzzy: "rev", "phoenix6", "photon", "pathplanner") or `url`, `season`, `channel`, `live?`.
- Out: vendordep metadata (name, version, uuid, frcYear, jsonUrl, mavenUrls, conflictsWith), install instructions
  (`./gradlew vendordep --url=…` / VS Code), compatibility verdict vs pin, raw JSON as `resource_link`.

### `frc_whats_new`
- In: `library` (or `all`), `since` (version or date), Pin fields, `live?`.
- Out: ordered release/changelog/known-issue entries with breaking-change flags and citations.

## 4. Resources (templates)

| URI template | Content |
|---|---|
| `frc://docs/{library}/{version}/{path}` | Doc page (Markdown) |
| `frc://api/{library}/{version}/{lang}/{symbol}` | Symbol card |
| `frc://vendordep/{name}/{season}` | Vendordep JSON |
| `frc://changelog/{library}/{version}` | Release notes |
| `frc://rules/{season}/{rule_id}` | Game Manual rule text + change history |
| `frc://index/manifest` | Shard manifest, ages, upstream revisions |

Completions are provided for `library`, `version`, `season`, `name`, `rule_id`.

## 5. Prompts (workflows)

| Prompt | Args | Chains |
|---|---|---|
| `frc-new-subsystem` | mechanism, motor/controller, sensors, control mode | `frc_context → frc_api → frc_search(kind=code) → frc_verify_code` |
| `frc-port-season` | from, to | `frc_context → frc_whats_new → frc_migrate → frc_verify_code` |
| `frc-swerve-setup` | modules, motors, encoders, gyro, library (CTRE generator / YAGSL / AdvantageKit template) | `frc_vendordep → frc_search → frc_api` |
| `frc-auto-routine` | tool (PathPlanner / Choreo), drivetrain | `frc_vendordep → frc_search → frc_api` |
| `frc-debug-vendordep` | error text | `frc_context → frc_vendordep → frc_whats_new` |

## 6. Transports

- **stdio** (default): launched by the client; logs to stderr only.
- **Streamable HTTP** (`--transport http --addr 127.0.0.1:7424`): stateless handler, implements `server/discover`,
  honors `Mcp-Method`/`Mcp-Name` headers, Origin validation, optional bearer token; suitable for a team server behind any
  load balancer. Optional server card at `/.well-known/mcp/server-card.json` (draft extension — tracked, not required).

## 7. Example client config

```json
{ "mcpServers": { "frc": { "command": "frc-mcp", "args": ["serve"] } } }
```
