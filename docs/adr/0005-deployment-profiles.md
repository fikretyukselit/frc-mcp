# ADR 0005: Deployment profiles (local-lite, hosted-full) over one shard format

- Status: Accepted (2026-09-29)

## Context
The largest single retrieval gain comes from cross-encoder reranking and stronger dense models. Both cost too much on
a student laptop's CPU. Some teams, schools and the Foundation itself could run a shared server. We want those gains
there without forking the index format or the code paths.

## Decision
Two build/runtime profiles share **byte-identical shards**: text, FTS, fact tables and symbol tables are the same.

| | local-lite (default) | hosted-full |
|---|---|---|
| Transport | stdio (HTTP optional, localhost) | stateless Streamable HTTP behind any LB |
| Dense | potion-code-16M-v2 (pure Go) | potion **plus** a stronger open model (candidate: Qwen3-Embedding-0.6B @ MRL-256), query-embedded by a sidecar (TEI/ONNX) |
| Rerank | Conditional: top-10 only when the abstention margin is low, and only in binaries built with the `rerank` tag | Always on, top-20 → top-k, open cross-encoder (candidates: Qwen3-Reranker-0.6B, bge-reranker-v2-m3) |
| Filesystem args | Allowed (project root, scoped) | Disabled |
| Auth | none (local) | Bearer token now; OAuth 2.1 + CIMD later |
| Telemetry | none; local `doctor --stats` | Aggregate counters by `Mcp-Name`; no query logs without opt-in |
| Extra toolset | — | `--toolset openai` (`search` / `fetch`) |

**Vector layers:**
- Vectors ship as per-model sidecar layers `chunk.vec.<embed_model_id>`, each a separate OCI layer listed in the
  manifest with its dims and quantization.
- Clients fetch only the layers their profile uses.
- The retriever uses a layer only if its query encoder's id matches the layer's id. Otherwise it drops dense for that
  layer and reports `_meta.degraded: ["dense:<id>"]`.

**Sidecar rule:**
- The "no sidecar" rule in CLAUDE.md §3 applies to local-lite only.
- hosted-full may depend on one inference sidecar. The server must still degrade gracefully to lite behavior when the
  sidecar is unhealthy.

## Promotion criteria
- A dense model or reranker becomes part of hosted-full only if `make eval` shows ≥ 3 nDCG@10 points gained with
  wrong-season@5 unchanged.
- It is added to local-lite only if it also meets the local p95 budget.
