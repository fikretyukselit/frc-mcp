package frc.robot;

import org.wpilib.math.geometry.Rotation2d;
import org.wpilib.math.geometry.Translation2d;
import org.wpilib.math.kinematics.ChassisVelocities;
import org.wpilib.math.kinematics.SwerveDriveKinematics;
import org.wpilib.math.kinematics.SwerveModuleVelocity;

/** Swerve inverse kinematics for a four-module drivetrain with modules at the corners. */
public class SwerveMath {
  /** Maximum attainable module velocity in meters per second. */
  public static final double MAX_MODULE_VELOCITY = 4.5;

  private final SwerveDriveKinematics m_kinematics;

  /**
   * Creates the kinematics for a rectangular swerve drive.
   *
   * @param trackWidth distance between the left and right modules, in meters
   * @param wheelBase distance between the front and back modules, in meters
   */
  public SwerveMath(double trackWidth, double wheelBase) {
    double x = wheelBase / 2.0;
    double y = trackWidth / 2.0;
    m_kinematics =
        new SwerveDriveKinematics(
            new Translation2d(x, y), // front left
            new Translation2d(x, -y), // front right
            new Translation2d(-x, y), // back left
            new Translation2d(-x, -y)); // back right
  }

  /**
   * Converts a field-relative velocity into module targets.
   *
   * @param vx field-relative x velocity in m/s
   * @param vy field-relative y velocity in m/s
   * @param omega angular velocity in rad/s
   * @param headingRadians the robot's current heading in radians
   * @return module targets (FL, FR, BL, BR) desaturated to {@link #MAX_MODULE_VELOCITY}
   */
  public SwerveModuleVelocity[] toModuleTargets(
      double vx, double vy, double omega, double headingRadians) {
    ChassisVelocities robotRelative =
        new ChassisVelocities(vx, vy, omega).toRobotRelative(new Rotation2d(headingRadians));
    SwerveModuleVelocity[] targets = m_kinematics.toSwerveModuleVelocities(robotRelative);
    return SwerveDriveKinematics.desaturateWheelVelocities(targets, MAX_MODULE_VELOCITY);
  }

  public SwerveDriveKinematics getKinematics() {
    return m_kinematics;
  }
}
