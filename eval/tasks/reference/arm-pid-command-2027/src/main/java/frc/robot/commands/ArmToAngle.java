package frc.robot.commands;

import java.util.function.DoubleConsumer;
import java.util.function.DoubleSupplier;
import org.wpilib.command2.Command;
import org.wpilib.command2.Subsystem;
import org.wpilib.math.controller.PIDController;

/** Drives an arm to a target angle with PID and finishes at the setpoint. */
public class ArmToAngle extends Command {
  private final DoubleSupplier m_angleRad;
  private final DoubleConsumer m_setVoltage;
  private final PIDController m_controller = new PIDController(5.0, 0.0, 0.2);

  public ArmToAngle(
      DoubleSupplier angleRad, DoubleConsumer setVoltage, double targetRad, Subsystem arm) {
    m_angleRad = angleRad;
    m_setVoltage = setVoltage;
    m_controller.setSetpoint(targetRad);
    m_controller.setTolerance(0.02);
    addRequirements(arm);
  }

  @Override
  public void initialize() {
    m_controller.reset();
  }

  @Override
  public void execute() {
    m_setVoltage.accept(m_controller.calculate(m_angleRad.getAsDouble()));
  }

  @Override
  public void end(boolean interrupted) {
    m_setVoltage.accept(0.0);
  }

  @Override
  public boolean isFinished() {
    return m_controller.atSetpoint();
  }
}
