package dcmotor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

const java = `
  /**
   * Return a gearbox of NEO brushless motors.
   *
   * @param numMotors Number of motors in the gearbox.
   * @return a gearbox of NEO motors.
   */
  public static DCMotor getNEO(int numMotors) {
    return new DCMotor(
        12, 2.6, 105, 1.8, Units.rotationsPerMinuteToRadiansPerSecond(5676), numMotors);
  }

  /**
   * Return a gearbox of Kraken X60 brushless motors with FOC (Field-Oriented Control) enabled.
   *
   * @param numMotors Number of motors in the gearbox.
   * @return A gearbox of Kraken X60 FOC enabled motors.
   */
  public static DCMotor getKrakenX60Foc(int numMotors) {
    // From https://store.ctr-electronics.com/announcing-kraken-x60/
    return new DCMotor(
        12, 9.37, 483, 2, Units.rotationsPerMinuteToRadiansPerSecond(5800), numMotors);
  }
`

func TestParse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "DCMotor.java")
	if err := os.WriteFile(p, []byte(java), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []index.HWSpec
	src := sources.Source{Season: "2026", License: "BSD-3-Clause", Trust: "official", BaseURL: "https://github.com/x/DCMotor.java", URL: "https://raw/x"}
	st, err := Parse(p, src, "v2026.2.2", time.Unix(0, 0), func(h index.HWSpec) error { got = append(got, h); return nil })
	if err != nil || st.Motors != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	n, k := got[0], got[1]
	if n.Part != "neo" || n.Name != "NEO" || n.Fields["stall_torque_nm"] != 2.6 || n.Fields["free_speed_rpm"] != 5676 || n.Factory != "getNEO" {
		t.Errorf("neo %+v", n)
	}
	if k.Part != "krakenx60-foc" || k.Fields["stall_current_a"] != 483 || k.Note != "From https://store.ctr-electronics.com/announcing-kraken-x60/" {
		t.Errorf("kraken %+v", k)
	}
}
