// Package hwdata loads the curated hardware specs in data/hardware/*.yaml:
// parts that no machine-readable upstream describes (swerve modules,
// encoders, IMUs), each number copied from the vendor page it cites. The rows
// feed the hw_spec table next to the adapter-built sources and are served by
// frc_hardware, labeled with the vendor as their source; they are never
// merged with another source's row.
//
// A file looks like:
//
//	license: MIT     # of this file: our own compilation of cited facts
//	trust: vendor
//	shard: hardware
//	parts:
//	  - part: sdsmk4i
//	    name: SDS MK4i
//	    category: swerve_module
//	    source: sds
//	    citation: https://www.swervedrivespecialties.com/products/mk4i-swerve-module
//	    checked: 2026-09-29
//	    fields: {steer_ratio: 21.428571428571427}
//	    note: >-
//	      Steering ratio 150/7:1 as stated on the product page.
//
// Validation is strict: unknown keys, a field without a unit suffix, a
// non-https citation or a duplicate (part, source) fail the index build.
package hwdata

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// ErrData is returned (wrapped) for an invalid data file.
var ErrData = errors.New("hwdata: invalid hardware data")

// Season marks rows that do not depend on the FRC season: frc_hardware
// returns them for every season.
const Season = "all"

// Categories are the part categories frc_hardware serves.
var Categories = []string{"motor", "encoder", "imu", "swerve_module"}

// File is one data/hardware/*.yaml file.
type File struct {
	License string `yaml:"license"`
	Trust   string `yaml:"trust"`
	Shard   string `yaml:"shard"`
	Parts   []Part `yaml:"parts"`
}

// Part is one curated row.
type Part struct {
	Part     string             `yaml:"part"`
	Name     string             `yaml:"name"`
	Category string             `yaml:"category"`
	Source   string             `yaml:"source"`
	Citation string             `yaml:"citation"`
	SeeAlso  []string           `yaml:"see_also"`
	Checked  string             `yaml:"checked"`
	Fields   map[string]float64 `yaml:"fields"`
	Note     string             `yaml:"note"`
}

// Row is a validated hardware spec and the shard it belongs in.
type Row struct {
	Shard string
	Spec  index.HWSpec
}

var (
	idRe = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
	// partRe: curated part ids are plain lower-case alphanumerics, the form
	// frc_hardware's name normalization produces ("SDS MK4i" → "sdsmk4i").
	partRe = regexp.MustCompile(`^[a-z0-9]+$`)
	// units are the accepted field-key suffixes. A dimensionless gear ratio
	// is named "…ratio" or "ratio_<option>" instead (steer_ratio,
	// drive_ratio_l2).
	units   = []string{"v", "a", "ma", "nm", "rpm", "w", "lb", "g", "in", "mm", "hz", "us", "ms", "bits", "cpr", "deg", "deg_per_hour", "deg_per_min", "deg_per_s", "pct"}
	ratioRe = regexp.MustCompile(`^(?:[a-z0-9]+_)*ratio(?:_[a-z0-9]+)*$`)
	keyRe   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$`)
)

// ValidKey reports whether a field key carries a unit suffix (or is a ratio).
func ValidKey(k string) bool {
	if ratioRe.MatchString(k) {
		return true
	}
	if !keyRe.MatchString(k) {
		return false
	}
	for _, u := range units {
		if strings.HasSuffix(k, "_"+u) && len(k) > len(u)+1 {
			return true
		}
	}
	return false
}

// Load reads and validates every *.yaml file in dir. A missing dir yields
// no rows. (part, source) must be unique across all files.
func Load(dir string) ([]Row, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Row
	seen := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rows, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		for _, r := range rows {
			k := r.Spec.Part + "/" + r.Spec.Source
			if prev, dup := seen[k]; dup {
				return nil, fmt.Errorf("%w: %s: part %s from source %s is also in %s", ErrData, filepath.Base(p), r.Spec.Part, r.Spec.Source, prev)
			}
			seen[k] = filepath.Base(p)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// Parse validates one data file.
func Parse(b []byte) ([]Row, error) {
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}
	if strings.TrimSpace(f.License) == "" || !slices.Contains([]string{"official", "vendor", "community"}, f.Trust) || !idRe.MatchString(f.Shard) {
		return nil, fmt.Errorf("%w: need license, trust (official|vendor|community) and shard (got %q %q %q)", ErrData, f.License, f.Trust, f.Shard)
	}
	if len(f.Parts) == 0 {
		return nil, fmt.Errorf("%w: no parts", ErrData)
	}
	var out []Row
	for i, p := range f.Parts {
		h, err := p.spec(f)
		if err != nil {
			return nil, fmt.Errorf("%w: parts[%d] %s: %w", ErrData, i, p.Part, err)
		}
		out = append(out, Row{Shard: f.Shard, Spec: h})
	}
	return out, nil
}

func (p Part) spec(f File) (index.HWSpec, error) {
	var h index.HWSpec
	switch {
	case !partRe.MatchString(p.Part):
		return h, errors.New("part must be lower-case letters and digits (e.g. sdsmk4i)")
	case strings.TrimSpace(p.Name) == "":
		return h, errors.New("missing name")
	case !slices.Contains(Categories, p.Category):
		return h, fmt.Errorf("category %q is not one of %v", p.Category, Categories)
	case !idRe.MatchString(p.Source):
		return h, errors.New("source must be a lower-case vendor label (e.g. sds, ctre)")
	case len(p.Fields) == 0:
		return h, errors.New("no fields")
	}
	for _, c := range append([]string{p.Citation}, p.SeeAlso...) {
		if u, err := url.Parse(c); err != nil || u.Scheme != "https" || u.Host == "" {
			return h, fmt.Errorf("citation %q must be an https URL", c)
		}
	}
	checked, err := time.Parse(time.DateOnly, p.Checked)
	if err != nil {
		return h, fmt.Errorf("checked %q must be the YYYY-MM-DD date the citation was read", p.Checked)
	}
	for k, v := range p.Fields {
		if !ValidKey(k) {
			return h, fmt.Errorf("field %q needs a unit suffix (_%s) or must be a ratio", k, strings.Join(units, ", _"))
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return h, fmt.Errorf("field %q is not a finite number", k)
		}
	}
	note := strings.Join(strings.Fields(p.Note), " ")
	if len(p.SeeAlso) > 0 {
		note = strings.TrimSpace(note + " Also from: " + strings.Join(p.SeeAlso, ", ") + ".")
	}
	return index.HWSpec{Part: p.Part, Name: strings.TrimSpace(p.Name), Category: p.Category, Source: p.Source, Season: Season,
		Fields: p.Fields, Note: note, SourceURL: p.Citation, UpstreamRev: "checked " + p.Checked, RetrievedAt: checked,
		License: f.License, Trust: f.Trust}, nil
}
