package mcpserver_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/hwdata"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/recalc"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// hardwareEngine builds the two hardware shards the way the index build
// does: wpilib-dcmotor rows, ReCalc's recorded Motor.ts through its adapter
// and the committed data/hardware files in "hardware"; a vendor spec-page row
// in "hardware-restricted".
func hardwareEngine(t *testing.T) *retrieve.Engine {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	var main []index.HWSpec
	main = append(main, index.HWSpec{Part: "neo", Name: "NEO", Category: "motor", Source: "wpilib-dcmotor", Season: "2026",
		Fields: map[string]float64{"nominal_voltage_v": 12, "stall_torque_nm": 2.6, "stall_current_a": 105, "free_current_a": 1.8,
			"free_speed_rpm": 5676}, Factory: "getNEO", SourceURL: "https://github.com/wpilibsuite/allwpilib/blob/v2026.2.2/DCMotor.java",
		UpstreamRev: "v2026.2.2", RetrievedAt: at, License: "BSD-3-Clause", Trust: "official"})
	src := sources.Source{Season: "all", Version: "8702a80eb61300b3cbfd7b800ea8d4b0e7614ab9", License: "MIT", Trust: "community",
		BaseURL: "https://github.com/tervay/recalc/blob/8702a80eb61300b3cbfd7b800ea8d4b0e7614ab9/app/lib/models/Motor.ts"}
	if _, err := recalc.Parse(filepath.Join("..", "ingest", "source", "recalc", "testdata", "Motor.ts"), src, at,
		func(h index.HWSpec) error { main = append(main, h); return nil }); err != nil {
		t.Fatal(err)
	}
	rows, err := hwdata.Load(filepath.Join("..", "..", "data", "hardware"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		main = append(main, r.Spec)
	}
	restricted := []index.HWSpec{{Part: "neo", Name: "NEO V1.1", Category: "motor", Source: "rev-docs", Season: "all",
		Fields: map[string]float64{"stall_torque_nm": 2.6, "free_speed_rpm": 5676}, SourceURL: "https://docs.revrobotics.com/brushless/neo/v1.1",
		UpstreamRev: "etag", RetrievedAt: at, License: "LicenseRef-REV-Docs-NoLicense", Trust: "vendor"}}
	var readers []*index.Reader
	for name, hs := range map[string][]index.HWSpec{"hardware": main, "hardware-restricted": restricted} {
		p := filepath.Join(t.TempDir(), name+".sqlite")
		w, err := index.Create(ctx, p, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hs {
			if err := w.AddHWSpec(ctx, h); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(ctx); err != nil {
			t.Fatal(err)
		}
		r, err := index.Open(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		readers = append(readers, r)
	}
	return retrieve.New(readers, retrieve.Options{DefaultSeason: "2026"})
}

func sourcesOf(t *testing.T, part map[string]any) []string {
	t.Helper()
	var out []string
	for _, s := range part["sources"].([]any) {
		row := s.(map[string]any)
		out = append(out, row["source"].(string)+"@"+row["frc_season"].(string))
	}
	return out
}

func TestHardwareToolSources(t *testing.T) {
	cs := connect(t, hardwareEngine(t))

	// "neo": every source is its own labeled column, never merged.
	_, sc, text := call(t, cs, "frc_hardware", map[string]any{"parts": []string{"neo"}})
	parts := sc["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("parts %v", sc)
	}
	neo := parts[0].(map[string]any)
	if got := strings.Join(sourcesOf(t, neo), ","); got != "recalc@all,rev-docs@all,wpilib-dcmotor@2026" {
		t.Fatalf("neo sources %s\n%s", got, text)
	}
	byName := map[string]map[string]any{}
	for _, s := range neo["sources"].([]any) {
		row := s.(map[string]any)
		byName[row["source"].(string)] = row
	}
	if byName["recalc"]["fields"].(map[string]any)["stall_torque_nm"] != 4.201 ||
		byName["wpilib-dcmotor"]["fields"].(map[string]any)["stall_torque_nm"] != 2.6 {
		t.Errorf("per-source values: %v", byName)
	}
	if byName["recalc"]["library"] != "recalc" || byName["wpilib-dcmotor"]["library"] != "wpilib" ||
		byName["rev-docs"]["license"] != "LicenseRef-REV-Docs-NoLicense" {
		t.Errorf("citations: %v", byName)
	}
	for _, want := range []string{"| recalc all |", "rev-docs all", "wpilib-dcmotor 2026", "4.201", "2.6", "Source: https://github.com/tervay/recalc/"} {
		if !strings.Contains(text, want) {
			t.Errorf("content lacks %q:\n%s", want, text)
		}
	}

	// 2027: WPILib's 2026 constants drop out, season-independent sources stay.
	_, sc, _ = call(t, cs, "frc_hardware", map[string]any{"parts": []string{"neo"}, "frc_season": "2027"})
	if got := strings.Join(sourcesOf(t, sc["parts"].([]any)[0].(map[string]any)), ","); got != "recalc@all,rev-docs@all" {
		t.Errorf("2027 neo sources %s", got)
	}

	// Categories beyond motor come from data/hardware.
	for cat, want := range map[string]string{"swerve_module": "sdsmk4i", "encoder": "ctrecancoder", "imu": "ctrepigeon2"} {
		_, sc, text := call(t, cs, "frc_hardware", map[string]any{"category": cat})
		var ids []string
		for _, p := range sc["parts"].([]any) {
			m := p.(map[string]any)
			if m["category"] != cat {
				t.Errorf("%s: part %v has category %v", cat, m["part"], m["category"])
			}
			ids = append(ids, m["part"].(string))
		}
		if !strings.Contains(strings.Join(ids, ","), want) || sc["status"] != "ok" {
			t.Errorf("%s: parts %v status %v\n%s", cat, ids, sc["status"], text)
		}
	}

	// Vendor-less names resolve (unique suffix / alias).
	_, sc, text = call(t, cs, "frc_hardware", map[string]any{"parts": []string{"mk4i", "cancoder", "pigeon 2", "maxswerve"}})
	var ids []string
	for _, p := range sc["parts"].([]any) {
		ids = append(ids, p.(map[string]any)["part"].(string))
	}
	if strings.Join(ids, ",") != "sdsmk4i,ctrecancoder,ctrepigeon2,revmaxswerve" {
		t.Errorf("aliases resolved to %v\n%s", ids, text)
	}
	if !strings.Contains(text, "steer_ratio") {
		t.Errorf("swerve fields not rendered:\n%s", text)
	}

	// An unknown category is an instructive error listing the valid ones.
	res, _, text := call(t, cs, "frc_hardware", map[string]any{"category": "controller"})
	if !res.IsError || !strings.Contains(text, "swerve_module") {
		t.Errorf("bad category: isError=%v %s", res.IsError, text)
	}
}
