// Package specpage reads a vendor's motor spec page published by GitBook
// (the page's `.md` rendition, e.g. docs.revrobotics.com/brushless/neo/vortex.md
// or docs.wcproducts.com/…/motor-performance.md) and turns its parameter
// table into hardware spec rows (adapter "gitbook-spec-table").
//
// Only rows whose parameter is in a fixed list are read, and each must carry
// the unit that list expects: a page that renames a parameter or changes a
// unit fails the build instead of storing a number under the wrong key.
// Values are copied, never derived; a page that states no nominal voltage
// gets none. A page with GitBook tabs yields one row per tab: a
// "Trapezoidal" tab is the part itself, an "FOC" tab is the part's "-foc"
// variant (the ids wpilib-dcmotor uses).
package specpage

import (
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// ErrFormat is returned (wrapped) when a page no longer looks as expected.
var ErrFormat = errors.New("specpage: unexpected spec page format")

type param struct {
	key  string
	unit string // expected unit text after the number, compared case-insensitively without spaces
}

// params maps a normalized parameter label to its hw_spec key and unit.
var params = map[string]param{
	"nominaloperatingvoltage": {"nominal_voltage_v", "v"},
	"motorkv":                 {"kv_rpm_per_v", "kv"},
	"freespeed":               {"free_speed_rpm", "rpm"},
	"freerunningcurrent":      {"free_current_a", "a"},
	"freecurrent":             {"free_current_a", "a"},
	"stallcurrent":            {"stall_current_a", "a"},
	"stalltorque":             {"stall_torque_nm", "nm"},
	"peakoutputpower":         {"peak_power_w", "w"},
	"peakpower":               {"peak_power_w", "w"},
	"kt":                      {"kt_mnm_per_a", "mnm/a"},
	"maxefficiency":           {"max_efficiency_pct", "%w(out)/w(in)"},
	"currentmaxefficiency":    {"max_efficiency_current_a", "a"},
}

// required are the fields every emitted row must have.
var required = []string{"free_speed_rpm", "stall_torque_nm", "stall_current_a", "free_current_a"}

var (
	tabRe    = regexp.MustCompile(`(?s)\{% tab title="([^"]*)" %\}(.*?)\{% endtab %\}`)
	tabsRe   = regexp.MustCompile(`(?s)\{% tabs %\}.*?\{% endtabs %\}`)
	titleRe  = regexp.MustCompile(`(?m)^# (.+)$`)
	tableRe  = regexp.MustCompile(`(?s)<table[^>]*>(.*?)</table>`)
	rowRe    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe   = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
	sepRe    = regexp.MustCompile(`^\|[\s:|-]+\|$`)
	numberRe = regexp.MustCompile(`^(\d[\d,]*(?:\.\d+)?)\s*(.*)$`)
	plainRe  = regexp.MustCompile(`^[A-Za-z0-9 .()\-]+$`)
)

// Stats summarizes a parse.
type Stats struct{ Rows int }

// Parse reads the page at path and emits one spec per variant (tab).
func Parse(path string, src sources.Source, rev string, retrieved time.Time, emit func(index.HWSpec) error) (Stats, error) {
	var st Stats
	if src.Hardware == nil {
		return st, fmt.Errorf("specpage: source %s has no hardware block", src.ID)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	page := string(b)
	title := ""
	if m := titleRe.FindStringSubmatch(page); m != nil && plainRe.MatchString(strings.TrimSpace(m[1])) {
		title = strings.TrimSpace(m[1])
	}
	type segment struct{ tab, text string }
	segs := []segment{{"", tabsRe.ReplaceAllString(page, "")}}
	for _, m := range tabRe.FindAllStringSubmatch(page, -1) {
		segs = append(segs, segment{m[1], m[2]})
	}
	seen := map[string]bool{}
	for _, sg := range segs {
		fields, err := readFields(sg.text)
		if err != nil {
			return st, fmt.Errorf("%s %q: %w", src.ID, sg.tab, err)
		}
		if len(fields) == 0 {
			continue
		}
		for _, k := range required {
			if _, ok := fields[k]; !ok {
				return st, fmt.Errorf("%w: %s %q has no %s", ErrFormat, src.ID, sg.tab, k)
			}
		}
		part, name, err := variant(src.Hardware, sg.tab)
		if err != nil {
			return st, fmt.Errorf("%s: %w", src.ID, err)
		}
		if seen[part] {
			return st, fmt.Errorf("%w: %s: two tables for part %s", ErrFormat, src.ID, part)
		}
		seen[part] = true
		note := "From the vendor spec page"
		if title != "" {
			note += " \"" + title + "\""
		}
		if sg.tab != "" && plainRe.MatchString(sg.tab) {
			note += ", tab \"" + sg.tab + "\""
		}
		h := index.HWSpec{Part: part, Name: name, Category: "motor", Source: src.Hardware.Source, Season: src.Season,
			Fields: fields, Note: note, SourceURL: src.BaseURL, UpstreamRev: rev, RetrievedAt: retrieved,
			License: src.License, Trust: src.Trust}
		if err := emit(h); err != nil {
			return st, err
		}
		st.Rows++
	}
	if st.Rows == 0 {
		return st, fmt.Errorf("%w: no motor parameter table in %s", ErrFormat, src.URL)
	}
	return st, nil
}

// variant maps a tab title to the part id and name it describes.
func variant(hw *sources.Hardware, tab string) (part, name string, err error) {
	l := strings.ToLower(tab)
	switch {
	case tab == "" || strings.Contains(l, "trapezoidal"):
		return hw.Part, hw.Name, nil
	case strings.Contains(l, "foc"):
		return hw.Part + "-foc", hw.Name + " (FOC)", nil
	}
	return "", "", fmt.Errorf("%w: tab %q is neither trapezoidal nor FOC", ErrFormat, tab)
}

// readFields collects the known parameters of every table in text.
func readFields(text string) (map[string]float64, error) {
	fields := map[string]float64{}
	for _, row := range rows(text) {
		if len(row) < 2 || len(row) > 3 {
			continue
		}
		p, ok := params[label(row[0])]
		if !ok {
			continue
		}
		v, err := value(strings.Join(row[1:], " "), p.unit)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrFormat, row[0], err)
		}
		if _, dup := fields[p.key]; dup {
			return nil, fmt.Errorf("%w: %s given twice", ErrFormat, p.key)
		}
		fields[p.key] = v
	}
	return fields, nil
}

// rows returns the cells of every HTML and Markdown table row in text.
func rows(text string) [][]string {
	var out [][]string
	for _, t := range tableRe.FindAllStringSubmatch(text, -1) {
		for _, r := range rowRe.FindAllStringSubmatch(t[1], -1) {
			var cells []string
			for _, c := range cellRe.FindAllStringSubmatch(r[1], -1) {
				cells = append(cells, clean(c[1]))
			}
			out = append(out, cells)
		}
	}
	for _, line := range strings.Split(tableRe.ReplaceAllString(text, ""), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") || sepRe.MatchString(line) {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, clean(c))
		}
		out = append(out, cells)
	}
	return out
}

func clean(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, ""))), " ")
}

// label normalizes a parameter name: "Free Speed`" → "freespeed",
// "Current @ Max. Efficiency" → "currentmaxefficiency".
func label(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// value parses "6,000 RPM" / "3.6 Nm" / "87% W(out) / W(in)" and checks the unit.
func value(s, unit string) (float64, error) {
	m := numberRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("no number in %q", s)
	}
	if got := strings.ToLower(strings.ReplaceAll(m[2], " ", "")); got != unit {
		return 0, fmt.Errorf("unit %q, want %q", m[2], unit)
	}
	return strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
}
