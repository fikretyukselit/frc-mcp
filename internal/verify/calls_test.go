package verify_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

type row struct{ fqn, lib, season, lang, kind, sig, repl string }

// engineOf builds a one-shard engine from symbol rows.
func engineOf(t *testing.T, rows []row) *retrieve.Engine {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v.sqlite")
	w, err := index.Create(ctx, path, "v")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		s := index.Symbol{FQN: r.fqn, Library: r.lib, Version: r.season + ".0.1", Season: r.season, Language: r.lang, Kind: r.kind,
			Signature: r.sig, SourceURL: "https://example.org/" + r.fqn, UpstreamRev: "r", RetrievedAt: time.Unix(1, 0),
			License: "BSD-3-Clause", Trust: "official", Replacement: r.repl}
		if r.repl != "" {
			s.ReplacementSrc = "generated"
		}
		if err := w.AddSymbol(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	rd, err := index.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return retrieve.New([]*index.Reader{rd}, retrieve.Options{DefaultSeason: "2026"})
}

const talon = "com.ctre.phoenix6.hardware.TalonFX"

func phoenixRows() []row {
	return []row{
		{talon, "phoenix6", "2026", "java", "class", "public class TalonFX extends CoreTalonFX", ""},
		{talon + "#TalonFX", "phoenix6", "2026", "java", "constructor", "public TalonFX(int deviceId)", ""},
		{talon + "#TalonFX", "phoenix6", "2026", "java", "constructor", `@Deprecated(since="2026", forRemoval=true) public TalonFX(int deviceId, String canbus)`, ""},
		{talon + "#TalonFX", "phoenix6", "2026", "java", "constructor", "public TalonFX(int deviceId, CANBus canbus)", ""},
		{talon + "#setVoltage", "phoenix6", "2026", "java", "method", "public void setVoltage(double volts)", ""},
		{talon, "phoenix6", "2027", "java", "class", "public class TalonFX extends CoreTalonFX", ""},
		{talon + "#TalonFX", "phoenix6", "2027", "java", "constructor", "public TalonFX(int deviceId, CANBus canbus)", ""},
		{talon + "#setVoltage", "phoenix6", "2027", "java", "method", "public void setVoltage(double volts)", ""},
		{"com.ctre.phoenix6.hardware.core.CoreTalonFX", "phoenix6", "2027", "java", "class", "public class CoreTalonFX extends ParentDevice", ""},
		{"com.ctre.phoenix6.hardware.core.CoreTalonFX", "phoenix6", "2026", "java", "class", "public class CoreTalonFX extends ParentDevice", ""},
		{"com.ctre.phoenix6.hardware.ParentDevice", "phoenix6", "2027", "java", "class", "public abstract class ParentDevice", ""},
		{"com.ctre.phoenix6.hardware.ParentDevice", "phoenix6", "2026", "java", "class", "public abstract class ParentDevice", ""},
	}
}

func TestCallShapes(t *testing.T) {
	e := engineOf(t, phoenixRows())
	check := func(code, season string) verify.Result {
		return verify.Java(context.Background(), e, "import com.ctre.phoenix6.hardware.TalonFX;\nclass R {\n"+code+"\n}\n", season)
	}
	for _, tc := range []struct {
		name, code, season string
		errs, warns        int
	}{
		{"string canbus removed in 2027", `TalonFX m = new TalonFX(1, "canivore");`, "2027", 1, 0},
		{"id-only removed in 2027", `TalonFX m = new TalonFX(1);`, "2027", 1, 0},
		{"CANBus object fine in 2027", `TalonFX m = new TalonFX(1, bus);`, "2027", 0, 0},
		{"expression args are never typed", `TalonFX m = new TalonFX(id, name);`, "2027", 0, 0},
		{"2026 accepts all three", `TalonFX a = new TalonFX(1); TalonFX b = new TalonFX(2, bus);`, "2026", 0, 0},
		{"no season has a 3-arg constructor", `TalonFX m = new TalonFX(1, bus, 3);`, "2027", 0, 1},
		{"method literal type mismatch nowhere accepted", `void f(TalonFX m) { m.setVoltage("x"); }`, "2027", 0, 0},
		{"method fine", `void f(TalonFX m) { m.setVoltage(12); m.setVoltage(v * 2.0); }`, "2027", 0, 0},
		{"nested parens and lambdas", `TalonFX m = new TalonFX(calc(1, 2), bus.with(() -> { a(1, 2); }));`, "2027", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := check(tc.code, tc.season)
			shape := 0
			for _, f := range r.Findings {
				if f.Kind == "signature" || (f.Kind == "wrong_season" && f.Symbol == talon+"#TalonFX") {
					shape++
				}
			}
			if r.Errors != tc.errs || r.Warnings < tc.warns || (tc.warns == 0 && r.Warnings != 0) {
				t.Fatalf("errors=%d warnings=%d, want %d/%d: %+v", r.Errors, r.Warnings, tc.errs, tc.warns, r.Findings)
			}
			if tc.errs+tc.warns > 0 && shape == 0 {
				t.Fatalf("no call-shape finding: %+v", r.Findings)
			}
		})
	}
	// The 2027 error names the working overload.
	r := check(`TalonFX m = new TalonFX(1, "rio");`, "2027")
	if len(r.Findings) != 1 || r.Findings[0].Fix != "2027 overloads: TalonFX(int deviceId, CANBus canbus)" || r.Findings[0].Line != 3 {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestPythonAndCpp(t *testing.T) {
	e := engineOf(t, []row{
		{"wpimath.kinematics.ChassisSpeeds", "wpilib", "2026", "python", "class", "class ChassisSpeeds", "wpimath.ChassisVelocities"},
		{"wpimath.ChassisVelocities", "wpilib", "2027", "python", "class", "class ChassisVelocities", ""},
		{"wpimath.Pose2d", "wpilib", "2027", "python", "class", "class Pose2d", ""},
		{"wpimath.geometry.Pose2d", "wpilib", "2026", "python", "class", "class Pose2d", ""},
		{"wpilib.Timer", "wpilib", "2027", "python", "class", "class Timer", ""},
		{"wpilib.Timer#has_elapsed", "wpilib", "2027", "python", "method", "def has_elapsed(self, period: float) -> bool", ""},
		{"wpilib.Timer", "wpilib", "2026", "python", "class", "class Timer", ""},
		{"wpilib.Timer#hasElapsed", "wpilib", "2026", "python", "method", "def hasElapsed(self, period: float) -> bool", ""},
		{"wpilib.TimedRobot", "wpilib", "2027", "python", "class", "class TimedRobot(IterativeRobotBase)", ""},
		{"wpilib.IterativeRobotBase", "wpilib", "2027", "python", "class", "class IterativeRobotBase(RobotBase)", ""},
		{"wpilib.RobotBase", "wpilib", "2027", "python", "class", "class RobotBase", ""},
		{"wpilib.RobotBase#is_enabled", "wpilib", "2027", "python", "method", "def is_enabled(self) -> bool", ""},
		{"frc::ChassisSpeeds", "wpilib", "2026", "cpp", "class", "struct frc::ChassisSpeeds", "wpi::math::ChassisVelocities"},
		{"wpi::math::ChassisVelocities", "wpilib", "2027", "cpp", "class", "struct wpi::math::ChassisVelocities", ""},
		{"wpi::Timer", "wpilib", "2027", "cpp", "class", "class wpi::Timer", ""},
		{"wpi::Timer#Get", "wpilib", "2027", "cpp", "method", "units::second_t Get () const", ""},
	})
	ctx := context.Background()
	py := verify.Check(ctx, e, `import wpilib
from wpimath.kinematics import ChassisSpeeds
from wpimath.geometry import Pose2d

class Robot(wpilib.TimedRobot):
    def robotInit(self):
        self.t = wpilib.Timer()
        self.t.hasElapsed(1.0)
        self.t.has_elapsed(1.0)
        self.is_enabled()
`, "python", "2027", verify.Options{})
	if py.Errors != 2 || py.Warnings != 1 || !has(py, "error", "wrong_season", "wpimath.kinematics.ChassisSpeeds") ||
		!has(py, "error", "wrong_season", "Timer.hasElapsed") || !has(py, "warning", "wrong_season", "wpimath.geometry.Pose2d") {
		t.Fatalf("python: %+v", py.Findings)
	}
	if py.Coverage["wpilib"] == "" || py.Coverage["wpilib"][:7] != "partial" {
		t.Errorf("coverage %v", py.Coverage)
	}
	cpp := verify.Check(ctx, e, `#include <frc/kinematics/ChassisSpeeds.h>
frc::ChassisSpeeds s{};
auto t = wpi::Timer{}.Get();
ctre::phoenix6::hardware::TalonFX m{1};
`, "cpp", "2027", verify.Options{})
	if cpp.Errors != 1 || !has(cpp, "error", "wrong_season", "frc::ChassisSpeeds") || cpp.Findings[0].Fix != "use wpi::math::ChassisVelocities" {
		t.Fatalf("cpp: %+v", cpp.Findings)
	}
	if c := cpp.Coverage["phoenix6"]; c == "" || c[:4] != "none" {
		t.Errorf("phoenix6 cpp coverage = %q, want none", c)
	}
	// Clean code in its own season.
	if r := verify.Check(ctx, e, "frc::ChassisSpeeds s{};\n", "cpp", "2026", verify.Options{}); r.Errors+r.Warnings != 0 {
		t.Errorf("2026 cpp: %+v", r.Findings)
	}
}
