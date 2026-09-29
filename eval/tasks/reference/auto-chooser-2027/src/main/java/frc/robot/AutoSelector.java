package frc.robot;

import org.wpilib.command2.Command;
import org.wpilib.command2.Commands;
import org.wpilib.tunable.Selectable;
import org.wpilib.tunable.Tunables;

/** Lets the drive team pick the autonomous routine from the dashboard. */
public class AutoSelector {
  private final Selectable<Command> m_chooser = new Selectable<>();

  public AutoSelector() {
    m_chooser.addDefault("Do nothing", Commands.none());
    m_chooser.add("Taxi", Commands.print("taxi"));
    m_chooser.add("Two piece", Commands.print("two piece"));
    Tunables.publish("Auto", m_chooser);
  }

  public Command getSelected() {
    return m_chooser.getSelected();
  }
}
