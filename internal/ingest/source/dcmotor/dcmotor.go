// Package dcmotor reads WPILib's DCMotor.java (src.URL: the raw file at a
// release tag) and turns every `public static DCMotor getX(int numMotors)`
// factory into a hardware spec row: nominal voltage, stall torque, stall
// current, free current and free speed, exactly as WPILib simulates the
// motor. These are the "wpilib-dcmotor" source for frc_hardware; they are
// labeled as such and never merged with vendor or dyno numbers.
package dcmotor

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

var (
	factoryRe = regexp.MustCompile(`(?s)/\*\*(.*?)\*/\s*public static DCMotor (get\w+)\(int numMotors\)\s*\{(.*?)\n  \}`)
	ctorRe    = regexp.MustCompile(`(?s)new DCMotor\(\s*([\d.]+),\s*([\d.]+),\s*([\d.]+),\s*([\d.]+),\s*Units\.rotationsPerMinuteToRadiansPerSecond\(([\d.]+)\)`)
	nameRe    = regexp.MustCompile(`(?i)gearbox of (.+?) motors`)
	fromRe    = regexp.MustCompile(`//\s*(From\s+\S+)`)
)

// Stats summarizes a parse.
type Stats struct{ Motors int }

// Part normalizes a factory name: getKrakenX60Foc → "krakenx60-foc".
func Part(factory string) string {
	p := strings.ToLower(strings.TrimPrefix(factory, "get"))
	if base, ok := strings.CutSuffix(p, "foc"); ok {
		return base + "-foc"
	}
	return p
}

// Parse reads DCMotor.java at path and emits one spec per factory.
func Parse(path string, src sources.Source, rev string, retrieved time.Time, emit func(index.HWSpec) error) (Stats, error) {
	var st Stats
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	for _, m := range factoryRe.FindAllStringSubmatch(string(b), -1) {
		doc, factory, body := m[1], m[2], m[3]
		c := ctorRe.FindStringSubmatch(body)
		if c == nil {
			continue
		}
		var v [5]float64
		for i := range v {
			if v[i], err = strconv.ParseFloat(c[i+1], 64); err != nil {
				return st, fmt.Errorf("dcmotor: %s: %w", factory, err)
			}
		}
		name := strings.TrimPrefix(factory, "get")
		if n := nameRe.FindStringSubmatch(strings.Join(strings.Fields(strings.ReplaceAll(doc, "*", " ")), " ")); n != nil {
			name = strings.TrimSuffix(strings.TrimSpace(n[1]), " brushless")
		}
		if strings.HasSuffix(factory, "Foc") && !strings.Contains(strings.ToLower(name), "foc") {
			name += " (FOC)"
		}
		note := ""
		if f := fromRe.FindStringSubmatch(body); f != nil {
			note = f[1]
		}
		h := index.HWSpec{Part: Part(factory), Name: name, Category: "motor", Source: "wpilib-dcmotor", Season: src.Season,
			Fields: map[string]float64{"nominal_voltage_v": v[0], "stall_torque_nm": v[1], "stall_current_a": v[2],
				"free_current_a": v[3], "free_speed_rpm": v[4]},
			Factory: factory, Note: note, SourceURL: src.BaseURL, UpstreamRev: rev, RetrievedAt: retrieved,
			License: src.License, Trust: src.Trust}
		if err := emit(h); err != nil {
			return st, err
		}
		st.Motors++
	}
	if st.Motors == 0 {
		return st, fmt.Errorf("dcmotor: no factories found in %s (upstream format changed?)", src.URL)
	}
	return st, nil
}
