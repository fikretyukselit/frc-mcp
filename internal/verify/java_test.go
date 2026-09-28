package verify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/testfixture"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

func findings(t *testing.T, code, season string) verify.Result {
	t.Helper()
	return verify.Java(context.Background(), testfixture.Engine(t), code, season)
}

func has(r verify.Result, severity, kind, symbol string) bool {
	for _, f := range r.Findings {
		if f.Severity == severity && f.Kind == kind && strings.Contains(f.Symbol, symbol) {
			return true
		}
	}
	return false
}

func TestWrongSeasonImportWithFix(t *testing.T) {
	code := "package frc.robot;\nimport edu.wpi.first.math.kinematics.SwerveDriveKinematics;\nclass R {}\n"
	r := findings(t, code, "2027")
	if r.Errors != 1 || !has(r, "error", "wrong_season", "edu.wpi.first.math.kinematics.SwerveDriveKinematics") {
		t.Fatalf("%+v", r)
	}
	if f := r.Findings[0]; f.Line != 2 || f.Fix != "use org.wpilib.math.kinematics.SwerveDriveKinematics" {
		t.Fatalf("line/fix: %+v", f)
	}
	// Same code is clean in its own season.
	if r := findings(t, code, "2026"); r.Errors != 0 || r.Warnings != 0 {
		t.Fatalf("2026: %+v", r)
	}
	// Reverse direction: a 2027 import in a 2026 project maps back.
	r = findings(t, "import org.wpilib.math.kinematics.SwerveDriveKinematics;\n", "2026")
	if r.Errors != 1 || r.Findings[0].Fix != "use edu.wpi.first.math.kinematics.SwerveDriveKinematics" {
		t.Fatalf("reverse: %+v", r)
	}
}

func TestCommentsAndStringsIgnored(t *testing.T) {
	code := `// import org.wpilib.math.kinematics.SwerveDriveKinematics;
/* org.wpilib.math.kinematics.SwerveDriveKinematics */
class R { String s = "org.wpilib.math.kinematics.SwerveDriveKinematics"; String t = """
org.wpilib.math.kinematics.SwerveDriveKinematics
"""; }`
	if r := findings(t, code, "2026"); len(r.Findings) != 0 {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestNoGuessWithoutCounterpart(t *testing.T) {
	// SmartDashboard exists in 2026 only and has no generated 2027 counterpart:
	// the verifier must not claim an error it cannot back with a fix.
	r := findings(t, "import edu.wpi.first.wpilibj.smartdashboard.SmartDashboard;\n", "2027")
	if r.Errors != 0 || !has(r, "warning", "wrong_season", "SmartDashboard") {
		t.Fatalf("%+v", r)
	}
}

func TestVendorImportsReportCoverage(t *testing.T) {
	r := findings(t, "import com.revrobotics.spark.SparkMax;\nimport com.ctre.phoenix6.hardware.TalonFX;\n", "2026")
	if len(r.Findings) != 0 || r.Coverage["revlib"] == "" || r.Coverage["phoenix6"] == "" {
		t.Fatalf("%+v", r)
	}
}

func TestUnknownIsInfoOnly(t *testing.T) {
	r := findings(t, "import edu.wpi.first.wpilibj.DoesNotExist;\n", "2026")
	if r.Errors != 0 || r.Warnings != 0 || !has(r, "info", "unknown", "DoesNotExist") {
		t.Fatalf("%+v", r)
	}
}

func FuzzJavaNeverPanics(f *testing.F) {
	f.Add("import static edu.wpi.first.units.Units.*;\nclass A { B b; void f(){ b.c(); \"x\\\"\"; } }")
	f.Add("/* unterminated")
	f.Add("\"\"\" unterminated text block")
	e := testfixture.Engine(f)
	f.Fuzz(func(t *testing.T, code string) {
		if len(code) > 1<<14 {
			return
		}
		r := verify.Java(context.Background(), e, code, "2026")
		for _, x := range r.Findings {
			if x.Line < 1 || x.Col < 1 {
				t.Fatalf("bad position %+v", x)
			}
		}
	})
}
