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
- Content that states no redistribution license (`LicenseRef-*`: REV docs and Java API, Phoenix 6 Java/Python, spec
  pages, CTRE/REV release notes) is loaded over HTTP only with `--include-unlicensed`, which the Foundation's server
  passes (docs/sources.md §0.2). Prohibited content (forum posts; `LicenseRef-*-EULA`: CTRE's C++ header EULA) is
  never loaded over HTTP, whatever the flags.
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

## Behind an existing nginx and Cloudflare (the Foundation's server)

The public instance, `https://mrkaynak.com/frc/mcp`, shares a host with other sites, so it uses the host's nginx
instead of Caddy:

- **Container** (`/opt/frc-mcp/docker-compose.yml`): the released image pinned by digest, ports published on
  `127.0.0.1` only (`7424` MCP, `9465` metrics), on its own network with a fixed gateway, plus the same hardening as
  above and `mem_limit`, `cpus`, `pids_limit` and log rotation.
- **Trusted proxies**: `--trust-proxy=<network gateway>/32,<Cloudflare ranges>`. nginx reaches the container through
  the gateway, and Cloudflare is the hop in front of nginx, so the right-most untrusted `X-Forwarded-For` entry is the
  real client. The Cloudflare list comes from `https://www.cloudflare.com/ips-v4` and `/ips-v6`; refresh it when
  Cloudflare changes it.
- **nginx** (inside the site's `server` block):

  ```nginx
  location = /frc/mcp {
      proxy_pass http://127.0.0.1:7424/mcp;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto $scheme;
      proxy_set_header Connection "";
      proxy_buffering off;
      proxy_read_timeout 90s;
      client_max_body_size 1m;
      access_log off;
  }
  location = /frc/healthz { proxy_pass http://127.0.0.1:7424/readyz; access_log off; }
  ```

Checked on 2026-09-29 against the live endpoint: `initialize`, `tools/list` and tool calls through Cloudflare; a
per-client burst of 30 then `429`; another client unaffected meanwhile; spoofed `X-Forwarded-For` does not reset the
bucket; `path` arguments refused; bodies over 1 MiB → `413`; cross-site browser requests → `403`; the MCP and
metrics ports closed on the public address.

## Before announcing the URL

Walk the launch checklist in `docs/security.md` §4.
