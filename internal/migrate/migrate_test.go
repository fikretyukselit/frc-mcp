package migrate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

func sym(fqn, lib, season, lang string, mut ...func(*index.Symbol)) index.Symbol {
	s := index.Symbol{FQN: fqn, Library: lib, Version: season + ".1.0", Season: season, Language: lang, Kind: "class",
		Signature: "class " + index.SimpleName(fqn), SourceURL: "https://example.org/" + fqn, UpstreamRev: "r1",
		RetrievedAt: time.Unix(1, 0), License: "BSD-3-Clause", Trust: "official"}
	for _, m := range mut {
		m(&s)
	}
	return s
}

func repl(to, src string) func(*index.Symbol) {
	return func(s *index.Symbol) { s.Replacement, s.ReplacementSrc, s.RemovedIn = to, src, "2027.0.0" }
}

const (
	cs     = "edu.wpi.first.math.kinematics.ChassisSpeeds"
	cv     = "org.wpilib.math.kinematics.ChassisVelocities"
	sdk26  = "edu.wpi.first.math.kinematics.SwerveDriveKinematics"
	sdk27  = "org.wpilib.math.kinematics.SwerveDriveKinematics"
	dash   = "edu.wpi.first.wpilibj.smartdashboard.SmartDashboard"
	timer  = "edu.wpi.first.wpilibj.Timer"
	timer7 = "org.wpilib.system.Timer"
	talon  = "com.ctre.phoenix6.hardware.TalonFX"
)

func testEngine(t *testing.T) *retrieve.Engine {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.sqlite")
	w, err := index.Create(ctx, path, "m")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []index.Symbol{
		sym(cs, "wpilib", "2026", "java", repl("", "")),
		sym(cs+"#fromFieldRelativeSpeeds", "wpilib", "2026", "java", func(s *index.Symbol) { s.Kind = "method" }),
		sym(cv, "wpilib", "2027", "java"),
		sym(cv+"#toRobotRelative", "wpilib", "2027", "java", func(s *index.Symbol) { s.Kind = "method" }),
		sym(sdk26, "wpilib", "2026", "java", repl(sdk27, "generated")),
		sym(sdk27, "wpilib", "2027", "java"),
		sym(dash, "wpilib", "2026", "java"),
		sym(timer, "wpilib", "2026", "java", repl(timer7, "generated")),
		sym(timer7, "wpilib", "2027", "java"),
		sym("edu.wpi.first.wpilibj.Gone", "wpilib", "2026", "java"),
		sym(talon, "phoenix6", "2026", "java"),
		sym(talon+"#TalonFX", "phoenix6", "2026", "java", func(s *index.Symbol) { s.Kind = "constructor" }),
		sym(talon, "phoenix6", "2027", "java"),
		sym(talon+"#TalonFX", "phoenix6", "2027", "java", func(s *index.Symbol) { s.Kind = "constructor" }),
		sym("wpimath.kinematics.ChassisSpeeds", "wpilib", "2026", "python"),
		sym("wpimath.ChassisVelocities", "wpilib", "2027", "python"),
	} {
		if err := w.AddSymbol(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	cite := "https://github.com/wpilibsuite/allwpilib/pull/8479"
	for _, m := range []index.Migration{
		{RuleID: "speeds", Library: "wpilib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "rename", From: cs, To: cv, Notes: "Speeds → Velocities.", Citation: cite, Verified: true},
		{RuleID: "speeds", Library: "wpilib", Language: "python", FromSeason: "2026", ToSeason: "2027", Kind: "rename", From: "wpimath.kinematics.ChassisSpeeds", To: "wpimath.ChassisVelocities", Citation: cite, Verified: true},
		{RuleID: "field-relative", Library: "wpilib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "rename", From: cs + "#fromFieldRelativeSpeeds", To: cv + "#toRobotRelative", Citation: cite, Verified: true},
		{RuleID: "dashboard", Library: "wpilib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "removed", From: dash, Notes: "Use Telemetry.", Citation: cite, Verified: false},
		{RuleID: "canbus", Library: "phoenix6", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "signature", From: talon + "#TalonFX", To: talon + "#TalonFX", Notes: "String canbus removed; pass a CANBus.", Citation: "https://v6.docs.ctr-electronics.com/x", Verified: true},
	} {
		if err := w.AddMigration(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := index.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return retrieve.New([]*index.Reader{r}, retrieve.Options{DefaultSeason: "2026"})
}

func TestMapSymbol(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, symbol, lang string
		members            bool
		wantTo, wantSrc    string
		wantConf, wantKind string
		wantMappings       int
		unresolved         int
	}{
		{name: "curated rename by simple name", symbol: "ChassisSpeeds", lang: "java", wantTo: cv, wantSrc: SrcCurated, wantConf: "high", wantKind: "rename", wantMappings: 1},
		{name: "curated rename with members", symbol: cs, lang: "java", members: true, wantTo: cv, wantSrc: SrcCurated, wantConf: "high", wantKind: "rename", wantMappings: 2},
		{name: "curated member via dot form", symbol: "ChassisSpeeds.fromFieldRelativeSpeeds", lang: "java", wantTo: cv + "#toRobotRelative", wantSrc: SrcCurated, wantConf: "high", wantKind: "rename", wantMappings: 1},
		{name: "generated package move", symbol: sdk26, lang: "java", wantTo: sdk27, wantSrc: SrcGenerated, wantConf: "high", wantKind: "move", wantMappings: 1},
		{name: "generated move to another package", symbol: timer, lang: "java", wantTo: timer7, wantSrc: SrcGenerated, wantConf: "medium", wantKind: "move", wantMappings: 1},
		{name: "curated removal (unverified)", symbol: "SmartDashboard", lang: "java", wantSrc: SrcCurated, wantConf: "medium", wantKind: "removed", wantMappings: 1},
		{name: "unchanged", symbol: talon, lang: "java", wantTo: talon, wantSrc: SrcUnchanged, wantConf: "high", wantKind: "unchanged", wantMappings: 1},
		{name: "signature rule on a kept constructor", symbol: talon + "#TalonFX", lang: "java", wantTo: talon + "#TalonFX", wantSrc: SrcCurated, wantConf: "high", wantKind: "signature", wantMappings: 1},
		{name: "python", symbol: "wpimath.kinematics.ChassisSpeeds", lang: "python", wantTo: "wpimath.ChassisVelocities", wantSrc: SrcCurated, wantConf: "high", wantKind: "rename", wantMappings: 1},
		{name: "no mapping", symbol: "edu.wpi.first.wpilibj.Gone", lang: "java", unresolved: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Map(ctx, e, Query{Symbol: tc.symbol, Language: tc.lang, From: "2026", To: "2027", Members: tc.members})
			if len(res.Mappings) != tc.wantMappings || len(res.Unresolved) != tc.unresolved {
				t.Fatalf("mappings=%d unresolved=%d, want %d/%d: %+v", len(res.Mappings), len(res.Unresolved), tc.wantMappings, tc.unresolved, res)
			}
			if tc.wantMappings == 0 {
				return
			}
			m := res.Mappings[0]
			if m.To != tc.wantTo || m.Source != tc.wantSrc || m.Confidence != tc.wantConf || m.Kind != tc.wantKind {
				t.Errorf("got to=%q src=%s conf=%s kind=%s, want %q %s %s %s", m.To, m.Source, m.Confidence, m.Kind, tc.wantTo, tc.wantSrc, tc.wantConf, tc.wantKind)
			}
			if m.Source == SrcCurated && m.Citation == "" {
				t.Error("curated mapping without citation")
			}
		})
	}
}

func TestMapBackward(t *testing.T) {
	res := Map(context.Background(), testEngine(t), Query{Symbol: cv, Language: "java", From: "2027", To: "2026"})
	if len(res.Mappings) != 1 || res.Mappings[0].To != cs || res.Mappings[0].Source != SrcCurated {
		t.Fatalf("backward: %+v", res)
	}
}

func TestMapNotFoundAndAlreadyMigrated(t *testing.T) {
	e := testEngine(t)
	res := Map(context.Background(), e, Query{Symbol: "NoSuchThing", Language: "java", From: "2026", To: "2027"})
	if len(res.NotFound) != 1 {
		t.Errorf("not found: %+v", res)
	}
	res = Map(context.Background(), e, Query{Symbol: cv, Language: "java", From: "2026", To: "2027"})
	if len(res.AlreadyIn) != 1 || len(res.Mappings) != 0 {
		t.Errorf("already migrated: %+v", res)
	}
}

func TestMapCode(t *testing.T) {
	code := `package frc.robot;

import edu.wpi.first.math.kinematics.ChassisSpeeds;
import edu.wpi.first.math.kinematics.SwerveDriveKinematics;
import edu.wpi.first.wpilibj.smartdashboard.SmartDashboard;
import com.ctre.phoenix6.hardware.TalonFX;
import frc.robot.Constants;

public class Drive {
  private final TalonFX motor = new TalonFX(1, "canivore");
  ChassisSpeeds speeds = ChassisSpeeds.fromFieldRelativeSpeeds(1, 0, 0, null);
  void periodic() { SmartDashboard.putNumber("x", 1); }
}
`
	res := Map(context.Background(), testEngine(t), Query{Code: code, From: "2026", To: "2027"})
	got := map[string]Mapping{}
	for _, m := range res.Mappings {
		got[m.From] = m
	}
	for from, to := range map[string]string{cs: cv, sdk26: sdk27, dash: "", talon: talon, talon + "#TalonFX": talon + "#TalonFX",
		cs + "#fromFieldRelativeSpeeds": cv + "#toRobotRelative"} {
		m, ok := got[from]
		if !ok {
			t.Errorf("missing mapping for %s (have %v)", from, keys(got))
			continue
		}
		if m.To != to {
			t.Errorf("%s → %q, want %q", from, m.To, to)
		}
		if m.Line == 0 {
			t.Errorf("%s: no line", from)
		}
	}
	if len(res.Mappings) != 6 {
		t.Errorf("mappings = %d, want 6 (no duplicates): %v", len(res.Mappings), keys(got))
	}
	if got[cs].Line != 3 {
		t.Errorf("ChassisSpeeds line = %d, want 3", got[cs].Line)
	}
}

func keys(m map[string]Mapping) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInheritedMemberAndBackwardHops(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "h.sqlite")
	w, err := index.Create(ctx, path, "h")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []index.Symbol{
		sym("com.revrobotics.spark.SparkMax", "revlib", "2026", "java", func(s *index.Symbol) { s.Signature = "public class SparkMax extends SparkBase" }),
		sym("com.revrobotics.spark.SparkBase", "revlib", "2026", "java"),
		sym("com.revrobotics.spark.SparkBase#getOutputCurrent", "revlib", "2026", "java", func(s *index.Symbol) { s.Kind = "method" }),
		sym("com.revrobotics.spark.SparkBase", "revlib", "2027", "java"),
		sym("com.revrobotics.spark.SparkBase#getOutputCurrent", "revlib", "2027", "java", func(s *index.Symbol) { s.Kind = "method" }),
		sym(cs, "wpilib", "2026", "java"),
		sym(cv, "wpilib", "2027", "java"),
		sym("edu.wpi.first.wpilibj.Old", "wpilib", "2025", "java"),
	} {
		if err := w.AddSymbol(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []index.Migration{
		{RuleID: "current", Library: "revlib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "signature",
			From: "com.revrobotics.spark.SparkBase#getOutputCurrent", To: "com.revrobotics.spark.SparkBase#getOutputCurrent",
			Notes: "returns a Signal now", Citation: "https://example.org/rev", Verified: true},
		{RuleID: "speeds", Library: "wpilib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "rename", From: cs, To: cv,
			Citation: "https://example.org/w", Verified: true},
	} {
		if err := w.AddMigration(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := index.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	e := retrieve.New([]*index.Reader{r}, retrieve.Options{})

	// A member called on a subclass resolves to its declaring type's rule.
	res := Map(ctx, e, Query{Code: "import com.revrobotics.spark.SparkMax;\nclass A { SparkMax m; double f() { return m.getOutputCurrent(); } }\n",
		Language: "java", From: "2026", To: "2027"})
	found := false
	for _, mp := range res.Mappings {
		found = found || mp.RuleID == "current"
	}
	if !found {
		t.Fatalf("inherited member rule not reported: %+v", res)
	}
	// 2027 → 2025 needs evidence for every hop: 2026 has it, 2025 does not.
	res = Map(ctx, e, Query{Symbol: cv, Language: "java", From: "2027", To: "2025"})
	if len(res.Mappings) != 0 || len(res.Unresolved) != 1 {
		t.Fatalf("backward over a season without evidence: %+v", res)
	}
	res = Map(ctx, e, Query{Symbol: cv, Language: "java", From: "2027", To: "2026"})
	if len(res.Mappings) != 1 || res.Mappings[0].To != cs {
		t.Fatalf("one-hop backward: %+v", res)
	}
	// Truncation is reported.
	res = Map(ctx, e, Query{Code: "import edu.wpi.first.math.kinematics.ChassisSpeeds;\nimport com.revrobotics.spark.SparkMax;\n",
		Language: "java", From: "2026", To: "2027", MaxRefs: 1})
	if res.Omitted != 1 {
		t.Fatalf("omitted = %d", res.Omitted)
	}
}
