package frc.robot.commands;

import edu.wpi.first.math.controller.PIDController;
import edu.wpi.first.wpilibj2.command.Command;
import edu.wpi.first.wpilibj2.command.Subsystem;
import java.util.function.DoubleConsumer;
import java.util.function.DoubleSupplier;

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
