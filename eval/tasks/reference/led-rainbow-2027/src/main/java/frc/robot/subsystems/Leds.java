package frc.robot.subsystems;

import static org.wpilib.units.Units.Meters;
import static org.wpilib.units.Units.MetersPerSecond;

import org.wpilib.command2.Command;
import org.wpilib.command2.SubsystemBase;
import org.wpilib.hardware.led.AddressableLED;
import org.wpilib.hardware.led.AddressableLEDBuffer;
import org.wpilib.hardware.led.LEDPattern;
import org.wpilib.units.measure.Distance;
import org.wpilib.util.Color;

/** A 60-LED addressable strip that scrolls a rainbow unless another pattern is requested. */
public class Leds extends SubsystemBase {
  private static final int LENGTH = 60;
  /** Density of the strip: 120 LEDs per meter. */
  private static final Distance LED_SPACING = Meters.of(1.0 / 120.0);

  private final AddressableLED m_led = new AddressableLED(0);
  private final AddressableLEDBuffer m_buffer = new AddressableLEDBuffer(LENGTH);

  private final LEDPattern m_rainbow =
      LEDPattern.rainbow(255, 128).scrollAtAbsoluteVelocity(MetersPerSecond.of(1.0), LED_SPACING);

  public Leds() {
    m_led.setLength(LENGTH);
    m_led.setData(m_buffer);
    setDefaultCommand(runPattern(m_rainbow).ignoringDisable(true).withName("Rainbow"));
  }

  /** Shows a solid color while the returned command runs. */
  public Command solid(Color color) {
    return runPattern(LEDPattern.solid(color)).withName("Solid " + color);
  }

  private Command runPattern(LEDPattern pattern) {
    return run(() -> pattern.applyTo(m_buffer));
  }

  @Override
  public void periodic() {
    m_led.setData(m_buffer);
  }
}
