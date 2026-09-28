# ADR 0002 — SQLite FTS5 + flat int8 vectors + model2vec, no ANN, no vector DB

- Status: Accepted (2026-09-28)

## Context
Corpus is ~50–150k chunks across seasons. Queries are identifier-heavy. Budget: p95 ≤ 50 ms on CPU, pure Go.
Evidence: BM25 beats small static embeddings alone on code retrieval (CoIR 42.3 vs 39.1 nDCG@10) while the hybrid wins
(43.4); flat scan of 100k × 256 int8 is a few ms.

## Decision
- `modernc.org/sqlite` FTS5 with weighted columns for lexical retrieval; shards opened read-only/immutable.
- Dense: potion-code-16M-v2 (model2vec, MIT) computed in pure Go at query time; int8 vectors in an mmapped sidecar;
  exact flat scan after metadata pre-filter.
- Fusion: RRF (k=60) + data-driven boosts. Reranker optional behind build tag.

## Alternatives rejected
bleve (vector support needs FAISS/cgo), sqlite-vec (needs ncruces driver; ANN still alpha), HNSW libraries
(unnecessary at this scale; approximate), ColBERT/PLAID (index size, no Go impl), transformer query embeddings by
default (runtime + latency), hosted vector DB (offline requirement).

## Revisit when
Corpus > 1M chunks, or eval shows dense contributes < 1 nDCG point (drop it) or a distilled domain model2vec gains > 3.
