package retrieve

import (
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// A loose spelling of a type ("talonFX") still finds it; a constant or a
// member that only shares the letters ("TalonFX" vs MotorType#TALONFX) does not.
func TestOtherSymbol(t *testing.T) {
	for _, tc := range []struct {
		fqn, id string
		want    bool
	}{
		{"com.ctre.phoenix6.hardware.TalonFX", "talonFX", false},
		{"swervelib.motors.MotorType#TALONFX", "TalonFX", true},
		{"swervelib.motors.MotorType#talonFx", "TalonFX", true},
		{"com.ctre.phoenix6.hardware.TalonFX#setControl", "TalonFX#SetControl", false},
		{"edu.wpi.first.units.Units.RPM", "Rpm", true},
	} {
		if got := otherSymbol(index.Symbol{FQN: tc.fqn}, tc.id); got != tc.want {
			t.Errorf("otherSymbol(%s, %s) = %v, want %v", tc.fqn, tc.id, got, tc.want)
		}
	}
}
