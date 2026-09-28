## What & why

## Checklist
- [ ] `make test lint` pass
- [ ] Contracts/docs updated (tool schema, shard schema, render output → `make golden` diff reviewed)
- [ ] Hot-path change? `benchstat` before/after attached
- [ ] No new cgo dependency; new modules justified
- [ ] Upstream facts verified or marked `verified: false`
