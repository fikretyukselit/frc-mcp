package specpage

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

func source(part, name, label string) sources.Source {
	return sources.Source{ID: "t", Season: "all", License: "LicenseRef-Test-NoLicense", Trust: "vendor",
		URL: "https://docs.example/p.md", BaseURL: "https://docs.example/p",
		Hardware: &sources.Hardware{Source: label, Part: part, Name: name}}
}

func parseFile(t *testing.T, path string, src sources.Source) ([]index.HWSpec, error) {
	t.Helper()
	var got []index.HWSpec
	_, err := Parse(path, src, "etag-1", time.Unix(0, 0), func(h index.HWSpec) error { got = append(got, h); return nil })
	return got, err
}

func TestParseREVStyle(t *testing.T) {
	got, err := parseFile(t, filepath.Join("testdata", "rev-style.md"), source("neo", "NEO V1.1", "rev-docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows %+v", got)
	}
	h := got[0]
	want := map[string]float64{"nominal_voltage_v": 12, "kv_rpm_per_v": 473, "free_speed_rpm": 5676, "free_current_a": 1.8,
		"stall_current_a": 105, "stall_torque_nm": 2.6, "peak_power_w": 406}
	if len(h.Fields) != len(want) {
		t.Errorf("fields %v (the weight and 40 A power rows must be ignored)", h.Fields)
	}
	for k, v := range want {
		if h.Fields[k] != v {
			t.Errorf("%s = %v, want %v", k, h.Fields[k], v)
		}
	}
	if h.Part != "neo" || h.Source != "rev-docs" || h.Season != "all" || h.UpstreamRev != "etag-1" ||
		h.SourceURL != "https://docs.example/p" || h.Note != `From the vendor spec page "NEO V1.1"` {
		t.Errorf("row %+v", h)
	}
}

func TestParseWCPTabs(t *testing.T) {
	got, err := parseFile(t, filepath.Join("testdata", "wcp-style.md"), source("krakenx60", "Kraken X60", "wcp-docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Part != "krakenx60" || got[1].Part != "krakenx60-foc" || got[1].Name != "Kraken X60 (FOC)" {
		t.Fatalf("rows %+v", got)
	}
	trap, foc := got[0].Fields, got[1].Fields
	if trap["free_speed_rpm"] != 6000 || trap["peak_power_w"] != 1108 || trap["max_efficiency_pct"] != 87 ||
		trap["max_efficiency_current_a"] != 30 || trap["stall_torque_nm"] != 7.09 {
		t.Errorf("trapezoidal %v", trap)
	}
	if foc["stall_torque_nm"] != 9.37 || foc["stall_current_a"] != 483 || len(foc) != 4 {
		t.Errorf("foc %v", foc)
	}
	if _, ok := trap["nominal_voltage_v"]; ok {
		t.Error("a page that states no voltage must not get one")
	}
	if !strings.Contains(got[1].Note, `tab "Field Oriented Control (FOC) Communtation"`) {
		t.Errorf("note %q", got[1].Note)
	}
}

func TestParseRejects(t *testing.T) {
	rev, err := os.ReadFile(filepath.Join("testdata", "rev-style.md"))
	if err != nil {
		t.Fatal(err)
	}
	wcp, err := os.ReadFile(filepath.Join("testdata", "wcp-style.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"unit changed":   strings.Replace(string(rev), "2.6 Nm", "23 in-lb", 1),
		"required gone":  strings.Replace(string(rev), "| Stall Current                  | 105 A              |\n", "", 1),
		"duplicate row":  strings.Replace(string(rev), "| Peak Output Power ", "| Stall Torque | 2.7 Nm |\n| Peak Output Power ", 1),
		"no table":       "# NEO\n\nNothing here.\n",
		"unknown tab":    strings.Replace(string(wcp), `title="Trapezoidal Commutation"`, `title="Sinusoidal"`, 1),
		"not a number":   strings.Replace(string(rev), "5676 RPM", "fast", 1),
		"two same parts": strings.Replace(string(wcp), `title="Field Oriented Control (FOC) Communtation"`, `title="Trapezoidal (again)"`, 1),
	} {
		p := filepath.Join(t.TempDir(), "p.md")
		if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := parseFile(t, p, source("neo", "NEO", "rev-docs")); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}

func TestValue(t *testing.T) {
	for _, tc := range []struct {
		in, unit string
		want     float64
	}{{"6,000 RPM", "rpm", 6000}, {"15.37 mNm/A", "mnm/a", 15.37}, {"85.4% W(out) / W(in)", "%w(out)/w(in)", 85.4}, {"3.6 Nm", "nm", 3.6}} {
		if got, err := value(tc.in, tc.unit); err != nil || got != tc.want {
			t.Errorf("%q: %v %v", tc.in, got, err)
		}
	}
	if _, err := value("413(86.1%) W(%Eff)", "w"); err == nil {
		t.Error("a value with a different unit must be rejected")
	}
}
