package frc.robot;

import org.littletonrobotics.junction.networktables.LoggedNetworkChooser;
import org.wpilib.command2.Command;
import org.wpilib.command2.Commands;

/** Autonomous chooser whose selection is logged and replayed by AdvantageKit. */
public class AutoRoutines {
  private final LoggedNetworkChooser<Command> m_chooser = new LoggedNetworkChooser<>("Auto Choices");

  public AutoRoutines(Command leave) {
    m_chooser.addDefault("None", Commands.none());
    m_chooser.add("Leave", leave);
  }

  /** Returns the routine selected on the dashboard (or the replayed selection). */
  public Command get() {
    return m_chooser.get();
  }
}
