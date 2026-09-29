# Benchmarks

## Agent-level eval (M4 exit criterion, 2026-09-29)

**Question:** does the same coding agent write robot code that compiles more often with frc-mcp than without it?

**Setup** (`cmd/agenteval`, `eval/tasks/`):
- 26 tasks: 20 for 2027 (WPILib 2027.0.0-alpha-7 and the vendors' 2027 alphas) and 6 control tasks for 2026. Prompts
  are what a student would ask and name the season and library versions, never the API under test (a test enforces
  this). Every task has a reference solution that compiles and that `frc_verify_code` finds clean.
- The agent is the claude CLI, headless, in an empty GradleRIO project (build file and vendordeps only), with file
  tools only: no shell and no web in either condition. The two conditions differ only in the frc-mcp server being
  attached. Every run records the MCP servers of the session, and a baseline run that sees one aborts.
- The result is compiled with `javac --release 25` (2027) or `17` (2026) against the exact Maven artifacts GradleRIO's
  `WPIJavaDepsExtension` puts on the classpath, plus the task's vendordeps and WPILib's annotation processors and javac
  plugin (so an ignored `@NoDiscard` result fails the build as in Gradle). It does not run Gradle, tests or simulation.
- Results: `eval/results/2026-09-29-*.jsonl`, one line per run with the agent's tool calls, turns, compile output and
  the verifier's error count. Reproduce with `agenteval run --model sonnet --trials 3 --out FILE` and `agenteval report
  FILE`.

| Model | Trials | Compile pass with frc-mcp | Without | Paired difference over 26 tasks (95% bootstrap) | 2027 only | 2026 control |
|---|---|---|---|---|---|---|
| Sonnet (claude-sonnet-5-5) | 3 | **98.7%** (77/78) | 38.5% (30/78) | **+60.3 pp** (+43.6 to +76.9) | 98.3% vs 20.0% | 100% vs 100% |
| Haiku (claude-haiku-4-5) | 1 | 34.6% (9/26) | 19.2% (5/26) | **+15.4 pp** (+3.8 to +30.8) | 15.0% vs 0.0% | 100% vs 83.3% |

**Reading the numbers:**
- **The gain is all in the season the model does not know.** On 2026 both conditions pass (the model's training data
  covers it); on 2027 Sonnet without frc-mcp passes 20%, and with it 98%. Without the server it writes
  `edu.wpi.first.*` imports, `ChassisSpeeds`, `SmartDashboard` and `new TalonFX(id, "canivore")` in a 2027 project.
- **Exit criterion (≥ +20 pp): met for Sonnet, not for Haiku.** Haiku calls frc_api and frc_search (6 calls per run)
  but never frc_verify_code, and 17 of its 20 2027 attempts fail to compile. Sonnet verified its code in 39 of 60
  2027 runs; its one failure (`PWMSparkMax.set`, `setThrottle` in 2027) was in a run that did not.
- **The verifier on agent code:** it flagged 32 of the 49 failed Sonnet compiles and 30 of the 38 failed Haiku
  compiles, with **0 errors on code that compiled** (208 runs).
- **Cost:** with frc-mcp Sonnet takes 12 turns instead of 7 and about 2.2× the cost of the baseline (USD 0.13 vs 0.06 per task
  at API prices).

## Migration and verification on real team code (M4, 2026-09-29)

12 public 2026 team repositories (Java, plus 6328's C++ tools), shallow-cloned from GitHub: 6328 Mechanical Advantage,
StuyPulse, Spartronics 4915, 5427, 4533, 102, 3082, Salem, 166, hammerheads5000, Earl of March 7476 and Oakville
Dynamics. 1,001 source files, 139,402 lines.

| Measurement | Result |
|---|---|
| `frc-mcp verify`, each repository pinned to its own season (2026), Java + C++ | **0 errors** · 19 warnings (0.000 errors/kLOC; gate ≤ 1.0) |
| `frc-mcp migrate --to 2027`, API references mapped | **11,535 of 11,789 (97.8%)** |
| … of the 254 unresolved | 251 belong to libraries with no 2027 table yet (PathPlannerLib, YAGSL); 3 have no cited mapping |
| Curated rules | 238 rules, 366 per language (WPILib, Phoenix 6, REVLib, PhotonLib, AdvantageKit, ChoreoLib), each citing its upstream change |

How the mapping rate was reached, and what it says about each source of mappings:

1. **Generated moves alone:** 80% (8,106 of 10,133). Same-name package moves (`edu.wpi.first.*` → `org.wpilib.*`,
   `frc::` → `wpi::`) are most of a 2027 port, but the rest is renames the diff cannot guess.
2. **Resolution fix:** the index's case-insensitive simple-name fallback made `Command` resolve to
   `EventMarker#command`; migration lookups are now exact and case-sensitive.
3. **Members through their type** (`CommandXboxController#a` → `CommandNiDsXboxController#a`) and a separate
   "no target table" reason: 92.7%.
4. **120 curated rules** (kinematics Speeds → Velocities, Commands v2 package, SmartDashboard/SendableChooser →
   Telemetry/Tunables, Phoenix 6 26.50 constructors and renames, AdvantageKit's LoggedNetworkChooser, …), then **118
   more** written from the list of references still unresolved (the DriverStation split, monotonic time, game data,
   MathUtil, alerts, Sendable, HAL usage reporting, simulation getters, …): 97.8%.

The verifier gate ran after the M4 additions (C++, Python, Java call shapes) and after a Doxygen fix: `deprecated.html`
had marked the classes of a deprecated member's *parameter types* deprecated (`frc::Rotation2d`, because
`EllipticalRegionConstraint`'s old constructor takes one), which produced 11 of the 30 warnings before the fix.

## Real index with vendors (M3, 2026-09-29)

The M3 index has 12 shards: per season it holds WPILib docs, the WPILib Java API, the vendordep catalog, vendor docs,
vendor Java APIs, and vendor-restricted (`LicenseRef-*`) content. That is about 3× the M1 content: roughly 13k chunks
and 58k symbols.

| Measurement | Result | Budget |
|---|---|---|
| `frc-mcp doctor`, 200 mixed queries | p50 2.1 ms · p95 10.1 ms (includes cross-season fallback queries) | p95 ≤ 50 ms |
| eval harness, 228 queries | p50 2.2 ms · p95 5.4 ms | p95 ≤ 50 ms |
| `BenchmarkSearchRealIndex` (`cmd/frc-mcp`, needs `.shards`) | 3.5 ms/op with 28 shards → about 2 ms/op with 12 | — |

**Three driver-level findings** (CPU profiles of `BenchmarkSearchRealIndex`):

1. **STAT4 forces re-preparation on every search.** With `sqlite_stat4` samples, the planner reads bound parameter
   values, and SQLite re-prepares the statement whenever a value changes. The FTS text changes every search, so
   `_sqlite3Reprepare` took 35–49% of search CPU. The writer now deletes the stat4 samples after `ANALYZE` and keeps
   stat1. `TestNoStat4Samples` guards this.
2. **The pure-Go driver's allocator is behind one global mutex.** With unlimited shard fan-out, around 80% of samples
   were in `pthread_cond_wait`/`usleep` under `libc.Xmalloc`. The shard fan-out is capped at `min(4, GOMAXPROCS)`.
   On 28 shards that measured 4.9 ms/op unlimited, 3.5 ms/op at 4 and 6.6 ms/op at 1.
3. **Every shard costs a query.**
   - Season-pinned searches skip shards without that season.
   - Symbol lookups skip shards without symbol tables.
   - Vendor sources are packed into three shards per season instead of one shard per library and source.

   Together with (1) and (2), p50 went from 11.3 ms to 2.1 ms at the same content.

**Note on packing:** BM25 statistics are per shard, so packing changes scores slightly. On the 228-query eval,
nDCG@10 moved from 0.831 to 0.827 and R@10 from 0.967 to 0.963.

## Real index (M1, 2026-09-29)

The corpus is the WPILib docs and Java API for 2026 and 2027-alpha, built by `frc-mcp index run`:
- 7,779 chunks and 35,350 symbols;
- 4 shards (48 MB) plus potion-code-16M-v2 (32 MB);
- Apple M2, Go 1.27.1.

| Path | Result | Budget |
|---|---|---|
| `frc-mcp index run`, cold (downloads ≈ 195 MB) | ≈ 25 s | — |
| `frc-mcp index run`, warm (all sources return 304 Not Modified) | ≈ 13–15 s (parse + embed + write) | — |
| Javadoc parse, one season (≈ 1,100 types, 15–18k members) | 0.6–0.8 s | — |
| model2vec query embedding (`m2v.Encode`) | 4.7 µs, 7 allocs | ≤ 1 ms |
| Load shards + model + vector layers (in the background; `tools/list` does not wait) | ≈ 125 ms | — |
| Warm process launch → first `tools/list` | 19 ms | ≤ 150 ms |
| `frc_search`, hybrid, 184 eval queries | p50 2.2 ms · p95 5.4 ms | p95 ≤ 50 ms |
| `frc-mcp doctor`, 200 mixed queries | p50 3.5 ms · p95 4.7 ms (lexical only: p95 4.4 ms) | p95 ≤ 50 ms |

## Verifier false-positive gate (M2, 2026-09-29)

`frc-mcp verify` was run over 12 public 2026 team repositories, shallow-cloned from GitHub (Java, each detected as
season 2026). The teams are 6328 Mechanical Advantage, StuyPulse, Spartronics 4915, 5427, 4533, 102, 3082, Salem,
166, 5409, 1810 and hammerheads5000.

| Code | Files | Lines | Errors | Warnings | Errors / kLOC | Gate |
|---|---|---|---|---|---|---|
| All 12 repositories, pinned to their own season (2026) | 1,016 | 128,329 | **0** | 7 | **0.000** | ≤ 1.0 ✅ |
| Team 5427 code pinned to **2027** (true-positive check) | 141 | 19,246 | 547 | 84 | 28.4 | — |

- **Two false-positive classes were found and fixed during the gate:**
  1. Inherited members such as `XboxController.isConnected` (declared on `GenericHID` in 2026). The fix walks the
     pinned season's supertype hierarchy.
  2. `org.wpilib.math.*` classes from **SleipnirJava**, a separate 2026 library. The rule now reports `error` only
     when the pinned-season counterpart is known, and `warning` otherwise.
- **The 7 remaining warnings** are those SleipnirJava imports. They are correctly worded as "ignore if it comes from
  another library".

## Distribution size (M2)

`frc-mcp index publish` on the full index (6 shards + model) produces 38 MB of gzip objects:
- 30 MB is the float16 model, which barely compresses and is downloaded once;
- the WPILib docs, API and vendordep shards plus vector layers are about 8 MB.

Objects are content-addressed, so an update downloads only shards whose content changed. Publishing takes 1.8 s.

## Fixture corpus (M0)

Measured on an Apple M2 (8 cores, macOS), Go 1.27.1, `CGO_ENABLED=0`. The numbers come from
`go test -p 1 -bench=. -benchmem ./internal/...`. **Corpus: the synthetic fixture shard** (20 chunks, 15 symbols),
except for the vector benchmarks, which use random unit vectors. Run-to-run noise on a laptop is about 15–30%.
Re-measure on the real M1 corpus before drawing conclusions about scale.

| Path | Result | Budget (CLAUDE.md §6) |
|---|---|---|
| Router decision (`router.Decide`) | 7–8 µs, 24 allocs | — |
| FTS5 query build (`textutil.FTSQuery`) | 2.7 µs | — |
| BM25 retrieval, one shard (`index.FTS`, light integer rows) | 0.18–0.30 ms, 62 allocs | — |
| Exact symbol lookup (`index.Symbols`) | 0.04–0.05 ms | ≤ 2 ms |
| Full `frc_search` pipeline (route → BM25 ∥ symbol → RRF → boosts → diversity → confidence → hydrate) | 0.31–0.59 ms | p95 ≤ 50 ms |
| `frc_search` over the MCP protocol (in-memory transport, SDK JSON + schema validation, client included) | 0.95–1.15 ms | — |
| `frc-mcp doctor`: 200 mixed queries | p50 0.30 ms · p95 0.43 ms · p99 0.64 ms | p95 ≤ 50 ms |
| Shard open | 0.97 ms | — |
| Process launch → first `tools/list` over stdio (warm) | 31 ms | ≤ 150 ms |
| First exec of a freshly built binary (macOS Gatekeeper/XProtect scan) | ~430 ms | notarization (M5) |
| int8 dot product, 256 dims (`vec.DotWide`) | ~60 ns (`vec.Dot` 85–98 ns) | — |
| Exact vector scan, 10k × 256 (1 core) | 0.68–0.80 ms, 0 allocs | — |
| Exact vector scan, 100k × 256 (parallel) | 1.9–2.6 ms | — |

## Decisions backed by these measurements

- **Pre-widened int32 query in the dot product:** 28% faster than int8×int8. Adopted.
- **Sign-bit Hamming prefilter with int8 rescoring:** rejected. At 100k × 256 it saved only about 18% of latency
  and dropped recall@10 to 0.60. The exact scan stays well inside budget at the target corpus size.
- **Router regexps replaced with token-set lookups:** 40 µs → 7 µs. Regexp backtracking was 60% of the CPU profile.
- **Light FTS rows** (enum codes, a document number, and library/symbol matches computed in SQL): no strings are
  allocated for candidates, and bodies are loaded only for the displayed page. The remaining allocations are about 75%
  inside the SQLite driver (per row and per text column). That share is outside our code.
- **Protocol overhead:** the MCP SDK's JSON decoding and output-schema validation account for about 0.6 ms per call.
  This is SDK-bound and acceptable.
