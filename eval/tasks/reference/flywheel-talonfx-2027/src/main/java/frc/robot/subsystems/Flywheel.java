package frc.robot.subsystems;

import com.ctre.phoenix6.CANBus;
import com.ctre.phoenix6.StatusSignal;
import com.ctre.phoenix6.configs.TalonFXConfiguration;
import com.ctre.phoenix6.controls.VelocityVoltage;
import com.ctre.phoenix6.hardware.TalonFX;
import com.ctre.phoenix6.signals.NeutralModeValue;
import org.wpilib.command2.SubsystemBase;
import org.wpilib.units.measure.AngularVelocity;

/** Shooter flywheel driven by a single Kraken X60 using on-controller velocity control. */
public class Flywheel extends SubsystemBase {
  private static final CANBus kCANivore = new CANBus("canivore");

  private final TalonFX m_motor = new TalonFX(5, kCANivore);
  private final VelocityVoltage m_velocityRequest = new VelocityVoltage(0.0).withSlot(0);
  private final StatusSignal<AngularVelocity> m_velocity = m_motor.getVelocity();

  public Flywheel() {
    TalonFXConfiguration config = new TalonFXConfiguration();
    config.Slot0.kS = 0.1;
    config.Slot0.kV = 0.12;
    config.Slot0.kP = 0.2;
    config.MotorOutput.NeutralMode = NeutralModeValue.Coast;
    m_motor.getConfigurator().apply(config);
  }

  /** Commands the flywheel to the given velocity in rotations per minute. */
  public void setRPM(double rpm) {
    m_motor.setControl(m_velocityRequest.withVelocity(rpm / 60.0));
  }

  /** Returns the flywheel velocity in rotations per minute. */
  public double getRPM() {
    return m_velocity.refresh().getValueAsDouble() * 60.0;
  }

  /** Returns whether the flywheel is within {@code toleranceRpm} of the commanded velocity. */
  public boolean atSpeed(double toleranceRpm) {
    return Math.abs(getRPM() - m_velocityRequest.Velocity * 60.0) <= toleranceRpm;
  }
}
