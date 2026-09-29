package build

import (
	"strings"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func TestApplyMigrationsHomeAndHistory(t *testing.T) {
	s := func(fqn, lib, season, license string) index.Symbol {
		return index.Symbol{FQN: fqn, Library: lib, Season: season, Language: "java", License: license}
	}
	const rev = "LicenseRef-REVLib-API"
	shards := func() map[string]*shardData {
		return map[string]*shardData{
			"vendor-restricted-api-2026": {name: "vendor-restricted-api-2026", symbols: []index.Symbol{
				s("com.revrobotics.spark.SparkMax", "revlib", "2026", rev), s("com.revrobotics.spark.SparkMax#SparkMax", "revlib", "2026", rev)}},
			"vendor-restricted-api-2027": {name: "vendor-restricted-api-2027", symbols: []index.Symbol{
				s("com.revrobotics.spark.SparkMax#SparkMax", "revlib", "2027", rev)}},
			"wpilib-java-2026": {name: "wpilib-java-2026", symbols: []index.Symbol{s("edu.wpi.first.wpilibj.TimedRobot", "wpilib", "2026", "BSD-3-Clause")}},
		}
	}
	rule := func(id, from, to, fs, ts string) index.Migration {
		return index.Migration{RuleID: id, Library: "revlib", Language: "java", FromSeason: fs, ToSeason: ts, Kind: "rename", From: from, To: to}
	}
	sh := shards()
	w, skipped, err := applyMigrations(sh, []index.Migration{
		{RuleID: "ctor", Library: "revlib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "signature",
			From: "com.revrobotics.spark.SparkMax#SparkMax", To: "com.revrobotics.spark.SparkMax#SparkMax"},
		rule("old", "com.revrobotics.CANSparkMax", "com.revrobotics.spark.SparkMax", "2024", "2025"),
		{RuleID: "cpp", Library: "revlib", Language: "cpp", FromSeason: "2024", ToSeason: "2025", Kind: "rename", From: "rev::CANSparkMax", To: "rev::spark::SparkMax"},
	})
	if err != nil || w != 2 || skipped != 1 {
		t.Fatalf("written=%d skipped=%d err=%v", w, skipped, err)
	}
	// Rules are the project's own data: never stored in a restricted shard.
	if got := len(sh["wpilib-java-2026"].migrations); got != 2 {
		t.Errorf("public shard holds %d rules, want 2", got)
	}
	for _, n := range []string{"vendor-restricted-api-2026", "vendor-restricted-api-2027"} {
		if len(sh[n].migrations) != 0 {
			t.Errorf("%s holds rules: %+v", n, sh[n].migrations)
		}
	}
	// A historical rule must describe a real change: the old name gone and
	// the new one present in the oldest newer table.
	for _, bad := range []index.Migration{
		rule("still-there", "com.revrobotics.spark.SparkMax", "com.revrobotics.spark.SparkFlex", "2024", "2025"),
		rule("no-target", "com.revrobotics.CANSparkMax", "com.revrobotics.spark.Nope", "2024", "2025"),
	} {
		if _, _, err := applyMigrations(shards(), []index.Migration{bad}); err == nil || !strings.Contains(err.Error(), bad.RuleID) {
			t.Errorf("%s: err = %v", bad.RuleID, err)
		}
	}
}
