# Review: research 03 ("MCP design & RAG, Sep 2026") vs. plan v0.1

- Date: 2026-09-29
- Input: `docs/research/03-mcp-design-and-rag-2026.tr.md`
- Reviewers: two independent staff-level reviews (Claude Fable 5.1 and Claude Opus 5.5), synthesized by the plan author.
- Outcome: plan revised to **v0.2**. All changes are listed in §4.

## 1. Summary judgement

The research is accurate on the MCP 2026-07-28 facts, tool-design practice, and the security threat model. Its stack
recommendations, however, assume a **hosted daemon**: Postgres + pgvector/pg_search, API embeddings, and API rerankers.
Our users are students on laptops, and frc-mcp is designed local-first, offline, as a single static binary. Applied to
that setting, most of the stack advice does not fit. Both reviewers independently reached the same position:

- **keep the architecture:** ADR-0001/0002/0003 stand;
- **adopt the security, contract, and structured-data lessons;**
- **adopt a separate hosted profile** for the cases where the research's stack does make sense.

## 2. Where the research is right and we lacked it

1. **Indirect prompt injection** comes in through release notes (auto-generated from PR titles) and forum/Reddit/YouTube
   text. Plan v0.1 had no `trust` field, no sanitization, and no fencing of untrusted text. (Both reviewers rated this critical.)
2. **SSRF / local file reads:** `frc_vendordep.url`, `frc_read.uri`, and `--live` probes accepted URLs, and
   `frc_context.project_root` read the filesystem. In HTTP mode these are exploitable. (Critical.)
3. **Tool poisoning and supply chain:** tool descriptions must be static and compiled in. Plan v0.1 also had no
   rollback/freeze protection on index manifests and did not pin the signer identity.
4. **Exact data belongs in relational tables, not embeddings:** vendordeps, the compatibility matrix, releases, hardware
   specs, and rules. Shard schema v1 only had `symbol`.
5. **Result contract:** `content` must carry the same meaning as `structuredContent`. Many clients show the model only
   `content`, and the MCP roadmap is moving to a single result shape. The contract also needs explicit `truncated`/`omitted`
   fields and a stateless cursor.
6. **Tool-description quality standard** ("use when / don't use when / example"). Validation errors must be `isError`
   results that list the valid values.
7. **Reranking** is the largest single retrieval gain. Our gate, "default only if p95 ≤ 50 ms", would never pass on CPU.
8. **The empty-result / abstention rate** is the best runtime quality signal.
9. **OpenAI search/fetch compatibility.** The research missed that this requires tools literally named `search` and
   `fetch` with fixed output shapes; a namespaced `frc_search` does not qualify.

## 3. Where the research does not fit our product (rejected)

| Research recommendation | Verdict | Reason |
|---|---|---|
| Postgres + pgvector + pg_search as the single data layer | **Reject** | Breaks the single binary, offline use, and the ≤150 ms cold start. Its sqlite-vec latency concern (100k × 1024 fp32 through an extension) does not apply to 100k × 256 int8 in a Go loop. |
| API embeddings (Voyage/Gemini) and API rerankers (Cohere/Voyage) | **Reject (local); adapt (hosted)** | Need network, keys, and money, and would send minors' queries to third parties. Acceptable only in the hosted profile, and only with open models. |
| "Streamable HTTP is ideal for teams" | **Adapt** | stdio stays the default. HTTP becomes a first-class but secondary profile. |
| TOON as an optional format for long tables | **Reject for v1** | The research's own evidence shows a 9-point accuracy loss in agentic loops, community-grade Go libraries, and spec drift. No output of ours needs it. |
| `frc_check_compat` as a separate tool | **Fold** into `frc_vendordep` (set validation) and `frc_context` (warnings) | A third overlapping entry point would hurt tool selection. |
| `frc_releases` | **Reject** | `frc_whats_new` is a superset of it. |
| `frc_community` as a tool | **Reject as a tool; adopt the substance** | Forum content is reachable via `frc_search kinds=[forum]`, off by default and always tagged `trust: community`. |
| `frc_scaffold` | **Defer (post-M4)** | Duplicates the `frc-new-subsystem` prompt. Revisit as a deterministic composite once `frc_api`/`frc_vendordep` are stable. |
| "IDs only in detailed mode" | **Reject** | A search → fetch server always needs IDs. |
| Recall@10 ≥ 0.9 as the initial target | **Adapt** | Unrealistic for about 150 mixed qrels. M1: Recall@10 ≥ 0.75, nDCG@10 ≥ 0.55, **wrong-season@5 = 0 as a hard gate**. |
| RAGAS faithfulness as a CI gate | **Reject as a gate** | The server does not generate answers, and LLM judges are nondeterministic. Use it only on sampled agent-level tasks. |

## 4. Decisions applied in plan v0.2

1. **Security baseline** (new `docs/security.md`, CLAUDE.md invariants 11–15):
   - a `trust` tier on every chunk;
   - ingest-time sanitization and a `suspect` flag;
   - untrusted text fenced in `content`;
   - network egress limited to an allowlist derived from `sources.yaml` (https only, re-checked on every redirect,
     private and link-local IPs blocked at dial time);
   - filesystem arguments disabled in HTTP mode;
   - static, golden-tested tool descriptions;
   - manifest serial + expiry;
   - cosign identity pinned to the workflow path and ref;
   - CI split between build and sign jobs; Actions pinned by SHA.
2. **Deployment profiles** (ADR-0005):
   - **local-lite** (default): stdio, offline, BM25 + potion + RRF, conditional rerank when that build is available;
   - **hosted-full**: stateless Streamable HTTP, a stronger dense model + cross-encoder rerank served by a sidecar,
     bearer/OAuth, aggregate metrics only.
   - Shards are byte-identical across profiles. Vectors are per-model sidecar layers, and the retriever uses a vector
     layer only when its query model matches.
3. **Conditional rerank:** rerank the top 10 only when the abstention margin is low, so confident queries keep their
   p95. It is always on in hosted-full. `make eval` measures both arms.
4. **Structured tables in the shard:** `vendordep`, `compat`, `release`, `hw_spec` (dual-source, never merged), and `rule`.
   New invariant: exact facts are relational lookups, never similarity retrieval.
5. **MCP surface v0.2 (9 tools):**
   - renames: `frc_read` → **`frc_fetch`**;
   - `frc_vendordep` absorbs compatibility checks;
   - new **`frc_hardware`** (implemented in M3; also exposed as a resource);
   - Envelope gains `truncated`, `omitted`, `next_cursor`, `trust`, and `coverage` (for the verifier);
   - HTTP-only opt-in `--toolset openai` exposes literal `search`/`fetch`;
   - TOON is rejected (ADR-0004).
6. **Verifier MVP scope cut:**
   - Java imports and qualified types only;
   - `error` only for `wrong_season` (the symbol is absent from the pinned table **and** present in another season's
     table), otherwise `info`;
   - symbol tables come from Javadoc search indexes / class files, Doxygen XML, and `.pyi` stubs;
   - `since`/`removed_in` are computed by diffing version tables;
   - the 2027 package-move table is generated by diffing and then curated by a human.
7. **Milestones re-scoped:**
   - the verifier MVP and signed distribution move up to M2, which must be usable before the January 2027 kickoff;
   - the adaptive scheduler moves to M5 (fixed cron + conditional GET until then);
   - Game Manual/rules ingestion is deferred.
8. **Distribution realities:**
   - first run serves `tools/list` immediately and syncs in the background (`status: syncing`);
   - an HTTPS mirror (GitHub Releases) for networks that block GHCR, with resumable downloads;
   - Windows Authenticode + macOS notarization;
   - a disk budget and retention policy.
9. **Project detection:** RobotPy (`pyproject.toml`) and multi-project roots. Symbol coverage per (library, language) is
   published in the manifest and returned by the verifier.
10. **Privacy:** users are minors, so there is no telemetry by default. `doctor --stats` reads a local ring buffer only.
    Hosted-full keeps aggregate counters keyed by `Mcp-Name`; query logs require consent.
11. **Evaluation additions:** no_match/empty rate, low_confidence rate, wrong-language@5, correctness of
    `version_mismatch` answers, truncation rate, p95 response tokens, verifier false positives (≤ 1 per 1k LOC on clean
    public 2026 team repos), and agent compile-pass uplift versus no MCP (target ≥ +20 pp). A human-written holdout is kept.
12. **Non-English queries** (e.g. Turkish teams) are a documented limitation. The router falls back to identifier-weighted
    BM25 when the query language is not English. A multilingual dense model is a hosted-full candidate.

## 5. Reviewer disagreements and resolution

- **`frc_hardware`:** Fable wanted to adopt it as a tool; Opus wanted to defer it to a resource. **Resolution:** it is
  specified in the v0.2 surface because feedforward and simulation code need motor constants, but it is implemented in M3,
  with the `frc://specs/{part}` resource first.
- **Rerank policy:** Fable proposed a conditional rerank tied to the abstention margin; Opus proposed a profile split.
  **Resolution:** do both. Conditional rerank in local builds that carry the `rerank` tag; always-on in hosted-full.
- **Dense in M1:** Opus proposed BM25-first and keeping potion only if it adds ≥ 1 nDCG point. **Adopted** as the
  M1 exit criterion (it matches the ADR-0002 revisit rule).

## 6. Open items to verify before M0

- Whether go-sdk v1.8.0 is GA. The research says v1.8 was pre-release at time of writing; our proxy.golang.org check
  saw v1.8.0. Pin ≥ v1.7.0 and confirm.
- That the Ettin reranker 17M exists, its licence, and its real CPU latency on the pure-Go backend of `hugot`.
- The exact output schemas for the OpenAI search/fetch toolset.
- The current MCP conformance suite, and a patched MCP Inspector version (CVE-2025-49596).
