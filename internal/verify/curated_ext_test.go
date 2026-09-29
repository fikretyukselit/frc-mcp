package verify_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

// No REVLib or Phoenix 6 table is indexed (as on a hosted server): curated
// rules still catch names that are gone.
func TestCuratedChecksWithoutTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.sqlite")
	w, err := index.Create(ctx, path, "c")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddSymbol(ctx, index.Symbol{FQN: "edu.wpi.first.wpilibj.TimedRobot", Library: "wpilib", Version: "2026.2.2", Season: "2026",
		Language: "java", Kind: "class", Signature: "public class TimedRobot", SourceURL: "https://example.org/t", UpstreamRev: "r",
		RetrievedAt: time.Unix(1, 0), License: "BSD-3-Clause", Trust: "official"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []index.Migration{
		{RuleID: "cansparkmax", Library: "revlib", Language: "java", FromSeason: "2024", ToSeason: "2025", Kind: "rename",
			From: "com.revrobotics.CANSparkMax", To: "com.revrobotics.spark.SparkMax", Citation: "https://example.org/rev", Verified: true},
		{RuleID: "cansparkmax", Library: "revlib", Language: "python", FromSeason: "2024", ToSeason: "2025", Kind: "rename",
			From: "rev.CANSparkMax", To: "rev.SparkMax", Citation: "https://example.org/rev", Verified: true},
		{RuleID: "canbus", Library: "phoenix6", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "signature",
			From: talon + "#TalonFX", To: talon + "#TalonFX", Notes: "Pass a CANBus.", Citation: "https://example.org/ctre", Verified: true},
	} {
		if err := w.AddMigration(ctx, m); err != nil {
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
	defer rd.Close()
	r := retrieve.New([]*index.Reader{rd}, retrieve.Options{DefaultSeason: "2026"})

	res := verify.Check(ctx, r, "import com.revrobotics.CANSparkMax;\nimport com.ctre.phoenix6.hardware.TalonFX;\nclass A {}\n", "java", "2026", verify.Options{})
	if len(res.Findings) != 1 || res.Findings[0].Symbol != "com.revrobotics.CANSparkMax" || res.Findings[0].Severity != "error" ||
		!strings.Contains(res.Findings[0].Fix, "com.revrobotics.spark.SparkMax") || res.Findings[0].Line != 1 {
		t.Fatalf("java: %+v", res.Findings)
	}
	if c := res.Coverage["revlib"]; !strings.Contains(c, "REV Robotics grants no redistribution license") {
		t.Errorf("coverage: %q", c)
	}
	res = verify.Check(ctx, r, "import rev\nm = rev.CANSparkMax(1, rev.CANSparkMax.MotorType.kBrushless)\n", "python", "2026", verify.Options{})
	if len(res.Findings) != 1 || res.Findings[0].Symbol != "rev.CANSparkMax" {
		t.Fatalf("python: %+v", res.Findings)
	}
	// The change's own season: the old name may linger deprecated.
	res = verify.Check(ctx, r, "import com.revrobotics.CANSparkMax;\n", "java", "2025", verify.Options{})
	if len(res.Findings) != 1 || res.Findings[0].Severity != "warning" {
		t.Fatalf("season of the change: %+v", res.Findings)
	}
	// A qualified reference, not imported, is checked too.
	res = verify.Check(ctx, r, "class A {\n  com.revrobotics.CANSparkMax m;\n}\n", "java", "2026", verify.Options{})
	if len(res.Findings) != 1 || res.Findings[0].Line != 2 || res.Coverage["revlib"] == "" {
		t.Fatalf("qualified: %+v %v", res.Findings, res.Coverage)
	}
	// A current name is fine.
	if res := verify.Check(ctx, r, "import com.revrobotics.spark.SparkMax;\n", "java", "2026", verify.Options{}); len(res.Findings) != 0 {
		t.Fatalf("current name flagged: %+v", res.Findings)
	}
}
