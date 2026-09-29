package recalc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

var src = sources.Source{Season: "all", Version: "8702a80eb61300b3cbfd7b800ea8d4b0e7614ab9", License: "MIT", Trust: "community",
	BaseURL: "https://github.com/tervay/recalc/blob/8702a80/app/lib/models/Motor.ts", URL: "https://raw/x"}

func parse(t *testing.T, path string) (map[string]index.HWSpec, Stats, error) {
	t.Helper()
	got := map[string]index.HWSpec{}
	st, err := Parse(path, src, time.Unix(0, 0), func(h index.HWSpec) error { got[h.Part] = h; return nil })
	return got, st, err
}

// TestParseRecorded runs the adapter over Motor.ts recorded at the pinned
// commit (testdata/, MIT; license in testdata/LICENSE-recalc.txt).
func TestParseRecorded(t *testing.T) {
	got, st, err := parse(t, filepath.Join("testdata", "Motor.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Motors != 21 || st.Skipped != 7 {
		t.Fatalf("stats %+v, want 21 FRC motors and 7 FTC/other skipped", st)
	}
	// Part ids must line up with wpilib-dcmotor's so both sources answer one query.
	for _, id := range []string{"krakenx60", "krakenx60-foc", "krakenx44", "krakenx44-foc", "falcon500", "falcon500-foc",
		"neo", "neovortex", "neo550", "minion", "vex775pro", "cim", "minicim", "bag", "andymark9015", "banebotsrs550"} {
		if _, ok := got[id]; !ok {
			t.Errorf("missing part %s", id)
		}
	}
	for _, id := range []string{"neo2", "minionadvhall", "thriftypulsar", "775redline", "snowblower"} {
		if _, ok := got[id]; !ok {
			t.Errorf("missing ReCalc-only part %s", id)
		}
	}
	neo := got["neo"]
	want := map[string]float64{"nominal_voltage_v": 12, "stall_torque_nm": 4.201, "stall_current_a": 216.269,
		"free_current_a": 1.782, "free_speed_rpm": 5906, "motor_weight_lb": 0.938, "controller_weight_lb": 0.25}
	if len(neo.Fields) != len(want) {
		t.Errorf("neo fields %v", neo.Fields)
	}
	for k, v := range want {
		if neo.Fields[k] != v {
			t.Errorf("neo %s = %v, want %v", k, neo.Fields[k], v)
		}
	}
	if neo.Source != "recalc" || neo.Season != "all" || neo.UpstreamRev != src.Version || neo.License != "MIT" ||
		neo.Name != "NEO" || neo.Category != "motor" || neo.Note != "measured by CTRE (ReCalc dataSource; not necessarily the vendor); brushless; sold by REV" {
		t.Errorf("neo row %+v", neo)
	}
	k := got["krakenx60-foc"]
	if k.Fields["stall_torque_nm"] != 9.362 || k.Fields["free_speed_rpm"] != 5784 {
		t.Errorf("kraken foc %v", k.Fields)
	}
	if _, ok := k.Fields["controller_weight_lb"]; ok || !strings.Contains(k.Note, "integrated controller") {
		t.Errorf("integrated controller must have no controller weight: %+v", k)
	}
	if _, ok := got["neverest"]; ok {
		t.Error("FTC motors must be skipped")
	}
}

func TestParseRejects(t *testing.T) {
	good, err := os.ReadFile(filepath.Join("testdata", "Motor.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(string) string{
		"unit changed": func(s string) string {
			return strings.Replace(s, "new Measurement(7.157, 'N*m')", "new Measurement(7.157, 'lbf*in')", 1)
		},
		"table renamed": func(s string) string { return strings.Replace(s, "export const ALL_MOTORS", "export const MOTORS", 1) },
		"field missing": func(s string) string {
			return strings.Replace(s, "    freeSpeed: new Measurement(6065, 'rpm'),\n", "", 1)
		},
		"unknown constant": func(s string) string {
			return strings.Replace(s, "controllerWeight: SPARK_MAX_WEIGHT", "controllerWeight: MYSTERY_WEIGHT", 1)
		},
		"duplicate part": func(s string) string { return strings.Replace(s, "name: 'NEO 2.0'", "name: 'NEO'", 1) },
	} {
		p := filepath.Join(t.TempDir(), "Motor.ts")
		if err := os.WriteFile(p, []byte(mut(string(good))), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := parse(t, p); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}

func TestPart(t *testing.T) {
	for in, want := range map[string]string{"Kraken X60 (FOC)": "krakenx60-foc", "775pro": "vex775pro", "New Motor 9000": "newmotor9000"} {
		if got := Part(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}
