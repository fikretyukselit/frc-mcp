#!/usr/bin/env bash
# Runs the official MCP conformance suite's server scenarios that apply to
# frc-mcp against the HTTP transport on the fixture index. Scenarios that need
# the suite's own test tools (image/audio results, sampling, elicitation,
# specific schema tools) do not apply to a read-only knowledge server.
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'kill "${pid:-0}" 2>/dev/null || true; rm -rf "$work"' EXIT
go build -o "$work/frc-mcp" ./cmd/frc-mcp
"$work/frc-mcp" index build --chunks testdata/fixture/chunks.jsonl --symbols testdata/fixture/symbols.jsonl \
  --out "$work/shards/fixture.sqlite" >/dev/null
"$work/frc-mcp" serve --transport http --offline --index "$work/shards" --addr 127.0.0.1:7433 2>"$work/serve.log" &
pid=$!
for _ in $(seq 1 50); do curl -sf http://127.0.0.1:7433/readyz >/dev/null && break; sleep 0.2; done
fail=0
for s in server-initialize ping tools-list resources-list logging-set-level dns-rebinding-protection; do
  if npx -y @modelcontextprotocol/conformance@0.1.16 server --url http://127.0.0.1:7433/mcp --scenario "$s" >"$work/$s.log" 2>&1; then
    echo "PASS $s"
  else
    echo "FAIL $s"; cat "$work/$s.log"; fail=1
  fi
done
exit $fail
