package frc.robot;

import org.wpilib.command2.Command;
import org.wpilib.command2.Commands;

public class RobotContainer {
  public RobotContainer() {
    configureBindings();
  }

  private void configureBindings() {}

  public Command getAutonomousCommand() {
    return Commands.none();
  }
}
