package retrieve

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func hardwareEngine(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "hw.sqlite")
	w, err := index.Create(ctx, p, "hw")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(0, 0)
	row := func(part, name, factory string, torque float64) index.HWSpec {
		return index.HWSpec{Part: part, Name: name, Category: "motor", Source: "wpilib-dcmotor", Season: "2026",
			Fields: map[string]float64{"stall_torque_nm": torque}, Factory: factory, SourceURL: "https://x", UpstreamRev: "r",
			RetrievedAt: at, License: "BSD-3-Clause", Trust: "official"}
	}
	recalc := func(part, name string, torque float64) index.HWSpec {
		return index.HWSpec{Part: part, Name: name, Category: "motor", Source: "recalc", Season: SeasonAll,
			Fields: map[string]float64{"stall_torque_nm": torque}, SourceURL: "https://y", UpstreamRev: "sha", RetrievedAt: at,
			License: "MIT", Trust: "community"}
	}
	module := index.HWSpec{Part: "sdsmk4i", Name: "SDS MK4i", Category: "swerve_module", Source: "sds", Season: SeasonAll,
		Fields: map[string]float64{"steer_ratio": 21.428571428571427}, SourceURL: "https://z", UpstreamRev: "checked 2026-09-29",
		RetrievedAt: at, License: "MIT", Trust: "vendor"}
	for _, h := range []index.HWSpec{row("krakenx60", "Kraken X60", "getKrakenX60", 7.09), row("krakenx60-foc", "Kraken X60 (FOC)", "getKrakenX60Foc", 9.37),
		row("andymarkrs775_125", "Andymark RS775-125", "getAndymarkRs775_125", 0.28), recalc("krakenx60", "Kraken X60", 7.157),
		recalc("thriftypulsar", "Thrifty Pulsar", 3.1), module} {
		if err := w.AddHWSpec(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	sym := func(fqn, kind, lang string) index.Symbol {
		return index.Symbol{FQN: fqn, Library: "wpilib", Version: "2026.2.2", Season: "2026", Language: lang, Kind: kind,
			Signature: "x", SourceURL: "https://x", UpstreamRev: "r", RetrievedAt: at, License: "BSD-3-Clause", Trust: "official"}
	}
	for _, s := range []index.Symbol{
		sym("frc::DCMotor", "class", "cpp"), sym("frc::DCMotor#KrakenX60", "method", "cpp"), sym("frc::DCMotor#KrakenX60FOC", "method", "cpp"),
		sym("frc::DCMotor#RS775_125", "method", "cpp"), sym("frc::DCMotor#stallTorque", "field", "cpp"),
		sym("wpimath.system.plant.DCMotor", "class", "python"), sym("wpimath.system.plant.DCMotor#krakenX60FOC", "method", "python"),
	} {
		if err := w.AddSymbol(ctx, s); err != nil {
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
	return New([]*index.Reader{r}, Options{})
}

func TestHardware(t *testing.T) {
	e := hardwareEngine(t)
	got, unknown := e.Hardware([]string{"Kraken X60 FOC", "kraken", "andymark rs775", "flux capacitor"}, "", "2026")
	if len(got) != 3 || got[0].Part != "krakenx60-foc" || got[1].Part != "krakenx60" || got[2].Part != "andymarkrs775_125" ||
		len(unknown) != 1 || unknown[0] != "flux capacitor" {
		t.Fatalf("got %+v unknown %v", got, unknown)
	}
	if all, _ := e.Hardware(nil, "motor", ""); len(all) != 4 {
		t.Fatalf("category: %+v", all)
	}
	// Season-independent rows answer every season, next to the season's own
	// rows, each source separate.
	k, _ := e.Hardware([]string{"kraken"}, "", "2027")
	if len(k) != 1 || len(k[0].Rows) != 1 || k[0].Rows[0].Source != "recalc" {
		t.Fatalf("2027 kraken: %+v", k)
	}
	k, _ = e.Hardware([]string{"kraken"}, "", "2026")
	if len(k) != 1 || len(k[0].Rows) != 2 || k[0].Rows[0].Source != "recalc" || k[0].Rows[1].Source != "wpilib-dcmotor" {
		t.Fatalf("2026 kraken: %+v", k)
	}
	if sw, _ := e.Hardware(nil, "swerve_module", "2026"); len(sw) != 1 || sw[0].Part != "sdsmk4i" {
		t.Fatalf("swerve category: %+v", sw)
	}
	// Names without the vendor resolve by unique suffix; aliases too.
	if got, unknown := e.Hardware([]string{"MK4i", "pulsar"}, "", "2026"); len(got) != 2 || len(unknown) != 0 ||
		got[0].Part != "sdsmk4i" || got[1].Part != "thriftypulsar" {
		t.Fatalf("suffix/alias: %+v %v", got, unknown)
	}
	ctx := context.Background()
	for _, tc := range []struct{ factory, lang, want string }{
		{"getKrakenX60Foc", "cpp", "frc::DCMotor#KrakenX60FOC"},
		{"getKrakenX60", "cpp", "frc::DCMotor#KrakenX60"},
		{"getAndymarkRs775_125", "cpp", "frc::DCMotor#RS775_125"},
		{"getKrakenX60Foc", "python", "wpimath.system.plant.DCMotor#krakenX60FOC"},
		{"getKrakenX60", "java", ""},
	} {
		if got := e.SimFactory(ctx, tc.factory, "2026", tc.lang); got != tc.want {
			t.Errorf("%s/%s: %q want %q", tc.factory, tc.lang, got, tc.want)
		}
	}
}

func TestPartID(t *testing.T) {
	for in, want := range map[string]string{"Kraken X60": "krakenx60", "kraken": "krakenx60", "Kraken X60 FOC": "krakenx60-foc",
		"NEO Vortex": "neovortex", "vortex": "neovortex", "Falcon 500 (FOC)": "falcon500-foc", "NEO 550": "neo550",
		"NEO 2.0": "neo2", "Pigeon 2.0": "ctrepigeon2", "Through Bore": "revthroughborev2", "MAXSwerve": "revmaxswerve"} {
		if got := PartID(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}
