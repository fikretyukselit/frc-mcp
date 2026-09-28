package apisym

import (
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func TestDiffMapsPackageMoves(t *testing.T) {
	old := []index.Symbol{
		{FQN: "edu.wpi.first.math.kinematics.SwerveDriveKinematics"},
		{FQN: "edu.wpi.first.math.kinematics.SwerveDriveKinematics#toSwerveModuleStates"},
		{FQN: "edu.wpi.first.wpilibj.smartdashboard.SmartDashboard"},
		{FQN: "edu.wpi.first.wpilibj.Timer"},
		{FQN: "edu.wpi.first.wpilibj.motorcontrol.MotorControllerGroup", Replacement: "PWMMotorController.addFollower(PWMMotorController)"},
	}
	nu := []index.Symbol{
		{FQN: "org.wpilib.math.kinematics.SwerveDriveKinematics"},
		{FQN: "org.wpilib.math.kinematics.SwerveDriveKinematics#toSwerveModuleStates"},
		{FQN: "org.wpilib.system.Timer"},
		{FQN: "org.wpilib.util.Timer"}, // ambiguous second candidate with a different package suffix
		{FQN: "org.wpilib.telemetry.Telemetry"},
	}
	removed, added, mapped := Diff(old, nu, "2027.0.0-alpha-7")
	if removed != 5 || added != 5 {
		t.Fatalf("removed=%d added=%d", removed, added)
	}
	want := map[string]string{
		"edu.wpi.first.math.kinematics.SwerveDriveKinematics":                      "org.wpilib.math.kinematics.SwerveDriveKinematics",
		"edu.wpi.first.math.kinematics.SwerveDriveKinematics#toSwerveModuleStates": "org.wpilib.math.kinematics.SwerveDriveKinematics#toSwerveModuleStates",
		"edu.wpi.first.wpilibj.smartdashboard.SmartDashboard":                      "", // no counterpart (replaced by Telemetry, curated later)
		"edu.wpi.first.wpilibj.Timer":                                              "", // ambiguous → no guess
		"edu.wpi.first.wpilibj.motorcontrol.MotorControllerGroup":                  "PWMMotorController.addFollower(PWMMotorController)",
	}
	for _, s := range old {
		if s.RemovedIn != "2027.0.0-alpha-7" || s.Replacement != want[s.FQN] {
			t.Errorf("%s: removed=%q repl=%q want %q", s.FQN, s.RemovedIn, s.Replacement, want[s.FQN])
		}
	}
	if mapped != 2 {
		t.Errorf("mapped = %d", mapped)
	}
	for _, s := range nu {
		if s.Since != "2027.0.0-alpha-7" {
			t.Errorf("%s: since=%q", s.FQN, s.Since)
		}
	}
}

func TestTypeName(t *testing.T) {
	for in, want := range map[string]string{
		"org.wpilib.hardware.led.AddressableLED.Buffer": "AddressableLED.Buffer",
		"edu.wpi.first.wpilibj.Timer":                   "Timer",
	} {
		if got := typeName(in); got != want {
			t.Errorf("typeName(%s)=%s", in, got)
		}
	}
}
