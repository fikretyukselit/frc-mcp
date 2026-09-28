package facts

import (
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func cat() *Catalog {
	return NewCatalog([]index.Vendordep{
		{UUID: "rev", Name: "REVLib", Version: "2026.0.0", Season: "2026", FRCYear: "2026", JSONURL: "https://software-metadata.revrobotics.com/REVLib-2026.json"},
		{UUID: "rev", Name: "REVLib", Version: "2026.0.5", Season: "2026", FRCYear: "2026", JSONURL: "https://software-metadata.revrobotics.com/REVLib-2026.json"},
		{UUID: "p6", Name: "CTRE-Phoenix (v6)", Version: "26.3.0", Season: "2026", FRCYear: "2026"},
		{UUID: "p6r", Name: "CTRE-Phoenix Replay (v6)", Version: "26.3.0", Season: "2026", FRCYear: "2026",
			Conflicts: []index.Conflict{{UUID: "p6", ErrorMessage: "Users must use the regular Phoenix 6 vendordep when using the Phoenix 6 replay vendordep."}}},
		{UUID: "pp", Name: "PathplannerLib", Version: "2025.1.2", Season: "2026", FRCYear: "2025"},
	})
}

func TestResolve(t *testing.T) {
	c := cat()
	for q, want := range map[string]string{"rev": "REVLib", "SparkMax": "REVLib", "talonfx": "CTRE-Phoenix (v6)",
		"phoenix 6": "CTRE-Phoenix (v6)", "p6": "CTRE-Phoenix (v6)", "pathplanner": "PathplannerLib"} {
		m, cands := c.Resolve(q, "2026")
		if m == nil || m.Latest.Name != want {
			t.Errorf("Resolve(%q) = %v %v, want %s", q, m, cands, want)
		}
	}
	if m, _ := c.Resolve("rev", "2026"); m.Latest.Version != "2026.0.5" || len(m.Versions) != 2 {
		t.Errorf("latest = %+v", m)
	}
	if m, cands := c.Resolve("nonexistent", "2026"); m != nil || cands != nil {
		t.Errorf("unknown: %v %v", m, cands)
	}
}

func TestCheck(t *testing.T) {
	fs := cat().Check([]Installed{
		{Name: "REVLib", Version: "2026.0.0", UUID: "rev", FRCYear: "2026"},
		{Name: "CTRE-Phoenix (v6)", Version: "26.3.0", UUID: "p6", FRCYear: "2026"},
		{Name: "CTRE-Phoenix Replay (v6)", Version: "26.3.0", UUID: "p6r", FRCYear: "2026"},
		{Name: "PathplannerLib", Version: "2025.2.7", UUID: "pp", FRCYear: "2025"},
		{Name: "TeamLib", Version: "1.0"},
	}, "2026")
	want := map[string]string{"REVLib": "outdated", "CTRE-Phoenix (v6)": "ok", "PathplannerLib": "wrong_year", "TeamLib": "unknown"}
	conflicts := 0
	for _, f := range fs {
		if f.Status == "conflict" {
			conflicts++
			continue
		}
		if w, ok := want[f.Name]; ok && f.Status != w {
			t.Errorf("%s: status %s, want %s (%s)", f.Name, f.Status, w, f.Message)
		}
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", conflicts)
	}
	for _, f := range fs {
		if f.Name == "REVLib" && f.Fix != "./gradlew vendordep --url=https://software-metadata.revrobotics.com/REVLib-2026.json" {
			t.Errorf("fix = %q", f.Fix)
		}
	}
}

func TestParseInstalled(t *testing.T) {
	in, err := ParseInstalled(`{"name":"REVLib","version":"2026.0.0","uuid":"rev","frcYear":2026}`)
	if err != nil || in.FRCYear != "2026" || in.Version != "2026.0.0" {
		t.Fatalf("%+v %v", in, err)
	}
	if in, err := ParseInstalled("REVLib@2026.0.5"); err != nil || in.Name != "REVLib" {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err := ParseInstalled("REVLib"); err == nil {
		t.Fatal("want error")
	}
}
