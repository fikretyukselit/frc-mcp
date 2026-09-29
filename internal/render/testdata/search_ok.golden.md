**frc_search** · status: ok · confidence: 0.90 · season: 2026 (default) · language: java · source: shard · index age: 2h30m

Exact API symbols:
- `com.ctre.phoenix6.hardware.TalonFX` (phoenix6 26.1.0, java, 2026) — `public class TalonFX extends CoreTalonFX implements MotorController`
- `com.ctre.phoenix6.hardware.TalonFX#TalonFX` (phoenix6 26.1.0, java, 2026) — `public TalonFX(int deviceId, CANBus canbus)`
- `com.ctre.phoenix6.hardware.TalonFX#TalonFX` (phoenix6 26.1.0, java, 2026) — `public TalonFX(int deviceId, String canbus)` · ⚠ deprecated in 26.0.0; use TalonFX(int deviceId, CANBus canbus)

1. **Motion Magic** — Device API › Closed-Loop Control › Motion Magic
   phoenix6 26.1.0 · season 2026 · java · prose · trust: vendor · id `phoenix6/2026/motion-magic#0`

   Motion Magic is a control mode that generates a trapezoidal or S-curve motion profile on the TalonFX. Configure cruise velocity, acceleration and jerk in TalonFXConfiguration.MotionMagic, then request MotionMagicVoltage.

   ```java
   var cfg = new TalonFXConfiguration();
   cfg.Slot0.kP = 60; cfg.Slot0.kV = 0.12;
   cfg.MotionMagic.MotionMagicCruiseVelocity = 80; // rps
   cfg.MotionMagic.MotionMagicAcceleration = 160; // rps/s
   talon.getConfigurator().apply(cfg);
   talon.setControl(new MotionMagicVoltage(0).withPosition(10));
   ```

   Source: https://v6.docs.ctr-electronics.com/en/latest/docs/api-reference/device-specific/talonfx/motion-magic.html (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

2. **New for 2026** — Yearly Changes › New for 2026 › CANBus
   phoenix6 26.1.0 · season 2026 · java · release · trust: vendor · id `phoenix6/2026/canbus#0`

   The <Device>(int id, String canbus) constructors are deprecated in 2026 and will be removed in 2027. Construct a CANBus object and pass it to device constructors instead: new TalonFX(1, new CANBus("canivore")).

   Source: https://v6.docs.ctr-electronics.com/en/latest/docs/yearly-changes/yearly-changelog.html (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

3. **Configuring a SPARK** — REVLib › SPARK › Configuration
   revlib 2026.0.0 · season 2026 · java · prose · trust: vendor · id `revlib/2026/spark-config#0`

   Since REVLib 2025, SPARK motor controllers are configured with configuration objects. Create a SparkMaxConfig, set parameters, then apply it with configure().

   ```java
   SparkMax max = new SparkMax(3, MotorType.kBrushless);
   SparkMaxConfig config = new SparkMaxConfig();
   config.smartCurrentLimit(40).idleMode(IdleMode.kBrake);
   max.configure(config, ResetMode.kResetSafeParameters, PersistMode.kPersistParameters);
   ```

   Source: https://docs.revrobotics.com/revlib/configuring-a-spark (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

4. **Build an Auto** — PathPlannerLib › Build an Auto › AutoBuilder
   pathplannerlib 2026.1.2 · season 2026 · java · prose · trust: vendor · id `pathplanner/2026/autobuilder#0`

   Configure AutoBuilder once in your drive subsystem constructor with pose supplier, reset pose consumer, robot-relative speeds supplier, a drive output consumer, a PPHolonomicDriveController, the RobotConfig loaded from the GUI settings, and an alliance flip supplier.

   Source: https://pathplanner.dev/pplib-build-an-auto.html (rev fixture-rev, retrieved 2026-09-28T12:00:00Z, license: fixture (illustrative, not authoritative))

Next: frc_fetch {"id": "phoenix6/2026/motion-magic#0"} for the full section
Next: frc_api {"symbol": "com.ctre.phoenix6.hardware.TalonFX"} for exact signatures
