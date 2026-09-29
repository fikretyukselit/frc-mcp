package frc.robot;

import edu.wpi.first.math.geometry.Rotation2d;
import edu.wpi.first.math.geometry.Translation2d;
import edu.wpi.first.math.kinematics.ChassisSpeeds;
import edu.wpi.first.math.kinematics.SwerveDriveKinematics;
import edu.wpi.first.math.kinematics.SwerveModuleState;

/** Swerve inverse kinematics for a four-module drivetrain with modules at the corners. */
public class SwerveMath {
  /** Maximum attainable module speed in meters per second. */
  public static final double MAX_MODULE_SPEED = 4.5;

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
   * Converts a field-relative velocity into module states.
   *
   * @param vx field-relative x velocity in m/s
   * @param vy field-relative y velocity in m/s
   * @param omega angular velocity in rad/s
   * @param headingRadians the robot's current heading in radians
   * @return module states (FL, FR, BL, BR) desaturated to {@link #MAX_MODULE_SPEED}
   */
  public SwerveModuleState[] toModuleStates(
      double vx, double vy, double omega, double headingRadians) {
    ChassisSpeeds robotRelative =
        ChassisSpeeds.fromFieldRelativeSpeeds(vx, vy, omega, new Rotation2d(headingRadians));
    SwerveModuleState[] states = m_kinematics.toSwerveModuleStates(robotRelative);
    SwerveDriveKinematics.desaturateWheelSpeeds(states, MAX_MODULE_SPEED);
    return states;
  }

  public SwerveDriveKinematics getKinematics() {
    return m_kinematics;
  }
}
