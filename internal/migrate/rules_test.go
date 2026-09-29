package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inlineComment finds a plain (unquoted) YAML scalar followed by " #": YAML
// drops the rest of the line as a comment, so "fixed in #8479" silently
// loses its tail. Notes cite PR numbers often; use a block scalar (>-).
var inlineComment = regexp.MustCompile(`^\s+(?:- )?[a-z_]+: [^'">|\s].* #`)

const validRules = `
library: wpilib
from_season: "2026"
to_season: "2027"
rules:
  - id: chassisspeeds-to-chassisvelocities
    kind: rename
    from:
      java: edu.wpi.first.math.kinematics.ChassisSpeeds
      cpp: frc::ChassisSpeeds
    to:
      java: org.wpilib.math.kinematics.ChassisVelocities
      cpp: wpi::math::ChassisVelocities
    notes: >
      Speeds became Velocities.
    citation: https://github.com/wpilibsuite/allwpilib/pull/8479
  - id: smartdashboard-removed
    kind: removed
    from:
      java: edu.wpi.first.wpilibj.smartdashboard.SmartDashboard
    notes: Use Telemetry.
    citation: https://example.org/x
    verified: false
`

func TestParseExpandsPerLanguage(t *testing.T) {
	ms, err := Parse([]byte(validRules))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 {
		t.Fatalf("got %d migrations, want 3 (2 languages + 1)", len(ms))
	}
	if ms[0].Language != "cpp" || ms[1].Language != "java" || ms[0].RuleID != ms[1].RuleID {
		t.Errorf("languages not expanded in order: %+v", ms[:2])
	}
	if ms[1].Notes != "Speeds became Velocities." || !ms[1].Verified {
		t.Errorf("notes/verified: %+v", ms[1])
	}
	if ms[2].Kind != "removed" || ms[2].To != "" || ms[2].Verified {
		t.Errorf("removed rule: %+v", ms[2])
	}
}

func TestParseRejects(t *testing.T) {
	head := "library: wpilib\nfrom_season: \"2026\"\nto_season: \"2027\"\nrules:\n"
	rule := func(body string) string { return head + body }
	for name, doc := range map[string]string{
		"unknown field":    rule("  - id: a\n    kind: rename\n    frm: {java: a.B}\n    citation: https://x\n"),
		"seasons reversed": "library: wpilib\nfrom_season: \"2027\"\nto_season: \"2026\"\nrules: []\n",
		"bad id":           rule("  - id: Bad_ID\n    kind: rename\n    from: {java: a.B}\n    to: {java: a.C}\n    citation: https://x\n"),
		"duplicate id":     rule("  - id: a\n    kind: rename\n    from: {java: a.B}\n    to: {java: a.C}\n    citation: https://x\n  - id: a\n    kind: rename\n    from: {java: a.D}\n    to: {java: a.E}\n    citation: https://x\n"),
		"http citation":    rule("  - id: a\n    kind: rename\n    from: {java: a.B}\n    to: {java: a.C}\n    citation: http://x\n"),
		"rename no target": rule("  - id: a\n    kind: rename\n    from: {java: a.B}\n    citation: https://x\n"),
		"to without from":  rule("  - id: a\n    kind: rename\n    from: {java: a.B}\n    to: {java: a.C, cpp: b::C}\n    citation: https://x\n"),
		"same from and to": rule("  - id: a\n    kind: rename\n    from: {java: a.B}\n    to: {java: a.B}\n    citation: https://x\n"),
		"removed no notes": rule("  - id: a\n    kind: removed\n    from: {java: a.B}\n    citation: https://x\n"),
		"bad kind":         rule("  - id: a\n    kind: rewrite\n    from: {java: a.B}\n    to: {java: a.C}\n    citation: https://x\n"),
		"bad language":     rule("  - id: a\n    kind: rename\n    from: {kotlin: a.B}\n    to: {kotlin: a.C}\n    citation: https://x\n"),
	} {
		if _, err := Parse([]byte(doc)); !errors.Is(err, ErrRules) {
			t.Errorf("%s: err = %v, want ErrRules", name, err)
		}
	}
}

// TestRepoRules validates every committed rule file (schema only; symbol
// existence is checked by the index build against the real tables).
func TestRepoRules(t *testing.T) {
	paths, _ := filepath.Glob("../../data/migrations/*.yaml")
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if inlineComment.MatchString(line) {
				t.Errorf("%s:%d: plain scalar with ' #' is cut at the '#'; use a block scalar (>-): %s", filepath.Base(p), i+1, strings.TrimSpace(line))
			}
		}
	}
	ms, err := Load("../../data/migrations")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range ms {
		k := m.Library + "/" + m.FromSeason + "/" + m.RuleID + "/" + m.Language
		if ids[k] {
			t.Errorf("duplicate rule %s", k)
		}
		ids[k] = true
		if strings.Contains(m.Notes, "TODO") {
			t.Errorf("%s: notes contain TODO", k)
		}
	}
}
