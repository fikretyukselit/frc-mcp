# Benchmarks

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
