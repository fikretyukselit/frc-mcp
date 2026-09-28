# Retrieval design

Status: Draft v0.2 (2026-09-29). v0.2 changes: `docs/reviews/2026-09-29-research-03-review.md`.

## 1. Principles

1. **Lexical carries identifiers; dense carries paraphrase.** On CoIR, BM25 alone (42.3 nDCG@10) beats the static
   potion-code-16M-v2 model alone (39.1), and the hybrid beats both (43.4). FRC queries are identifier-heavy
   (`TalonFXConfiguration`, `SparkMaxConfig`, `PhotonPoseEstimator`), so BM25 over a symbol-boosted schema is the spine.
2. **Metadata filtering beats model size.** The dominant real-world failure is season/language mixing, not embedding
   quality. Filter by pin *before* ranking.
3. **Spend compute at index time, not query time.** Context prefixes, symbol graphs, and (later) doc-expansion are
   computed in CI; the query path is FTS5 + a table lookup + a dot-product scan.
4. **Exact facts are lookups, not retrieval.** Vendordeps, compat, releases, hardware specs, and symbols are served
   from relational tables (`internal/facts`, `internal/apisym`); similarity search is for prose and examples only.
5. **Design for agentic, iterative retrieval.** Agents re-query. Return small, ranked, ID-addressable snippets and let
   the agent `frc_fetch` what it needs (search → fetch), rather than one huge context dump.

## 2. Pipeline (query time)

```
args ─► router ─► pin filter (library, season, channel, language, trust: community excluded unless opted in)
                   │
        ┌──────────┼───────────────────┐
        ▼          ▼                   ▼
   exact symbol  FTS5 BM25 top-50   m2v embed (≤1ms) → int8 flat scan top-50
   (symbol tbl)  (weighted cols)    (pre-filtered row set)
        └──────────┬───────────────────┘
                   ▼
        RRF (k=60) → boosts → dedupe (doc-level diversity, max 2/doc)
                   ▼
        conditional rerank (lite: `rerank` build + low abstention margin, top-10;
                            full: always, top-20, sidecar cross-encoder)
                   ▼
        abstention model → render (budgeted) → structuredContent + resource_links
```

- The three retrievers run concurrently (errgroup) with a shared deadline; any retriever that misses the deadline is
  dropped from fusion and recorded in `_meta.degraded`.
- **Boosts** (multiplicative on fused score, data-driven in `data/boost.yaml`): exact symbol hit ×2.0; pinned version
  exact ×1.3; official WPILib/vendor doc authority ×1.2; `kind=code` when intent=howto ×1.2; forum ×0.7 unless
  intent=troubleshoot; `suspect` ×0.3; alpha channel ×0.5 unless pinned to alpha.
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
  Each model has its own row-aligned layer `chunk.vec.<embed_model_id>`; a layer is used only when the query encoder id
  matches, otherwise dense is skipped for it and reported in `_meta.degraded` (ADR-0005).
- **M1 exit rule:** BM25 + exact symbol is the baseline; potion stays in lite only if it adds ≥ 1 nDCG@10 point.
- **Search:** exact flat scan over the pre-filtered row set; int8 dot product accumulating in int32; top-k via a
  fixed-size min-heap from `sync.Pool`. Expected: ~26M MACs for 100k × 256 → low single-digit ms on one core; split
  across `GOMAXPROCS` workers above 20k candidates.
- **v2 experiment:** distill a custom model2vec from a stronger teacher (Qwen3-Embedding-0.6B, Apache-2.0, or
  CodeRankEmbed) on the FRC corpus vocabulary; ship as a new `embed_model_id` (shards and query model must match —
  manifest enforces it).
- **Not chosen:** EmbeddingGemma (Gemma Terms complicate redistribution to students); ColBERT/PLAID (token-level index
  size, no Go implementation, transformer at query time); ANN (HNSW/DiskANN) — unnecessary at this scale.

## 5. Reranking

Cross-encoder reranking is the largest single retrieval gain in the literature (research 03, §2.1), but a CPU
cross-encoder on every query breaks the lite p95 budget. Policy:

- **lite (build tag `rerank`):** conditional — rerank the top-10 only when the abstention model's margin (top1 − top2
  fused score, BM25/dense disagreement) is below a threshold fit on the train split. Confident queries (most
  identifier queries) skip it. Candidate: Ettin-reranker-17M (Apache-2.0) via `hugot` GoMLX pure-Go backend
  (⚠️ verify model availability and real CPU latency before committing).
- **full:** always rerank top-20 → top-k with an open cross-encoder in the sidecar (candidates: Qwen3-Reranker-0.6B,
  bge-reranker-v2-m3). No commercial APIs (ADR-0004).
- `make eval` reports both arms (with/without rerank) and the share of queries that triggered the conditional path.
- Promotion: ≥ 3 nDCG@10 points with unchanged wrong-season@5; lite additionally requires overall p95 ≤ 50 ms.

## 6. Evaluation

- **Qrels** (`eval/qrels/*.tsv`, TREC format, graded 0–3), ≥ 150 at M1, ≥ 500 at M4, 80/20 train/holdout, sources:
  1. real questions mined from Chief Delphi Programming / FRC Discord FAQs mapped to the answering doc/symbol;
  2. synthetic per-chunk questions (LLM-generated, LLM-judged, human spot-checked);
  3. adversarial buckets: exact symbol, cross-language (Java↔C++↔Python), **cross-season** (Phoenix 5 vs 6, 2026 vs 2027),
     "how do I", troubleshooting.
- **Metrics:** Recall@5, Recall@10, nDCG@10, MRR, **wrong-season@5** (hard gate: 0 when a pinned-season answer
  exists), wrong-language@5, `version_mismatch` correctness, no_match/empty rate, low_confidence rate, abstention
  precision/recall, truncation rate, p95 response tokens, p50/p95 latency, allocations.
- **Targets:** M1 Recall@10 ≥ 0.75, nDCG@10 ≥ 0.55 (tighten per milestone); wrong-season@5 = 0 always.
- **Safety metrics:** injection-corpus suspect recall ≥ 0.95; 0 unfenced community snippets in rendered output.
- **Verifier:** false positives ≤ 1 per 1k LOC on clean public 2026 team repos; wrong-season recall on seeded corpora.
- **Holdout:** human-written (LLM-generated qrels skew toward lexical overlap with the source chunk).
- **Agent-level eval** (`eval/tasks`): ~50 coding tasks (e.g. "configure a Kraken X60 with Motion Magic on Phoenix 6
  26.x", "port this 2026 subsystem to 2027 alpha") run by an agent with frc-mcp; graded by compile check (GradleRIO
  build in a container) + LLM-judge rubric; pairwise vs previous release and vs the same agent **without** frc-mcp
  (target: ≥ +20 pp compile-pass uplift). RAGAS-style faithfulness only on sampled tasks, never as a CI gate.
- **CI gate:** `make eval` compares to `eval/baseline.json`; fail if Recall@10 or nDCG@10 drop > 1.5 points absolute,
  wrong-season@5 increases, or p95 > 50 ms. Metrics diff posted as a PR comment. Baseline updates require an explicit
  commit.

## 7. Rendering & token economy

- `response_format: concise` (default): top-k (default 5) snippets, each ≤ 350 tokens, with title, heading path,
  version, URL, `id`. `detailed`: up to `max_tokens` (default 6k, cap 20k).
- Full pages are never inlined from `frc_search`; return `resource_link` → `frc://docs/...` and let the client read on demand.
- Code blocks are rendered in the pinned language only unless `language: any`.
- Every response ends with a one-line `next:` hint when useful (e.g. "call frc_api for exact signature").
- `content` is meaning-equivalent to `structuredContent` (same renderer); community snippets are fenced
  (`docs/security.md §2.1`); truncation is explicit (`truncated`, `omitted`, `next_cursor`).
- Formats: Markdown for prose/code, compact JSON for data; no TOON (ADR-0004).
