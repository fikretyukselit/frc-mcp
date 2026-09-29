// Package migrate maps API symbols across FRC seasons for frc_migrate
// (docs/mcp-surface.md). Three sources are combined, most authoritative first:
//
//   - curated: data/migrations/*.yaml, one rule per upstream change, each with
//     a citation; validated against both seasons' symbol tables at index build;
//   - upstream: the library's own deprecation note ("use X instead");
//   - generated: apisym.Diff's same-name package moves between seasons.
//
// It produces mappings only and never rewrites code.
package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// ErrRules is returned (wrapped) for an invalid rule file.
var ErrRules = errors.New("migrate: invalid rules")

// File is one data/migrations/*.yaml file: the rules of one library for one
// season transition.
type File struct {
	Library    string `yaml:"library"`
	FromSeason string `yaml:"from_season"`
	ToSeason   string `yaml:"to_season"`
	Rules      []Rule `yaml:"rules"`
}

// Rule is one curated change, possibly in several languages.
type Rule struct {
	ID       string            `yaml:"id"`
	Kind     string            `yaml:"kind"`
	From     map[string]string `yaml:"from"`
	To       map[string]string `yaml:"to"`
	Notes    string            `yaml:"notes"`
	Citation string            `yaml:"citation"`
	Verified *bool             `yaml:"verified"`
}

var (
	idRe     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	seasonRe = regexp.MustCompile(`^20[2-3][0-9]$`)
)

// Load reads and validates every *.yaml file in dir and expands the rules to
// one index.Migration per language. A missing dir yields no rules.
func Load(dir string) ([]index.Migration, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []index.Migration
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		ms, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		out = append(out, ms...)
	}
	return out, nil
}

// Parse validates one rule file and expands it.
func Parse(b []byte) ([]index.Migration, error) {
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRules, err)
	}
	if f.Library == "" || !seasonRe.MatchString(f.FromSeason) || !seasonRe.MatchString(f.ToSeason) || f.FromSeason >= f.ToSeason {
		return nil, fmt.Errorf("%w: need library and from_season < to_season (got %q %q→%q)", ErrRules, f.Library, f.FromSeason, f.ToSeason)
	}
	seen := map[string]bool{}
	var out []index.Migration
	for _, r := range f.Rules {
		if !idRe.MatchString(r.ID) || seen[r.ID] {
			return nil, fmt.Errorf("%w: rule id %q must be unique kebab-case", ErrRules, r.ID)
		}
		seen[r.ID] = true
		if len(r.From) == 0 {
			return nil, fmt.Errorf("%w: rule %s has no from", ErrRules, r.ID)
		}
		for lang := range r.To {
			if _, ok := r.From[lang]; !ok {
				return nil, fmt.Errorf("%w: rule %s: to.%s without from.%s", ErrRules, r.ID, lang, lang)
			}
		}
		langs := make([]string, 0, len(r.From))
		for l := range r.From {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		for _, lang := range langs {
			m := index.Migration{RuleID: r.ID, Library: f.Library, Language: lang, FromSeason: f.FromSeason,
				ToSeason: f.ToSeason, Kind: r.Kind, From: strings.TrimSpace(r.From[lang]), To: strings.TrimSpace(r.To[lang]),
				Notes: strings.Join(strings.Fields(r.Notes), " "), Citation: strings.TrimSpace(r.Citation),
				Verified: r.Verified == nil || *r.Verified}
			if err := m.Validate(); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrRules, err)
			}
			if m.Kind != "signature" && m.Kind != "behavior" && m.From == m.To {
				return nil, fmt.Errorf("%w: rule %s (%s): from == to needs kind signature or behavior", ErrRules, r.ID, lang)
			}
			if m.Notes == "" && (m.Kind == "removed" || m.Kind == "signature" || m.Kind == "behavior") {
				return nil, fmt.Errorf("%w: rule %s: kind %s needs notes saying what to do", ErrRules, r.ID, m.Kind)
			}
			out = append(out, m)
		}
	}
	return out, nil
}
