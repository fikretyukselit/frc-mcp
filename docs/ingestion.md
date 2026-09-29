# Ingestion & change detection

Status: Draft v0.2 (2026-09-29). Endpoint-level details: `docs/sources.md`. Raw findings: `docs/research/`.

## 1. Goals

- Stable-channel changes visible in a published shard ≤ 24 h; alpha/beta and kickoff-week changes ≤ 6 h.
- Minimal upstream load: the steady state should be mostly `304 Not Modified`.
- Deterministic, replayable, testable: every adapter runs against recorded cassettes in CI.

## 2. Change-detection ladder

For each source the fetcher uses the **cheapest reliable signal first** and escalates only when it fires:

| Rung | Signal | Used for | Notes |
|---|---|---|---|
| 1 | Push (WebSub / webhook) | YouTube feeds (WebSub hub `pubsubhubbub.appspot.com`), TBA webhooks | Only for a hosted ingestion service with a public callback; CI cron ignores this rung |
| 2 | Monotonic version marker | PyPI `X-PyPI-Last-Serial`; vendordep `version`; Maven `<lastUpdated>`; GitHub release `tagName`/`publishedAt`; git commit/tree SHA of a docs `source/` dir | Compare marker only; no body processing |
| 3 | Conditional GET | Everything HTTP: `If-None-Match` (ETag) → `If-Modified-Since` (Last-Modified) | 304 = done. **Authenticated** GitHub 304s do not consume the primary rate limit; unauthenticated ones do — always authenticate in CI |
| 4 | Byte hash | Sources without validators (Chief Delphi RSS — no ETag/Last-Modified behind Cloudflare; FIRST blog listing; vendor spec pages) | `sha256(raw)` |
| 5 | Normalized-content hash | All documents after normalization | `sha256(normalize(text))`; **only this triggers re-chunk/re-embed**, which filters cosmetic rebuilds (timestamps, build IDs, nav) |

Hints we record but never trust alone: sitemap `lastmod` (RTD sitemaps list versions, not pages), feed `<updated>`,
`article:modified_time`.

### 2.1 Per-kind strategy

- **GitHub releases (≈20 repos):** one **GraphQL batch** per cycle (aliased `repository { releases(first: 5, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { tagName isPrerelease publishedAt updatedAt url } } }` — ~1 point for 50 repos). Fallback without a token: `releases.atom` with `If-None-Match` (verified: GitHub returns weak ETag + 304). No WebSub hub on GitHub Atom — poll.
- **Docs in GitHub (frc-docs, Phoenix6-Documentation, photonvision docs, YAGSL-gitbook, Choreo `docs/`):** REST `GET /repos/{o}/{r}/commits?path=source&per_page=1` with ETag → if SHA changed, `git fetch --depth=1` + diff the tree to re-process only changed files. Prefer RST/MD source over rendered HTML; for RST, a light heading/directive splitter (no full RST parser exists in Go) then render via Sphinx HTML only where directives matter (tabs, code-tabs).
- **Read the Docs rendered pages** (fallback): obey RTD automated-access policy (< 4 req/s, identifying UA, reuse ETags); prefer RTD's Markdown-for-bots / `_sources/*.rst.txt` where available.
- **GitBook sites (REV, YAGSL, Redux, Limelight, ThriftyBot):** `llms.txt` + per-page `.md` with ETag; fall back to HTML→Markdown.
- **Javadoc / Doxygen:** release-triggered only (no polling) — a new allwpilib / vendor release tag enqueues an API rebuild for that version.
- **Vendordep JSON:** ETag GET; compare `version`; validate schema and `frcYear`; also walk `wpilibsuite/vendor-json-repo` `*_metadata.json` (raw, `max-age=300` ⇒ floor 5 min) — this is the canonical catalog.
- **Maven `maven-metadata.xml`:** ETag/Last-Modified; compare `<versioning><latest>` / `<lastUpdated>`. Never watch `development` repos (noise).
- **PyPI:** JSON API, ETag + `X-PyPI-Last-Serial`; `max-age=900` ⇒ floor 15 min.
- **RSS/Atom (Chief Delphi `latest.rss` + `posts.rss`; YouTube channel feeds; WPILib blog):** honor `<ttl>`, `skipHours`, `skipDays` as *minimum* intervals, `Cache-Control`, `Retry-After`; dedupe by GUID + normalized item hash; Chief Delphi JSON endpoints are Cloudflare-blocked → RSS only, and its robots.txt disallows the category and topic feeds (`/c/*.rss`, `/t/*/*.rss`). Chief Delphi sends no validators, so `min_interval` in `data/sources.yaml` is the politeness floor: within it the fetcher serves the cached copy without a request.
- **PDF (Game Manual, Team Updates):** `HEAD` for ETag/Last-Modified on the season-materials page links; on change, extract with go-pdfium (Wasm), split by rule ID, diff rule-by-rule and emit a change log chunk ("R501 changed in Team Update 07").

### 2.2 Implemented adapters (`data/sources.yaml` `adapter:`)

| Adapter | Package | Input | Notes |
|---|---|---|---|
| `sphinx-htmlzip` | `internal/ingest/source/sphinx` | Read the Docs htmlzip | `sphinx_rtd_theme` and Furo; sphinx-design code tabs become per-language chunks |
| `javadoc-zip` | `internal/ingest/source/javadoc` | Javadoc zip (Maven `documentation` artifact) | symbol table + one API chunk per class |
| `vendordep-catalog` | `internal/ingest/source/vendordeps` | GitHub contents API listing of `vendor-json-repo/<year>` | fact rows + one install chunk per library |
| `github-markdown` | `internal/ingest/source/repomd` | git tree API URL of a pinned ref | fetches `include/**/*.md(x)` from raw.githubusercontent.com; `url_style` (`html`, `dir`, `plain`) and `lowercase` map files to page URLs; a Docusaurus `slug` wins |
| `gitbook-llms` | `internal/ingest/source/gitbook` | a GitBook site's `llms.txt` | same-host `.md` pages under `include`, ≤ 3.3 req/s, conditional GET |
| `github-releases` | `internal/ingest/source/ghreleases` | GitHub REST releases listing | release fact rows + release chunks; `include` = tag prefix; seasons ≥ 2025 via `facts.SeasonFor` |
| `doxygen-zip` | `internal/ingest/source/doxygen` | Doxygen HTML zip (WPILib Maven `wpilibc/documentation`) | class pages → symbols + API chunks; bases from "inherited from" headers; deprecations from `deprecated.html`; `include` = namespace roots (drops vendored fmt/Eigen/LLVM/Sleipnir) |
| `discourse-rss` | `internal/ingest/source/discourse` | a Discourse site's `latest.rss` (+ `posts_url`: `posts.rss`) | one `forum` chunk per post, trust `community`; `categories` allowlist (replies kept only for listed topics in allowed categories); dedupe by GUID; season = the post's year; cited by the canonical permalink (`/t/<slug>/<topic>[/<n>]`, `?page=N` dropped); HTML → Markdown through the Sphinx converter after a DOM pass that drops hidden elements, quotes of other posts and link previews; `sources.Validate` enforces trust, a `LicenseRef-*-UserContent` license and site-level feeds only |
| `pypi-wheel` | `internal/ingest/source/pypi` + `pystub` | version-pinned PyPI JSON | picks one wheel (pure-Python, else manylinux x86_64; sha256-checked); `.pyi` stubs or typed `.py`; private `_segments` dropped and package `__init__` re-exports resolved, so FQNs are the import paths users write (`phoenix6.hardware.TalonFX`, `commands2.Command`) |
| `wpilib-dcmotor` | `internal/ingest/source/dcmotor` | `DCMotor.java` at a release tag | one `hw_spec` row per `getX(int numMotors)` factory (source `wpilib-dcmotor`) |
| `recalc-motors` | `internal/ingest/source/recalc` | ReCalc `Motor.ts` at a pinned commit | one `hw_spec` row per FRC motor in `ALL_MOTORS` (source `recalc`, season `all`); units checked, nothing derived |
| `gitbook-spec-table` | `internal/ingest/source/specpage` | one GitBook page's `.md` rendition | the page's parameter table → `hw_spec` rows labeled by the entry's `hardware: {source, part, name}`; a trapezoidal and an FOC tab become the part and its `-foc` variant; listed parameters only, units checked |

Curated hardware rows (`data/hardware/*.yaml`, package `internal/hwdata`) are loaded by `index run --hardware`
(default `data/hardware`) like the migration rules: validated, then added to their shard when that shard is built
in the run; a (part, source, season) produced twice fails the build.

Both Markdown adapters share `internal/ingest/source/markdown`. It normalizes five dialects into CommonMark before
sectioning:
- MyST: `:::{note}`, `{directive}` fences, roles, `{eval-rst}` islands with `tab-set-code` and `code-block`;
- Docusaurus/MDX: front matter, import/export lines, `<Tabs>`, admonitions, `{#id}` anchors;
- Writerside: `<tabs>`/`<tab>`, `{style=…}`;
- MkDocs Material: indented `===` tabs and `!!!` admonitions;
- GitBook: `{% hint %}`, `{% tabs %}`, `{% embed %}`, `{% content-ref %}`, `<figure>`.

It then applies the Sphinx chunking rules (shared in `internal/ingest/chunking`): heading-path sections, one variant
per code language, and fences are never split. A normalizer fuzz target guards against crashes, and a leak check in
the tests fails if dialect syntax survives into a chunk body.

## 3. Scheduler — the decision model

**Phasing:** M2–M4 run a **fixed-interval scheduler** (interval per source from `sources.yaml`, scaled by the season
multiplier, bounded by floors). The Bayesian scheduler below lands in M5 and must keep the fixed path as fallback
(feature flag), so ingestion never blocks on it.

Each source `s` is modeled as a Poisson change process with unknown rate `λ_s` (changes/day).

**Posterior:** `λ_s ~ Gamma(α_s, β_s)`.

- **Prior** (from `data/sources.yaml` `prior_rate` r₀ and `data/season.yaml` multiplier m(t)):
  `β₀ = 7` (days of pseudo-observation), `α₀ = r₀ · m(t) · β₀`.
- **Update after a poll covering Δt days:** `β ← β + Δt`; `α ← α + c`, where `c` = exact number of upstream changes
  when timestamps are available (Atom `updated`, `publishedAt`, `Last-Modified`, commit list), otherwise `c = 1` if
  the normalized hash changed else 0. (With only a changed/unchanged bit, the Cho & Garcia-Molina bias-corrected
  estimator `λ̂ = −ln((n − X + 0.5)/(n + 0.5)) / I` is used to seed/validate α/β offline.)
- **Forgetting (regime shifts, e.g. kickoff):** weekly `α ← 1 + γ(α − 1)`, `β ← γβ`, `γ = 0.9`.
- **Next interval (Thompson sampling):** draw `λ̃ ~ Gamma(α, β)`;
  `I = clamp( −ln(1 − p) / λ̃ , I_min(s), I_max(s) )`
  where `p` = acceptable probability of missing ≥ 1 change within the interval (default 0.3; 0.15 for `critical`
  sources). Sampling gives exploration for free — no separate bandit.
- **Floors:** `I_min(s) = max(policy floor, Cache-Control max-age, feed ttl, Retry-After)`.
- **Budget arbitration:** when due work exceeds a host's token bucket, order by
  `priority = w_s · (1 − e^(−λ̂_s · age_s))` (importance × probability of being stale); `w_s` from sources.yaml
  (`critical` = 3, `high` = 2, `normal` = 1, `low` = 0.5).

**Season calendar multipliers** (`data/season.yaml`, data not code):

| Window | m(t) |
|---|---|
| Kickoff week (first Saturday of January ± 3 days) | 10 |
| Build season (to ~week 8) | 3 |
| Competition season (Mar–Apr) + Championship | 2 |
| Off-season (May–Aug) | 0.5 |
| Beta / alpha season (Sep–Dec; e.g. 2027 alphas) | 1.5 (alpha-channel sources: 3) |

**Execution model:** CI runs `frc-mcp index plan` every 30 min (cheap: reads state, emits due list), then
`frc-mcp index run` processes due sources. State (`validators`, `α/β`, hashes, last-seen markers) is persisted in a
small SQLite `state.db` cached between runs (actions/cache + a copy attached to each index release for recovery).
Rebuild of a shard is triggered only when ≥ 1 of its documents changed `norm_hash`.

## 3a. Sanitization & trust (before normalization)

Every fetched item passes `internal/ingest/sanitize` (`docs/security.md §2.1`): strip Unicode tag characters,
bidi/zero-width controls, HTML comments and hidden elements; NFC; per-item size cap; assign `trust` from the source's
registry entry (`official|vendor|community`; release-note bodies from non-vendor repos → `community`); run suspect
detection and set `suspect`. Sanitization is deterministic and golden/fuzz-tested.

## 4. Fetcher contract

- `http.Client` with per-request context deadline (default 20 s), `MaxIdleConnsPerHost` tuned, HTTP/2.
- Per-host `rate.Limiter` (defaults: GitHub API 10 rps, raw.githubusercontent 5 rps, RTD 2 rps, everything else 1 rps)
  + per-host concurrency semaphore (2).
- robots.txt fetched/cached per host (24 h); disallowed → source marked `blocked`, alert.
- Retries: 429/502/503/504 and network errors; exponential backoff with full jitter, honoring `Retry-After`; max 4.
- Body size cap per kind (HTML 5 MB, JSON 2 MB, PDF 50 MB); content-type checks.
- `User-Agent: frc-mcp-indexer/<ver> (+https://github.com/fikretyukselit/frc-mcp)`.
- All traffic goes through `internal/netguard`: https only, host allowlist derived from `sources.yaml`, redirects
  re-checked, private/loopback/link-local IPs blocked at dial time.
- CI job isolation: crawl/build jobs hold read-only tokens only; signing/publishing happens in a separate job.
- Records per response: status, validators, `max-age`, bytes, latency — feeds `freshness.json` and alerting.

## 5. Probing unverified endpoints

`make probe` (nightly) issues HEAD/GET to every `verified: false` URL and every templated next-season URL
(e.g. `REVLib-2027.json`, `Phoenix6-frc2027-latest.json`, `ChoreoLib2027.json`). Result is written to
`data/probe-report.json`; a green probe opens a bot PR flipping `verified: true` (human-reviewed). 404/410 on a
verified URL opens an issue.

## 6. Testing

- go-vcr v4 cassettes per adapter (Authorization stripped; matcher includes `If-None-Match`/`If-Modified-Since` so 304
  flows are covered).
- `testing/synctest` + `httptest.NewTestServer` (Go 1.27) for scheduler, backoff, and rate limiting with fake time.
- Fuzz: feed parsing, normalizer idempotence, vendordep parser, version comparators (SemVer-ish + FRC `2027.0.0-alpha-7`, Phoenix `26.50.0-alpha-1`).
- Golden files for normalized Markdown and chunk output per adapter.
