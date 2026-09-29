package router

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestDecide(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want Decision
	}{
		{"How do I configure Motion Magic on a TalonFX?",
			Decision{Intent: IntentHowTo, Identifiers: []string{"TalonFX"}, Libraries: []string{"phoenix6"}}},
		{"TalonFXConfiguration",
			Decision{Intent: IntentSymbol, Identifiers: []string{"TalonFXConfiguration"}}},
		{"edu.wpi.first.math.kinematics.SwerveDriveKinematics",
			Decision{Intent: IntentSymbol, Identifiers: []string{"edu.wpi.first.math.kinematics.SwerveDriveKinematics"},
				Libraries: []string{"wpilib"}, Language: "java"}},
		{"frc::SwerveDriveKinematics ToSwerveModuleStates",
			Decision{Intent: IntentSymbol, Identifiers: []string{"frc::SwerveDriveKinematics", "ToSwerveModuleStates"},
				Libraries: []string{"wpilib"}, Language: "cpp"}},
		{"CANSparkMax cannot find symbol after updating to 2025",
			Decision{Intent: IntentTroubleshoot, Identifiers: []string{"CANSparkMax"}, Libraries: []string{"revlib"}, Season: "2025"}},
		{"phoenix 5 vs phoenix 6 current limits",
			Decision{Intent: IntentGeneral, Libraries: []string{"phoenix5"}}},
		{"swerve odometry with photonvision in python",
			Decision{Intent: IntentGeneral, Libraries: []string{"photonvision"}, Language: "python"}},
		{"TalonFX nasıl ayarlanır?",
			Decision{Intent: IntentSymbol, Identifiers: []string{"TalonFX"}, Libraries: []string{"phoenix6"}, NonEnglish: true}},
		{"what is a subsystem",
			Decision{Intent: IntentHowTo}},
		// Source seasons do not pin the query.
		{"migrate REVLib 2024 code to the current API",
			Decision{Intent: IntentGeneral, Identifiers: []string{"REVLib"}, Libraries: []string{"revlib"}}},
		{"REVLib 2024 or older configuration",
			Decision{Intent: IntentGeneral, Identifiers: []string{"REVLib"}, Libraries: []string{"revlib"}}},
		{"migrate from 2026 to 2027",
			Decision{Intent: IntentGeneral, Season: "2027"}},
		{"closed loop on a spark max",
			Decision{Intent: IntentGeneral, Libraries: []string{"revlib"}}},
	} {
		got := Decide(tc.q)
		if d := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); d != "" {
			t.Errorf("Decide(%q) mismatch (-want +got):\n%s", tc.q, d)
		}
	}
}

func BenchmarkDecide(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Decide("How do I configure Motion Magic on a TalonFX with Phoenix 6 in 2026?")
	}
}
