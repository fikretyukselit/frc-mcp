# Retrieval design

Status: Draft v0.1 (2026-09-28).

## 1. Principles

1. **Lexical carries identifiers; dense carries paraphrase.** On CoIR, BM25 alone (42.3 nDCG@10) beats the static
   potion-code-16M-v2 model alone (39.1), and the hybrid beats both (43.4). FRC queries are identifier-heavy
   (`TalonFXConfiguration`, `SparkMaxConfig`, `PhotonPoseEstimator`), so BM25 over a symbol-boosted schema is the spine.
2. **Metadata filtering beats model size.** The dominant real-world failure is season/language mixing, not embedding
   quality. Filter by pin *before* ranking.
3. **Spend compute at index time, not query time.** Context prefixes, symbol graphs, and (later) doc-expansion are
   computed in CI; the query path is FTS5 + a table lookup + a dot-product scan.
4. **Design for agentic, iterative retrieval.** Agents re-query. Return small, ranked, ID-addressable snippets and let
   the agent `frc_read` what it needs (search → read), rather than one huge context dump.

## 2. Pipeline (query time)

```
args ─► router ─► pin filter (library, season, channel, language)
                   │
        ┌──────────┼───────────────────┐
        ▼          ▼                   ▼
   exact symbol  FTS5 BM25 top-50   m2v embed (≤1ms) → int8 flat scan top-50
   (symbol tbl)  (weighted cols)    (pre-filtered row set)
        └──────────┬───────────────────┘
                   ▼
        RRF (k=60) → boosts → dedupe (doc-level diversity, max 2/doc)
                   ▼
        optional rerank (build tag `rerank`, top-10, Ettin-17M)
                   ▼
        abstention model → render (budgeted) → structuredContent + resource_links
```

- The three retrievers run concurrently (errgroup) with a shared deadline; any retriever that misses the deadline is
  dropped from fusion and recorded in `_meta.degraded`.
- **Boosts** (multiplicative on fused score, data-driven in `data/boost.yaml`): exact symbol hit ×2.0; pinned version
  exact ×1.3; official WPILib/vendor doc authority ×1.2; `kind=code` when intent=howto ×1.2; forum ×0.7 unless
  intent=troubleshoot; alpha channel ×0.5 unless pinned to alpha.
- **Dedupe/diversity:** at most 2 chunks per `doc_id` in the top-k; adjacent chunks of the same section are merged.

## 3. Chunking

| Source kind | Unit | Prefix |
|---|---|---|
| Sphinx/RST prose (WPILib, CTRE, PhotonVision) | heading section, 150–600 tokens, split on paragraph boundary; code-block tab sets kept together and tagged per language | `lib › ver › page › h1 › h2` |
| GitBook/Markdown (REV, YAGSL, PathPlanner, Choreo, AdvantageKit) | same | same |
| Javadoc / Doxygen / Python stubs | **one chunk per symbol** (class summary; each method/ctor overload group) with signature, params, returns, throws, since/deprecated | `lib › ver › lang › package › Class` |
| Release notes / changelogs | one chunk per bullet group or version heading | `lib › version › "release notes"` |
| Known issues | one per issue | `lib › season › "known issue"` |
| Game Manual | one per rule ID (`R501`, `G204`) + blue-box | `manual › season › section › rule` |
| Chief Delphi RSS | one per post, dated, `authority=low` | `forum › category › topic title` |

Code blocks are never split. Chunks record `tokens` so the renderer can budget without re-tokenizing.

## 4. Embeddings

- **Model:** `minishlab/potion-code-16M-v2` (MIT, 256-d). Inference = tokenizer → token-id embedding lookup → weighted
  mean → L2 normalize. Implemented in `internal/embed/m2v` (~300 LOC, pure Go, golden-tested against the Python
  reference to 1e-5 cosine). Weights loaded via mmap from the shard bundle (`models/potion-code-16M-v2.safetensors`).
- **Storage:** int8 symmetric quantization per dimension (scale stored in manifest); 256 B/chunk → 100k chunks ≈ 25 MB.
- **Search:** exact flat scan over the pre-filtered row set; int8 dot product accumulating in int32; top-k via a
  fixed-size min-heap from `sync.Pool`. Expected: ~26M MACs for 100k × 256 → low single-digit ms on one core; split
  across `GOMAXPROCS` workers above 20k candidates.
- **v2 experiment:** distill a custom model2vec from a stronger teacher (Qwen3-Embedding-0.6B, Apache-2.0, or
  CodeRankEmbed) on the FRC corpus vocabulary; ship as a new `embed_model_id` (shards and query model must match —
  manifest enforces it).
- **Not chosen:** EmbeddingGemma (Gemma Terms complicate redistribution to students); ColBERT/PLAID (token-level index
  size, no Go implementation, transformer at query time); ANN (HNSW/DiskANN) — unnecessary at this scale.

## 5. Reranking (optional)

Ettin-reranker-17M (Apache-2.0) via `hugot` (GoMLX pure-Go backend) behind build tag `rerank`, top-10 only.
Rough CPU cost ~40 ms → exceeds the default p95 budget, therefore off by default and exposed as `rerank: true`
argument when the binary supports it. Evaluate: adopt as default only if nDCG@10 gain ≥ 3 points and p95 ≤ 50 ms.

## 6. Evaluation

- **Qrels** (`eval/qrels/*.tsv`, TREC format, graded 0–3), ≥ 150 at M1, ≥ 500 at M4, 80/20 train/holdout, sources:
  1. real questions mined from Chief Delphi Programming / FRC Discord FAQs mapped to the answering doc/symbol;
  2. synthetic per-chunk questions (LLM-generated, LLM-judged, human spot-checked);
  3. adversarial buckets: exact symbol, cross-language (Java↔C++↔Python), **cross-season** (Phoenix 5 vs 6, 2026 vs 2027),
     "how do I", troubleshooting.
- **Metrics:** Recall@5, Recall@10, nDCG@10, MRR, **wrong-season@5** (share of top-5 from a non-pinned season — must be 0
  when a pinned-season answer exists), abstention precision/recall, p50/p95 latency, allocations.
- **Agent-level eval** (`eval/tasks`): ~50 coding tasks (e.g. "configure a Kraken X60 with Motion Magic on Phoenix 6
  26.x", "port this 2026 subsystem to 2027 alpha") run by an agent with frc-mcp; graded by compile check (GradleRIO
  build in a container) + LLM-judge rubric; pairwise vs previous release.
- **CI gate:** `make eval` compares to `eval/baseline.json`; fail if Recall@10 or nDCG@10 drop > 1.5 points absolute,
  wrong-season@5 increases, or p95 > 50 ms. Metrics diff posted as a PR comment. Baseline updates require an explicit
  commit.

## 7. Rendering & token economy

- `response_format: concise` (default): top-k (default 5) snippets, each ≤ 350 tokens, with title, heading path,
  version, URL, `id`. `detailed`: up to `max_tokens` (default 6k, cap 20k).
- Full pages are never inlined from `frc_search`; return `resource_link` → `frc://docs/...` and let the client read on demand.
- Code blocks are rendered in the pinned language only unless `language: any`.
- Every response ends with a one-line `next:` hint when useful (e.g. "call frc_api for exact signature").
