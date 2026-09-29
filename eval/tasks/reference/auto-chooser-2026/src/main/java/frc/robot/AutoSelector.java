package frc.robot;

import edu.wpi.first.wpilibj.smartdashboard.SendableChooser;
import edu.wpi.first.wpilibj.smartdashboard.SmartDashboard;
import edu.wpi.first.wpilibj2.command.Command;
import edu.wpi.first.wpilibj2.command.Commands;

/** Lets the drive team pick the autonomous routine from the dashboard. */
public class AutoSelector {
  private final SendableChooser<Command> m_chooser = new SendableChooser<>();

  public AutoSelector() {
    m_chooser.setDefaultOption("Do nothing", Commands.none());
    m_chooser.addOption("Taxi", Commands.print("taxi"));
    m_chooser.addOption("Two piece", Commands.print("two piece"));
    SmartDashboard.putData("Auto", m_chooser);
  }

  public Command getSelected() {
    return m_chooser.getSelected();
  }
}
