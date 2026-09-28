# Fixture corpus

Synthetic, hand-written chunks and symbols used by tests and the M0 fixture shard. The content is
**illustrative, not authoritative**: it is shaped like real WPILib/vendor documentation, but it is not a
source of truth and must never be published in a release index. The seasons, languages, trust tiers
(including one deliberately malicious forum post) and version pairs (2026 `edu.wpi.first` vs 2027 `org.wpilib`,
REVLib 2024 `CANSparkMax` vs 2025+ `SparkMax`) exist to exercise pin filtering, `version_mismatch`,
fencing and suspect detection.

Build: `go run ./cmd/frc-mcp index build --chunks testdata/fixture/chunks.jsonl --symbols testdata/fixture/symbols.jsonl --out .shards/fixture.sqlite`
