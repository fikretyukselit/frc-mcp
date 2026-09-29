package hwdata

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const valid = `
license: MIT
trust: vendor
shard: hardware
parts:
  - part: sdsmk4i
    name: SDS MK4i
    category: swerve_module
    source: sds
    citation: https://www.swervedrivespecialties.com/products/mk4i-swerve-module
    checked: 2026-09-29
    fields:
      steer_ratio: 21.428571428571427
      weight_with_neo_lb: 6.0
    note: >-
      Steering ratio 150/7:1,
      per the product page.
  - part: reduxcanandmag
    name: Redux Canandmag
    category: encoder
    source: redux
    citation: https://docs.reduxrobotics.com/canandmag/electrical-specs
    see_also: [https://docs.reduxrobotics.com/canandmag]
    checked: 2026-09-29
    fields: {absolute_resolution_bits: 14}
`

func TestParse(t *testing.T) {
	rows, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	h := rows[0].Spec
	if rows[0].Shard != "hardware" || h.Part != "sdsmk4i" || h.Season != Season || h.Source != "sds" || h.License != "MIT" ||
		h.Trust != "vendor" || h.SourceURL != "https://www.swervedrivespecialties.com/products/mk4i-swerve-module" ||
		h.UpstreamRev != "checked 2026-09-29" || h.RetrievedAt.Format("2006-01-02") != "2026-09-29" ||
		h.Fields["steer_ratio"] != 21.428571428571427 || h.Note != "Steering ratio 150/7:1, per the product page." {
		t.Errorf("row %+v", h)
	}
	if n := rows[1].Spec.Note; n != "Also from: https://docs.reduxrobotics.com/canandmag." {
		t.Errorf("see_also must be cited in the note: %q", n)
	}
}

func TestParseRejects(t *testing.T) {
	head := "license: MIT\ntrust: vendor\nshard: hardware\nparts:\n"
	part := func(extra string) string {
		return head + "  - part: x1\n    name: X\n    category: encoder\n    source: ctre\n    citation: https://a.example/p\n    checked: 2026-09-29\n" + extra
	}
	for name, doc := range map[string]string{
		"unknown field":      part("    fields: {cpr_cpr: 1}\n    wheel: 4\n"),
		"no unit suffix":     part("    fields: {resolution: 12}\n"),
		"unknown unit":       part("    fields: {resolution_furlongs: 12}\n"),
		"http citation":      strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "https://a.example/p", "http://a.example/p", 1),
		"http see_also":      part("    see_also: [http://b.example]\n    fields: {max_speed_rpm: 1}\n"),
		"bad category":       strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "encoder", "flux_capacitor", 1),
		"part with hyphen":   strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "x1", "sds-mk4i", 1),
		"no fields":          part(""),
		"bad date":           strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "2026-09-29", "yesterday", 1),
		"no license":         strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "license: MIT", "license: ''", 1),
		"bad trust":          strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "trust: vendor", "trust: trusted", 1),
		"no parts":           head,
		"non-numeric field":  part("    fields: {interface_v: CAN}\n"),
		"infinite field":     part("    fields: {max_speed_rpm: .inf}\n"),
		"source not a label": strings.Replace(part("    fields: {max_speed_rpm: 1}\n"), "source: ctre", "source: CTRE Inc", 1),
	} {
		if _, err := Parse([]byte(doc)); !errors.Is(err, ErrData) {
			t.Errorf("%s: err = %v, want ErrData", name, err)
		}
	}
}

func TestValidKey(t *testing.T) {
	for k, want := range map[string]bool{"steer_ratio": true, "drive_ratio_l1": true, "steer_ratio_flipped": true,
		"wheel_diameter_in": true, "yaw_drift_no_motion_deg_per_hour": true, "quadrature_cpr": true, "weight_g": true,
		"resolution": false, "_v": false, "v": false, "Weight_lb": false, "ratios": false} {
		if got := ValidKey(k); got != want {
			t.Errorf("%q: %v want %v", k, got, want)
		}
	}
}

func TestLoadRejectsDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.yaml", "b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(valid), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Load(dir); !errors.Is(err, ErrData) {
		t.Fatalf("err = %v, want ErrData for a (part, source) in two files", err)
	}
}

// inlineComment finds a plain scalar followed by " #": YAML would drop the
// rest of the line as a comment. Notes must be block scalars (>-).
var inlineComment = regexp.MustCompile(`^\s+(?:- )?(?:note|name): [^'">|\s].* #`)

// TestCommittedData validates data/hardware exactly as the index build does
// and checks the conventions reviewers rely on.
func TestCommittedData(t *testing.T) {
	dir := filepath.Join("..", "..", "data", "hardware")
	rows, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	perCategory := map[string]int{}
	for _, r := range rows {
		perCategory[r.Spec.Category]++
		if r.Spec.Note == "" {
			t.Errorf("%s: every curated row needs a note saying what the numbers mean", r.Spec.Part)
		}
		if r.Shard != "hardware" || r.Spec.Trust != "vendor" {
			t.Errorf("%s: shard %s trust %s", r.Spec.Part, r.Shard, r.Spec.Trust)
		}
	}
	for _, c := range []string{"encoder", "imu", "swerve_module"} {
		if perCategory[c] == 0 {
			t.Errorf("no curated %s rows", c)
		}
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if inlineComment.MatchString(line) {
				t.Errorf("%s:%d: plain scalar with ' #' is cut by YAML; use a block scalar: %s", filepath.Base(p), i+1, line)
			}
		}
	}
}
