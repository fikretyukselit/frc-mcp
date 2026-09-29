package frc.robot.commands;

import org.wpilib.command2.Command;
import org.wpilib.command2.Commands;
import org.wpilib.driverstation.GenericHID.RumbleType;
import org.wpilib.driverstation.MatchState;
import org.wpilib.driverstation.RobotState;
import org.wpilib.driverstation.XboxController;

/** Rumbles the driver's controller when endgame starts. */
public final class EndgameRumble {
  private static final double ENDGAME_SECONDS = 20.0;

  private EndgameRumble() {}

  /** Waits for teleop with 20 s or less left, then rumbles the driver controller for 1 s. */
  public static Command create() {
    XboxController driver = new XboxController(0);
    return Commands.waitUntil(
            () -> {
              double matchTime = MatchState.getMatchTime();
              return RobotState.isTeleopEnabled() && matchTime >= 0 && matchTime <= ENDGAME_SECONDS;
            })
        .andThen(
            Commands.startEnd(() -> setRumble(driver, 1.0), () -> setRumble(driver, 0.0))
                .withTimeout(1.0))
        .withName("EndgameRumble");
  }

  private static void setRumble(XboxController controller, double strength) {
    controller.setRumble(RumbleType.LEFT_RUMBLE, strength);
    controller.setRumble(RumbleType.RIGHT_RUMBLE, strength);
  }
}
