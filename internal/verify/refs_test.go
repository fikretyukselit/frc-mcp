package verify

import (
	"slices"
	"testing"
)

func syms(refs []Ref) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Symbol
	}
	return out
}

func TestRefs(t *testing.T) {
	for _, tc := range []struct {
		name, lang, code string
		want             []string
	}{
		{"java", "java", `import edu.wpi.first.wpilibj.Timer;
import static edu.wpi.first.units.Units.Meters;
import edu.wpi.first.math.geometry.*;
import java.util.List;
import frc.robot.Constants;
class A {
  // edu.wpi.first.wpilibj.InComment
  String s = "edu.wpi.first.wpilibj.InString";
  Timer t = new Timer();
  void f() { t.start(); Timer.getFPGATimestamp(); var x = edu.wpi.first.wpilibj.RobotController.getBatteryVoltage(); }
}`, []string{"edu.wpi.first.wpilibj.Timer", "edu.wpi.first.units.Units#Meters", "edu.wpi.first.wpilibj.Timer#start",
			"edu.wpi.first.wpilibj.Timer#getFPGATimestamp", "edu.wpi.first.wpilibj.RobotController"}},
		{"python", "python", `import wpilib
import wpimath.geometry
from wpimath.kinematics import (ChassisSpeeds,
    SwerveModuleState as SMS)
from os import path
import commands2 as c2

class Robot(wpilib.TimedRobot):
    def robotInit(self):
        # wpilib.InComment
        self.timer = wpilib.Timer()
        self.timer.start()
        s = ChassisSpeeds.fromFieldRelativeSpeeds(1, 0, 0, None)
        wpilib.SmartDashboard.putNumber("x", 1)
        c2.CommandScheduler.getInstance()
        p = wpimath.geometry.Pose2d()
`, []string{"wpimath.kinematics.ChassisSpeeds", "wpimath.kinematics.SwerveModuleState", "wpilib.TimedRobot",
			"wpilib.Timer", "wpilib.Timer#start", "wpimath.kinematics.ChassisSpeeds#fromFieldRelativeSpeeds",
			"wpilib.SmartDashboard", "wpilib.SmartDashboard#putNumber", "commands2.CommandScheduler",
			"commands2.CommandScheduler#getInstance", "wpimath.geometry.Pose2d"}},
		{"cpp", "cpp", `#include <frc/TimedRobot.h>
// frc::InComment
class Robot : public frc::TimedRobot {
  frc::ChassisSpeeds s = frc::ChassisSpeeds::FromFieldRelativeSpeeds(1_mps, 0_mps, 0_rad_per_s, r);
  units::meter_t d{1};
  double t = frc::Timer::GetFPGATimestamp().value();
  const char* x = "frc::InString";
};`, []string{"frc::TimedRobot", "frc::ChassisSpeeds", "frc::ChassisSpeeds#FromFieldRelativeSpeeds", "frc::Timer",
			"frc::Timer#GetFPGATimestamp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := syms(Refs(tc.code, tc.lang))
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Errorf("missing %s in %v", w, got)
				}
			}
			for _, g := range got {
				if !slices.Contains(tc.want, g) {
					t.Errorf("unexpected %s", g)
				}
			}
		})
	}
}

func TestDetectLanguage(t *testing.T) {
	for code, want := range map[string]string{
		"package frc.robot;\nimport edu.wpi.first.wpilibj.TimedRobot;": "java",
		"#include <frc/TimedRobot.h>\nint x;":                          "cpp",
		"photon::PhotonCamera camera{\"front\"};":                      "cpp",
		"ctre::phoenix6::hardware::TalonFX motor{1};":                  "cpp",
		"import wpilib\n\nclass Robot(wpilib.TimedRobot):\n  pass":     "python",
		"hello": "",
	} {
		if got := DetectLanguage(code); got != want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", code, got, want)
		}
	}
}
