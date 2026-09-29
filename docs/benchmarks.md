# Benchmarks

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
