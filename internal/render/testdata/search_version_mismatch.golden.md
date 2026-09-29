**frc_search** · status: version_mismatch · confidence: 0.09 · season: 2026 (arg) · language: java · source: shard · index age: 2h30m

Exact API symbols:
- `com.revrobotics.CANSparkMax` (revlib 2024.2.4, java, 2024) — `public class CANSparkMax extends CANSparkBase` · ⚠ removed in 2025.0.0; use com.revrobotics.spark.SparkMax

No confident result for FRC season 2026. Weak 2026 matches:

1. **Configuring a SPARK** — REVLib › SPARK › Configuration
   revlib 2026.0.0 · season 2026 · java · prose · trust: vendor · id `revlib/2026/spark-config#0`

   Since REVLib 2025, SPARK motor controllers are configured with configuration objects. Create a SparkMaxConfig, set parameters, then apply it with configure().

   ```java
   SparkMax max = new SparkMax(3, MotorType.kBrushless);
   SparkMaxConfig config = new SparkMaxConfig();
   config.smartCurrentLimit(40).idleMode(IdleMode.kBrake);
   max.configure(config, ResetMode.kResetSafeParameters, PersistMode.kPersistParameters);
   ```

   Source: https://docs.revrobotics.com/revlib/configuring-a-spark (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

Matches from OTHER seasons — do not use for the pinned season without migrating:

1. **CANSparkMax (legacy)** — REVLib 2024 › CANSparkMax
   revlib 2024.2.4 · season 2024 · java · prose · trust: vendor · exact symbol match · id `revlib/2024/cansparkmax#0`

   In REVLib 2024 the SPARK MAX is controlled with CANSparkMax: new CANSparkMax(3, MotorType.kBrushless); settings are applied with individual setters such as setSmartCurrentLimit(40) and burned with burnFlash().

   Source: https://docs.revrobotics.com/revlib/2024/cansparkmax (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

Next: the confident answer is in another season; confirm the project's season (pinned: 2026) and pass frc_season explicitly, or migrate the other-season API
