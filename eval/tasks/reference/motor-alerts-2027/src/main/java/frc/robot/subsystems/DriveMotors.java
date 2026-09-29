package frc.robot.subsystems;

import com.ctre.phoenix6.BaseStatusSignal;
import com.ctre.phoenix6.CANBus;
import com.ctre.phoenix6.StatusSignal;
import com.ctre.phoenix6.controls.DutyCycleOut;
import com.ctre.phoenix6.hardware.TalonFX;
import org.wpilib.hardware.bus.CANPort;
import org.wpilib.command2.SubsystemBase;
import org.wpilib.units.measure.AngularVelocity;
import org.wpilib.units.measure.Voltage;
import org.wpilib.util.Alert;
import org.wpilib.util.Alert.Level;

/** Two TalonFX drive motors with dashboard alerts when either one drops off the bus. */
public class DriveMotors extends SubsystemBase {
  private static final CANBus kBus = new CANBus(CANPort.CAN_S0);

  private final TalonFX m_left = new TalonFX(1, kBus);
  private final TalonFX m_right = new TalonFX(2, kBus);

  private final DutyCycleOut m_leftRequest = new DutyCycleOut(0.0);
  private final DutyCycleOut m_rightRequest = new DutyCycleOut(0.0);

  private final StatusSignal<AngularVelocity> m_leftVelocity = m_left.getVelocity();
  private final StatusSignal<Voltage> m_leftVoltage = m_left.getMotorVoltage();
  private final StatusSignal<AngularVelocity> m_rightVelocity = m_right.getVelocity();
  private final StatusSignal<Voltage> m_rightVoltage = m_right.getMotorVoltage();

  private final Alert m_leftDisconnected =
      new Alert("DriveMotors/LeftDisconnected", "Left drive TalonFX (CAN 1) disconnected", Level.HIGH);
  private final Alert m_rightDisconnected =
      new Alert(
          "DriveMotors/RightDisconnected", "Right drive TalonFX (CAN 2) disconnected", Level.HIGH);

  /** Sets the duty cycle of each side, in [-1, 1]. */
  public void drive(double left, double right) {
    m_left.setControl(m_leftRequest.withOutput(left));
    m_right.setControl(m_rightRequest.withOutput(right));
  }

  @Override
  public void periodic() {
    // Refresh the motors' status signals, then flag any motor that is no longer on the bus.
    BaseStatusSignal.refreshAll(m_leftVelocity, m_leftVoltage, m_rightVelocity, m_rightVoltage);
    m_leftDisconnected.set(!m_left.isConnected());
    m_rightDisconnected.set(!m_right.isConnected());
  }
}
