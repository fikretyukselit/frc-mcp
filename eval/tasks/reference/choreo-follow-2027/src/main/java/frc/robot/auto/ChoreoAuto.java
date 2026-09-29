package frc.robot.auto;

import choreo.Choreo;
import choreo.trajectory.SwerveSample;
import choreo.trajectory.Trajectory;
import java.util.Optional;
import org.wpilib.driverstation.Alliance;
import org.wpilib.driverstation.MatchState;
import org.wpilib.math.geometry.Pose2d;

/** Loads and samples the "leave" Choreo trajectory. */
public final class ChoreoAuto {
  private ChoreoAuto() {}

  /** Loads deploy/choreo/leave.traj; empty if the file is missing or not a swerve trajectory. */
  public static Optional<Trajectory<SwerveSample>> loadLeave() {
    return Choreo.loadTrajectory("leave");
  }

  /**
   * Returns the pose the trajectory wants at the given time, flipped for the red alliance.
   *
   * @param trajectory the loaded trajectory
   * @param timeSeconds time since the start of the trajectory, in seconds
   * @return the target pose, or empty if the trajectory has no samples
   */
  public static Optional<Pose2d> poseAt(Trajectory<SwerveSample> trajectory, double timeSeconds) {
    return trajectory.sampleAt(timeSeconds, isRedAlliance()).map(SwerveSample::getPose);
  }

  private static boolean isRedAlliance() {
    return MatchState.getAlliance().filter(alliance -> alliance == Alliance.RED).isPresent();
  }
}
