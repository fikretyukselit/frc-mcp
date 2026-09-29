package frc.robot;

import org.wpilib.math.geometry.Pose2d;
import org.wpilib.smartdashboard.Field2d;
import org.wpilib.telemetry.Telemetry;

/** Displays the robot (and an optional target) on a 2D field widget. */
public class FieldView {
  private final Field2d m_field = new Field2d();

  /** Moves the robot on the field widget. Call this every loop. */
  public void update(Pose2d robot) {
    m_field.setRobotPose(robot);
    Telemetry.log("Field", m_field);
  }

  /** Draws a second object named "Target" on the field widget. */
  public void showTarget(Pose2d target) {
    m_field.getObject("Target").setPose(target);
    Telemetry.log("Field", m_field);
  }
}
