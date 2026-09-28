# ADR 0001 — Go, single static binary, CGO disabled

- Status: Accepted (2026-09-28)

## Context
Users are high-school students on mixed Windows/macOS/Linux laptops, often behind school networks, installing via the
path of least resistance. The MCP server is launched per-session by the agent client, so cold start matters.
MCP SDK tiers (2026-07-28): TypeScript, Python, Go, C# are Tier 1; Rust was beta at that release.

## Decision
Implement in Go 1.27 with the official `modelcontextprotocol/go-sdk`. The shipped binary is built with
`CGO_ENABLED=0` for all targets; every dependency in the default build graph must be pure Go (or Wasm-in-Go).

## Consequences
- `go install`, goreleaser cross-compilation, Homebrew/Scoop/Winget all trivial; no Node/Python runtime needed.
- Excludes cgo-based libraries (mattn/go-sqlite3, FAISS, tantivy, ONNX Runtime) from the default build; they may exist
  only behind opt-in build tags.
- Rust would be marginally faster, but retrieval latency is dominated by algorithmic choices, not language, at our scale.
