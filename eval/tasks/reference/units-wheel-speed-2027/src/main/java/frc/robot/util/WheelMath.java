package frc.robot.util;

import static org.wpilib.units.Units.Meters;
import static org.wpilib.units.Units.MetersPerSecond;
import static org.wpilib.units.Units.RadiansPerSecond;

import org.wpilib.units.measure.AngularVelocity;
import org.wpilib.units.measure.Distance;
import org.wpilib.units.measure.LinearVelocity;

/** Wheel angular-to-linear velocity conversions using the WPILib units library. */
public final class WheelMath {
  private WheelMath() {}

  /** Linear surface velocity of a wheel: v = omega * r. */
  public static LinearVelocity linearVelocity(AngularVelocity wheelVelocity, Distance wheelDiameter) {
    double radiusMeters = wheelDiameter.in(Meters) / 2.0;
    return MetersPerSecond.of(wheelVelocity.in(RadiansPerSecond) * radiusMeters);
  }

  /** Same as {@link #linearVelocity}, in meters per second. */
  public static double linearVelocityMetersPerSecond(
      AngularVelocity wheelVelocity, Distance wheelDiameter) {
    return linearVelocity(wheelVelocity, wheelDiameter).in(MetersPerSecond);
  }
}
