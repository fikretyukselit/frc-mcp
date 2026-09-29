package frc.robot.subsystems;

import org.wpilib.command2.SubsystemBase;
import org.wpilib.drivers.motor.PWMSparkMax;
import org.wpilib.hardware.rotation.Encoder;
import org.wpilib.math.controller.PIDController;
import org.wpilib.telemetry.Telemetry;
import org.wpilib.tunable.Tunables;

/** Elevator that holds a height setpoint with a dashboard-tunable PID controller. */
public class Elevator extends SubsystemBase {
  private static final double METERS_PER_PULSE = 0.01;
  private static final double TOLERANCE_METERS = 0.02;

  private final PWMSparkMax m_motor = new PWMSparkMax(0);
  private final Encoder m_encoder = new Encoder(0, 1);
  private final PIDController m_pid = new PIDController(4.0, 0.0, 0.1);

  public Elevator() {
    m_encoder.setDistancePerPulse(METERS_PER_PULSE);
    m_pid.setTolerance(TOLERANCE_METERS);
    m_pid.setSetpoint(0.0);
    // Exposes kP, kI and kD as tunables so they can be changed live from the dashboard.
    Tunables.publish("Elevator/PID", m_pid);
  }

  /** Sets the target height in meters. */
  public void setHeight(double meters) {
    m_pid.setSetpoint(meters);
  }

  /** Returns whether the elevator is at its height setpoint. */
  public boolean atSetpoint() {
    return m_pid.atSetpoint();
  }

  public double getHeight() {
    return m_encoder.getDistance();
  }

  @Override
  public void periodic() {
    m_motor.setThrottle(m_pid.calculate(getHeight()));
    Telemetry.log("Elevator/Height", getHeight());
    Telemetry.log("Elevator/Setpoint", m_pid.getSetpoint());
  }
}
