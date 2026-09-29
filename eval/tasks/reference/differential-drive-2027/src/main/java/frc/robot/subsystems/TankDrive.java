package frc.robot.subsystems;

import java.util.function.DoubleSupplier;
import org.wpilib.command2.Command;
import org.wpilib.command2.SubsystemBase;
import org.wpilib.drive.DifferentialDrive;
import org.wpilib.drivers.motor.PWMSparkMax;

/** Differential drivetrain with one PWM Spark MAX per side. */
public class TankDrive extends SubsystemBase {
  private final PWMSparkMax m_left = new PWMSparkMax(0);
  private final PWMSparkMax m_right = new PWMSparkMax(1);
  private final DifferentialDrive m_drive;

  public TankDrive() {
    m_right.setInverted(true);
    m_drive = new DifferentialDrive(m_left, m_right);
  }

  /** Arcade-drives from the given inputs until interrupted. */
  public Command arcade(DoubleSupplier forward, DoubleSupplier turn) {
    return run(() -> m_drive.arcadeDrive(forward.getAsDouble(), turn.getAsDouble()))
        .finallyDo(m_drive::stopMotor)
        .withName("Arcade");
  }
}
