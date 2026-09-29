package frc.robot;

import org.wpilib.framework.RobotBase;

/** Do not add static variables or initialization here; use {@link Robot} instead. */
public final class Main {
  private Main() {}

  public static void main(String... args) {
    RobotBase.startRobot(Robot::new);
  }
}
