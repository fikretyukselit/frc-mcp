package frc.robot;

import org.wpilib.command2.Command;
import org.wpilib.command2.button.CommandXboxController;

/** Driver button bindings. */
public final class Bindings {
  private static final CommandXboxController kDriver = new CommandXboxController(0);

  private Bindings() {}

  public static void configure(Command intake, Command shoot, Command stow) {
    kDriver.a().whileTrue(intake);
    kDriver.b().onTrue(stow);
    kDriver.rightTrigger(0.5).whileTrue(shoot);
  }
}
