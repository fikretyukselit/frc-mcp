package frc.robot;

import org.wpilib.math.estimator.SwerveDrivePoseEstimator;
import org.wpilib.math.geometry.Pose2d;
import org.wpilib.math.geometry.Rotation2d;
import org.wpilib.math.geometry.Translation2d;
import org.wpilib.math.kinematics.SwerveDriveKinematics;
import org.wpilib.math.kinematics.SwerveModulePosition;

/** Fuses swerve odometry and vision into a field-relative pose estimate. */
public class Localizer {
  private final SwerveDriveKinematics m_kinematics =
      new SwerveDriveKinematics(
          new Translation2d(0.3, 0.3), // front left
          new Translation2d(0.3, -0.3), // front right
          new Translation2d(-0.3, 0.3), // back left
          new Translation2d(-0.3, -0.3)); // back right

  private final SwerveDrivePoseEstimator m_estimator =
      new SwerveDrivePoseEstimator(
          m_kinematics,
          Rotation2d.ZERO,
          new SwerveModulePosition[] {
            new SwerveModulePosition(),
            new SwerveModulePosition(),
            new SwerveModulePosition(),
            new SwerveModulePosition()
          },
          Pose2d.ZERO);

  /** Updates the estimate with the latest gyro angle and module positions (FL, FR, BL, BR). */
  public void update(Rotation2d gyroAngle, SwerveModulePosition[] modules) {
    m_estimator.update(gyroAngle, modules);
  }

  /** Adds a vision pose measurement taken at the given FPGA timestamp in seconds. */
  public void addVision(Pose2d pose, double timestampSeconds) {
    m_estimator.addVisionMeasurement(pose, timestampSeconds);
  }

  public Pose2d getPose() {
    return m_estimator.getEstimatedPosition();
  }
}
