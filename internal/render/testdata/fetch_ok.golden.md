**frc_fetch** · status: ok · confidence: 1.00 · season: 2026 · language: java · source: shard · index age: 2h30m

## Motion Magic
Device API › Closed-Loop Control › Motion Magic
phoenix6 26.1.0 · season 2026 · java · prose · id `phoenix6/2026/motion-magic#0`

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
