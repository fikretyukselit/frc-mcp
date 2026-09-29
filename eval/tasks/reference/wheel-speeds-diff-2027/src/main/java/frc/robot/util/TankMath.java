package frc.robot.util;

import org.wpilib.math.kinematics.ChassisVelocities;
import org.wpilib.math.kinematics.DifferentialDriveKinematics;
import org.wpilib.math.kinematics.DifferentialDriveWheelVelocities;

/** Differential drive inverse kinematics. */
public final class TankMath {
  private static final double TRACK_WIDTH_METERS = 0.6;
  private static final double MAX_WHEEL_VELOCITY = 3.5;
  private static final DifferentialDriveKinematics kKinematics =
      new DifferentialDriveKinematics(TRACK_WIDTH_METERS);

  private TankMath() {}

  /**
   * Converts a robot velocity into wheel velocities.
   *
   * @param forward forward velocity in m/s
   * @param rotation angular velocity in rad/s (CCW positive)
   * @return left/right wheel velocities desaturated to 3.5 m/s
   */
  public static DifferentialDriveWheelVelocities toWheelVelocities(double forward, double rotation) {
    return kKinematics
        .toWheelVelocities(new ChassisVelocities(forward, 0.0, rotation))
        .desaturate(MAX_WHEEL_VELOCITY);
  }
}
