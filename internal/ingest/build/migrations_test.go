package build

import (
	"strings"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

func TestApplyMigrations(t *testing.T) {
	s := func(fqn, season string) index.Symbol {
		return index.Symbol{FQN: fqn, Library: "revlib", Season: season, Language: "java", License: "LicenseRef-REVLib-API"}
	}
	reg := &sources.Registry{Sources: []sources.Source{
		{ID: "revlib-java-2026", Library: "revlib", Language: "java", Season: "2026", Shard: "vendor-restricted-api-2026"},
		{ID: "revlib-java-2027", Library: "revlib", Language: "java", Season: "2027", Shard: "vendor-restricted-api-2027"},
	}}
	t26 := func() *shardData {
		return &shardData{name: "vendor-restricted-api-2026", symbols: []index.Symbol{
			s("com.revrobotics.spark.SparkMax", "2026"), s("com.revrobotics.spark.SparkMax#SparkMax", "2026")}}
	}
	t27 := func() *shardData {
		return &shardData{name: "vendor-restricted-api-2027", symbols: []index.Symbol{s("com.revrobotics.spark.SparkMax#SparkMax", "2027")}}
	}
	ctor := index.Migration{RuleID: "ctor", Library: "revlib", Language: "java", FromSeason: "2026", ToSeason: "2027", Kind: "signature",
		From: "com.revrobotics.spark.SparkMax#SparkMax", To: "com.revrobotics.spark.SparkMax#SparkMax"}
	rule := func(id, from, to string) index.Migration {
		return index.Migration{RuleID: id, Library: "revlib", Language: "java", FromSeason: "2024", ToSeason: "2025", Kind: "rename", From: from, To: to}
	}
	old := rule("old", "com.revrobotics.CANSparkMax", "com.revrobotics.spark.SparkMax")
	undeclared := index.Migration{RuleID: "p5", Library: "phoenix5", Language: "java", FromSeason: "2024", ToSeason: "2025", Kind: "removed", From: "x.Y", Notes: "n"}

	// Full run: every rule checked, all in the migrations shard, none in a
	// table shard (restricted or not).
	sh := map[string]*shardData{"vendor-restricted-api-2026": t26(), "vendor-restricted-api-2027": t27()}
	w, skipped, err := applyMigrations(sh, []index.Migration{ctor, old}, reg)
	if err != nil || w != 2 || skipped != 0 {
		t.Fatalf("full: written=%d skipped=%d err=%v", w, skipped, err)
	}
	if m := sh[MigrationsShard]; m == nil || len(m.migrations) != 2 {
		t.Fatalf("migrations shard: %+v", sh[MigrationsShard])
	}
	for _, n := range []string{"vendor-restricted-api-2026", "vendor-restricted-api-2027"} {
		if len(sh[n].migrations) != 0 {
			t.Errorf("%s holds rules", n)
		}
	}

	// Partial run of the 2027 table only: the 2026 → 2027 signature rule is
	// out of scope (not historical), and the migrations shard is left alone.
	sh = map[string]*shardData{"vendor-restricted-api-2027": t27()}
	if w, skipped, err := applyMigrations(sh, []index.Migration{ctor, old}, reg); err != nil || w != 0 || skipped != 2 || sh[MigrationsShard] != nil {
		t.Fatalf("partial: written=%d skipped=%d err=%v shard=%v", w, skipped, err, sh[MigrationsShard] != nil)
	}

	// Partial run of the 2026 table only: the rule's declared 2027 target is
	// not checked, so nothing is written either.
	sh = map[string]*shardData{"vendor-restricted-api-2026": t26()}
	if w, skipped, err := applyMigrations(sh, []index.Migration{ctor}, reg); err != nil || w != 0 || skipped != 1 || sh[MigrationsShard] != nil {
		t.Fatalf("partial 2026: written=%d skipped=%d err=%v", w, skipped, err)
	}

	// A rule no declared table can check, and a historical rule that does
	// not describe a real change, fail the build.
	for _, bad := range []index.Migration{
		undeclared,
		rule("still-there", "com.revrobotics.spark.SparkMax", "com.revrobotics.spark.SparkMax#SparkMax"),
		rule("no-target", "com.revrobotics.CANSparkMax", "com.revrobotics.spark.Nope"),
	} {
		sh := map[string]*shardData{"vendor-restricted-api-2026": t26()}
		if _, _, err := applyMigrations(sh, []index.Migration{bad}, reg); err == nil || !strings.Contains(err.Error(), bad.RuleID) {
			t.Errorf("%s: err = %v", bad.RuleID, err)
		}
	}
}
