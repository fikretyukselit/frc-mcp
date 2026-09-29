package frc.robot.subsystems;

import com.revrobotics.PersistMode;
import com.revrobotics.RelativeEncoder;
import com.revrobotics.ResetMode;
import com.revrobotics.spark.FeedbackSensor;
import com.revrobotics.spark.SparkClosedLoopController;
import com.revrobotics.spark.SparkLowLevel.ControlType;
import com.revrobotics.spark.SparkLowLevel.MotorType;
import com.revrobotics.spark.SparkMax;
import com.revrobotics.spark.config.SparkBaseConfig.IdleMode;
import com.revrobotics.spark.config.SparkMaxConfig;
import org.wpilib.command2.SubsystemBase;
import org.wpilib.hardware.bus.CANPort;

/** Arm driven by a NEO on a SPARK MAX, using the SPARK's on-board position PID. */
public class Arm extends SubsystemBase {
  private final SparkMax m_motor = new SparkMax(CANPort.CAN_S0, 9, MotorType.kBrushless);
  private final RelativeEncoder m_encoder = m_motor.getEncoder();
  private final SparkClosedLoopController m_controller = m_motor.getClosedLoopController();

  public Arm() {
    SparkMaxConfig config = new SparkMaxConfig();
    config.idleMode(IdleMode.kBrake).smartCurrentLimit(40);
    config.closedLoop.feedbackSensor(FeedbackSensor.kPrimaryEncoder).p(0.8);
    m_motor.configure(config, ResetMode.kResetSafeParameters, PersistMode.kPersistParameters);
  }

  /** Moves the arm to the given angle, in motor rotations, with closed-loop position control. */
  public void setAngleRotations(double rotations) {
    m_controller.setSetpoint(rotations, ControlType.kPosition);
  }

  /** Returns the arm angle in motor rotations from the built-in encoder. */
  public double getAngleRotations() {
    return m_encoder.getPosition().get();
  }
}
