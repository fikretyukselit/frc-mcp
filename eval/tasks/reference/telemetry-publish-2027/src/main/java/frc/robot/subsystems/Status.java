package frc.robot.subsystems;

import org.wpilib.command2.SubsystemBase;
import org.wpilib.driverstation.MatchState;
import org.wpilib.driverstation.RobotState;
import org.wpilib.math.geometry.Pose2d;
import org.wpilib.system.RobotController;
import org.wpilib.telemetry.Telemetry;
import org.wpilib.telemetry.TelemetryTable;

/** Publishes robot status to the dashboard every loop. */
public class Status extends SubsystemBase {
  private final TelemetryTable m_table = Telemetry.getTable("Status");
  private Pose2d m_pose = Pose2d.ZERO;

  public void setPose(Pose2d pose) {
    m_pose = pose;
  }

  @Override
  public void periodic() {
    m_table.log("Battery Voltage", RobotController.getBatteryVoltage());
    m_table.log("Match Time", MatchState.getMatchTime());
    m_table.log("Enabled", RobotState.isEnabled());
    m_table.log("Pose", m_pose, Pose2d.struct);
  }
}
