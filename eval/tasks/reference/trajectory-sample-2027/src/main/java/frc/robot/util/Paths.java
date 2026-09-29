package frc.robot.util;

import java.util.List;
import org.wpilib.math.geometry.Pose2d;
import org.wpilib.math.geometry.Rotation2d;
import org.wpilib.math.geometry.Translation2d;
import org.wpilib.math.trajectory.DrivetrainSplineTrajectory;
import org.wpilib.math.trajectory.DrivetrainSplineTrajectoryGenerator;
import org.wpilib.math.trajectory.TrajectoryConfig;

/** Pre-generated spline trajectories. */
public final class Paths {
  private static final DrivetrainSplineTrajectory kPath = generate();

  private Paths() {}

  /** Generates the (0, 0, 0°) -> (1.5, 0.5) -> (3, 1, 0°) trajectory at 3 m/s and 2 m/s². */
  public static DrivetrainSplineTrajectory generate() {
    return DrivetrainSplineTrajectoryGenerator.generate(
        new Pose2d(0.0, 0.0, Rotation2d.fromDegrees(0.0)),
        List.of(new Translation2d(1.5, 0.5)),
        new Pose2d(3.0, 1.0, Rotation2d.fromDegrees(0.0)),
        new TrajectoryConfig(3.0, 2.0));
  }

  /** Samples the trajectory's pose at the given time since its start. */
  public static Pose2d poseAt(double seconds) {
    return kPath.sampleAt(seconds).pose;
  }
}
