// Package recalc reads ReCalc's motor table (tervay/recalc,
// app/lib/models/Motor.ts, MIT) at a pinned commit and turns every FRC motor
// in ALL_MOTORS into a hardware spec row labeled "recalc".
//
// ReCalc takes most of its numbers from vendor dynos (its dataSource field:
// CTRE, REV, VEX, Thrifty), so they differ from WPILib's DCMotor constants,
// sometimes a lot (NEO stall torque 4.201 N·m here vs 2.6 N·m in WPILib).
// The rows are therefore their own source and are never merged with
// wpilib-dcmotor. Values are copied as written; nothing is derived (ReCalc
// computes resistance, kV and kT at runtime from these five numbers, so they
// are not upstream data and are not stored).
package recalc

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// Source is the hw_spec source label of every row.
const Source = "recalc"

// ErrFormat is returned (wrapped) when Motor.ts no longer looks as expected.
var ErrFormat = errors.New("recalc: unexpected Motor.ts format")

var (
	tableRe   = regexp.MustCompile(`(?s)export const ALL_MOTORS: MotorSpecs\[\] = \[(.*?)\n\];`)
	objectRe  = regexp.MustCompile(`(?s)\{\s*\n(.*?)\n  \}`)
	nameRe    = regexp.MustCompile(`(?m)^\s*name: '([^']+)',`)
	measureRe = regexp.MustCompile(`(?m)^\s*(\w+): new Measurement\(([\d.]+), '([^']+)'\),`)
	constRe   = regexp.MustCompile(`(?m)^const (\w+) = new Measurement\(([\d.]+), '([^']+)'\);`)
	identRe   = regexp.MustCompile(`(?m)^\s*controllerWeight: ([A-Z_]+),`)
	stringRe  = regexp.MustCompile(`(?m)^\s*(type|dataSource): '([^']+)',`)
	vendorsRe = regexp.MustCompile(`(?m)^\s*vendors: \[([^\]]*)\],`)
	programRe = regexp.MustCompile(`(?m)^\s*intendedProgram: IntendedProgram\.(\w+),`)
	// plainRe keeps upstream strings that end up in a note to plain words.
	plainRe = regexp.MustCompile(`^[A-Za-z0-9 .\-]+$`)
)

// fields maps a MotorSpecs measurement to its unit-suffixed hw_spec key and
// the only unit accepted for it. Another unit fails the parse rather than
// being stored under a wrong suffix.
var fields = map[string]struct{ key, unit string }{
	"voltage":          {"nominal_voltage_v", "V"},
	"stallTorque":      {"stall_torque_nm", "N*m"},
	"stallCurrent":     {"stall_current_a", "A"},
	"freeCurrent":      {"free_current_a", "A"},
	"freeSpeed":        {"free_speed_rpm", "rpm"},
	"motorWeight":      {"motor_weight_lb", "lb"},
	"controllerWeight": {"controller_weight_lb", "lb"},
}

// required are the fields every motor must have (the DCMotor constructor).
var required = []string{"voltage", "stallTorque", "stallCurrent", "freeCurrent", "freeSpeed"}

// partIDs maps ReCalc motor names to the part ids used by the other sources
// (wpilib-dcmotor), so one frc_hardware query returns every source's column.
// Names not listed fall back to Part's normalization and become new parts.
var partIDs = map[string]string{
	"Kraken X60": "krakenx60", "Kraken X60 (FOC)": "krakenx60-foc",
	"Kraken X44": "krakenx44", "Kraken X44 (FOC)": "krakenx44-foc",
	"Falcon 500": "falcon500", "Falcon 500 (FOC)": "falcon500-foc",
	"NEO": "neo", "NEO 2.0": "neo2", "NEO Vortex": "neovortex", "NEO 550": "neo550",
	"Minion": "minion", "Minion (Adv Hall)": "minionadvhall", "Thrifty Pulsar": "thriftypulsar",
	"775pro": "vex775pro", "775 RedLine": "775redline", "CIM": "cim", "MiniCIM": "minicim", "BAG": "bag",
	"AM-9015": "andymark9015", "BaneBots 550": "banebotsrs550", "Snowblower": "snowblower",
}

// Part returns the part id for a ReCalc motor name.
func Part(name string) string {
	if id, ok := partIDs[name]; ok {
		return id
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Stats summarizes a parse.
type Stats struct {
	Motors  int // FRC motors emitted
	Skipped int // FTC and other non-FRC motors left out
}

// Parse reads Motor.ts at path and emits one spec per FRC motor. The
// upstream revision is the pinned commit (src.Version), not the fetch ETag.
func Parse(path string, src sources.Source, retrieved time.Time, emit func(index.HWSpec) error) (Stats, error) {
	var st Stats
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	text := string(b)
	consts := map[string]float64{}
	for _, m := range constRe.FindAllStringSubmatch(text, -1) {
		if m[3] != "lb" {
			return st, fmt.Errorf("%w: constant %s has unit %q, want lb", ErrFormat, m[1], m[3])
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return st, fmt.Errorf("%w: constant %s: %w", ErrFormat, m[1], err)
		}
		consts[m[1]] = v
	}
	table := tableRe.FindStringSubmatch(text)
	if table == nil {
		return st, fmt.Errorf("%w: ALL_MOTORS not found in %s", ErrFormat, src.URL)
	}
	seen := map[string]bool{}
	for _, obj := range objectRe.FindAllStringSubmatch(table[1], -1) {
		h, frc, err := motor(obj[1], consts)
		if err != nil {
			return st, err
		}
		if !frc {
			st.Skipped++
			continue
		}
		if seen[h.Part] {
			return st, fmt.Errorf("%w: two motors map to part %q", ErrFormat, h.Part)
		}
		seen[h.Part] = true
		h.Source, h.Season, h.SourceURL, h.UpstreamRev = Source, src.Season, src.BaseURL, src.Version
		h.RetrievedAt, h.License, h.Trust = retrieved, src.License, src.Trust
		if err := emit(h); err != nil {
			return st, err
		}
		st.Motors++
	}
	if st.Motors == 0 {
		return st, fmt.Errorf("%w: no FRC motors in %s", ErrFormat, src.URL)
	}
	return st, nil
}

// motor parses one ALL_MOTORS object. frc is false for FTC/other motors.
func motor(obj string, consts map[string]float64) (h index.HWSpec, frc bool, err error) {
	n := nameRe.FindStringSubmatch(obj)
	if n == nil {
		return h, false, fmt.Errorf("%w: motor without a name", ErrFormat)
	}
	h.Name, h.Part, h.Category = n[1], Part(n[1]), "motor"
	p := programRe.FindStringSubmatch(obj)
	if p == nil {
		return h, false, fmt.Errorf("%w: %s has no intendedProgram", ErrFormat, h.Name)
	}
	if p[1] != "FRC" {
		return h, false, nil
	}
	h.Fields = map[string]float64{}
	for _, m := range measureRe.FindAllStringSubmatch(obj, -1) {
		f, ok := fields[m[1]]
		if !ok {
			continue
		}
		if m[3] != f.unit {
			return h, true, fmt.Errorf("%w: %s %s has unit %q, want %q", ErrFormat, h.Name, m[1], m[3], f.unit)
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return h, true, fmt.Errorf("%w: %s %s: %w", ErrFormat, h.Name, m[1], err)
		}
		h.Fields[f.key] = v
	}
	if c := identRe.FindStringSubmatch(obj); c != nil {
		v, ok := consts[c[1]]
		if !ok {
			return h, true, fmt.Errorf("%w: %s: unknown controller weight %s", ErrFormat, h.Name, c[1])
		}
		if v > 0 { // NO_CONTROLLER_WEIGHT: the controller is integrated (Kraken, Falcon)
			h.Fields[fields["controllerWeight"].key] = v
		}
	}
	for _, r := range required {
		if _, ok := h.Fields[fields[r].key]; !ok {
			return h, true, fmt.Errorf("%w: %s has no %s", ErrFormat, h.Name, r)
		}
	}
	h.Note = note(obj)
	return h, true, nil
}

// note says where ReCalc's numbers come from, from its own dataSource,
// type and vendors fields (plain words only; anything else is dropped).
func note(obj string) string {
	var typ, data string
	for _, m := range stringRe.FindAllStringSubmatch(obj, -1) {
		if !plainRe.MatchString(m[2]) {
			continue
		}
		if m[1] == "type" {
			typ = m[2]
		} else {
			data = m[2]
		}
	}
	var vendors []string
	if v := vendorsRe.FindStringSubmatch(obj); v != nil {
		for _, s := range strings.Split(v[1], ",") {
			if s = strings.Trim(strings.TrimSpace(s), "'"); s != "" && plainRe.MatchString(s) {
				vendors = append(vendors, s)
			}
		}
	}
	var parts []string
	if data != "" {
		// dataSource names who measured the curve, which is often not the
		// seller (CTRE's dyno runs cover REV motors): say so explicitly.
		parts = append(parts, "measured by "+data+" (ReCalc dataSource; not necessarily the vendor)")
	}
	if typ != "" {
		parts = append(parts, strings.ToLower(typ))
	}
	if len(vendors) > 0 {
		parts = append(parts, "sold by "+strings.Join(vendors, ", "))
	}
	if strings.Contains(obj, "controllerWeight: NO_CONTROLLER_WEIGHT") {
		parts = append(parts, "integrated controller (no separate controller weight)")
	}
	return strings.Join(parts, "; ")
}
