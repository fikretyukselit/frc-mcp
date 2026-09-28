# ADR 0003 — Separate ingestion and serving planes; signed OCI index shards

- Status: Accepted (2026-09-28)

## Context
Crawling from thousands of student laptops would hammer upstreams (RTD, GitHub, Chief Delphi) and be slow and flaky.
Frozen indexes baked into packages (existing FRC MCPs) go stale within weeks during kickoff and 2027 alpha churn.

## Decision
A central ingestion plane (GitHub Actions, adaptive scheduler) builds per-(source group, season) SQLite shards,
gates them with the retrieval eval, and publishes them to GHCR as OCI artifacts via oras-go, signed with cosign keyless.
Clients sync by manifest digest (content-addressed ⇒ only changed shards download) and verify signatures. The serving
plane never crawls; it may perform bounded, ETag-cached freshness probes for "latest version" answers.

## Consequences
- Upstream load is O(1) in the number of users; freshness is centrally observable (`freshness.json`).
- Requires CI secrets hygiene and an index schema-versioning discipline (binary refuses incompatible major schema).
