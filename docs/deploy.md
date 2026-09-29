# Deploying the hosted server

The hosted profile (ADR-0005) serves the same signed index over Streamable HTTP so that anyone can point an MCP
client at one URL without installing anything. This guide is for the Foundation's server; any Linux host with Docker
works the same way.

## What you get

- `https://<domain>/mcp`: the MCP endpoint (stateless Streamable HTTP, JSON responses).
- Anonymous access with limits: per-client token bucket (default 1 request/s sustained, burst 30), a global cap of
  64 concurrent requests, 1 MiB request bodies, 60 s responses. Over the limit a client gets `429` with `Retry-After`.
- The index syncs from the signed `index-stable` channel at startup and every 6 hours (ed25519, ADR-0006). A failed
  sync keeps serving the installed index.
- Content without a redistribution license (`LicenseRef-*`: CTRE docs/APIs, REV Java API and docs, spec pages, forum
  posts) is **not loaded** over HTTP. Only pass `--include-unlicensed` once the vendors have granted permission.
- Filesystem arguments (`project_root`, `path`) are disabled: clients send code inline.
- No query logging. The only telemetry is aggregate counters (requests by JSON-RPC method, tool and status class,
  plus a latency histogram) on an internal metrics port.

## Requirements

- A Linux host with Docker Engine 24+ and the compose plugin; 1 vCPU and 2 GB RAM are enough (the served index is about
  100 MB on disk, memory-mapped).
- A DNS `A`/`AAAA` record for the domain pointing at the host, and ports 80 and 443 open (Caddy obtains and renews
  the TLS certificate automatically).

## Steps

```sh
git clone https://github.com/fikretyukselit/frc-mcp && cd frc-mcp/deploy
DOMAIN=mcp.example.org VERSION=$(git describe --tags --always) docker compose up -d --build
docker compose logs -f frc-mcp        # "index synced" then "index loaded"
curl https://mcp.example.org/readyz    # "ready" once the index is loaded
```

Clients then add the server by URL, e.g. for Claude Code:

```sh
claude mcp add --transport http frc https://mcp.example.org/mcp
```

## Operating it

| Task | How |
|---|---|
| Update the server | `git pull && docker compose up -d --build` (the index volume is kept) |
| Force an index refresh | `docker compose restart frc-mcp` (sync runs at startup) |
| Change limits | `FRC_MCP_RATE`, `FRC_MCP_BURST`, `FRC_MCP_MAX_INFLIGHT` in the environment of `docker compose up` |
| Metrics | `docker compose exec caddy wget -qO- http://frc-mcp:9464/metrics` (never publish port 9464) |
| Health | `/healthz` (process up), `/readyz` (index loaded) |
| Logs | JSON on stderr: `docker compose logs frc-mcp`. They contain no request content. |

**Rate limits and proxies.** frc-mcp rate-limits by client IP. Behind Caddy it trusts `X-Forwarded-For` only from
Caddy's fixed address on the private network (`--trust-proxy=172.28.0.10/32`); any other peer's header is ignored,
so clients cannot spoof their address. If you put another proxy or a CDN in front, add its address ranges to
`--trust-proxy`.

**Hardening in the compose file.** The container runs as a non-root user on a read-only root filesystem with all
capabilities dropped and `no-new-privileges`; only `/data` (the index) is writable. The images are pinned by digest.

## Before announcing the URL

Walk the launch checklist in `docs/security.md` §4.
