# ADR 0004: Reject Postgres, API embedding/rerank, and TOON for the local profile

- Status: Accepted (2026-09-29)
- Context source: `docs/research/03-mcp-design-and-rag-2026.tr.md`; review: `docs/reviews/2026-09-29-research-03-review.md`

## Context
Research 03 recommends the following stack:
- Postgres with pgvector and pg_search as the single data layer.
- API embeddings (Voyage or Gemini) and API rerankers (Cohere or Voyage).
- TOON as an optional output format for long tables.

That research assumes a hosted daemon. frc-mcp's primary users are students running a local agent session, which
brings these requirements:
- R6: a single static binary.
- R7: the server works offline.
- A cold start of ≤ 150 ms.
- No transfer of minors' queries to third parties.

## Decision
- **Data layer:** SQLite (modernc) holds FTS5, relational fact tables and int8 vectors, per ADR-0002. Postgres is not
  used in any profile. The hosted profile serves the same shards.
- **Embedding/rerank APIs:** not used in any profile. The hosted-full profile may run *open-weight* models in a
  sidecar (ADR-0005). Commercial APIs are excluded, so that queries never leave operator-controlled infrastructure
  and there are no per-query costs.
- **TOON:** not implemented in v1. Evidence for rejecting it:
  - The research's own independent source (arXiv 2605.29676) reports −9 points of accuracy in agentic loops, parse
    errors across multiple turns, and broken parallel tool calls.
  - The Go libraries are community-grade.
  - The spec version has drifted.
  - No frc-mcp output needs it: the long tables are rare and fit in compact Markdown or JSON.
  - Revisit only with an eval showing no accuracy loss on our agent tasks.

## Consequences
- Keeps R6 and R7, and avoids operational burden for a volunteer-maintained foundation project.
- Accepts lower dense-retrieval quality in local-lite. This is mitigated by lexical/symbol-first routing, conditional
  rerank and structured lookups (ADR-0005).
