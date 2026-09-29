# ADR 0006: Sign index manifests with ed25519 (compiled-in keys); keep cosign for binaries

- Status: Accepted (2026-09-29). This amends ADR-0003 and `docs/security.md` §2.4.
- Deciders: plan author. Needs the Foundation to generate the production key; see *Rollout*.

## Context
ADR-0003 planned cosign keyless signatures for index shards. Verifying keyless signatures inside the client means:
- vendoring sigstore-go (TUF, Rekor and Fulcio verification, protobuf);
- fetching the Sigstore trust root over the network before the first verification;
- binary size and attack surface growing well past the 40 MB budget's intent.

Students must also be able to sync from school mirrors and verify offline.

## Decision
- **Index manifests** are signed with **ed25519** (Go standard library). Each manifest lists every file's sha256 and
  gzip sha256, and carries a monotonic `serial` and an `expires_at`.
- **The client trusts only compiled-in public keys** (`internal/dist/keys.go`), plus keys given explicitly with
  `--trusted-key` / `FRC_MCP_TRUSTED_KEYS` for self-hosted mirrors.
- **The client rejects:**
  - an untrusted key or a bad signature;
  - a lower serial (rollback);
  - a digest mismatch.
- **It reports** an expired manifest as `index_stale`. It keeps serving and never goes dark.
- **The private key** lives only in the protected GitHub environment `index-publish` (main branch only) as
  `FRC_MCP_INDEX_KEY`.
  - The `build` job has no secrets; only the isolated `publish` job signs.
  - `frc-mcp index keygen` prints the seed once and never writes it to disk.
- **Rotation:** ship the new public key in a binary release first, switch the publisher, then remove the old key one
  release later.
- **Binaries** still ship with cosign keyless signatures, SLSA provenance and SBOMs (goreleaser, M5). Security reviewers
  verify them with standard tooling.

## Consequences
- Verification needs no network access, no extra dependencies, and takes under 1 ms.
- Key management becomes our responsibility. A leaked key lets an attacker sign indexes until rotation. This is
  mitigated by environment protection, the serial + expiry fields, and the fact that shards are read-only data served
  with trust tiers and fencing. Shards never contain code.
- Until the production key exists, `trustedKeys` is empty and `serve` does not sync. Local `index run` builds keep
  working unchanged.

## Rollout
1. A Foundation maintainer runs `frc-mcp index keygen`.
2. They store the seed as the `FRC_MCP_INDEX_KEY` secret in the protected `index-publish` environment.
3. The public key is committed to `internal/dist/keys.go`.
4. The `index` workflow runs; it needs GitHub Actions billing, or a public repository.

**Status of the rollout (2026-09-29):** steps 1–3 are done. The production key has id `4d6c685f407388a5`; its seed was
piped straight from `index keygen` into the `FRC_MCP_INDEX_KEY` secret of the `index-publish` environment (protected
branches only) and never written to disk or shown. The repository is public, so step 4 runs on the free Actions tier.

