// Package project detects an FRC robot project's pin set — WPILib season and
// version, language, and vendordep versions — from its build files
// (docs/mcp-surface.md frc_context). Reads are scoped to a fixed set of files
// under the project root, symlinks must stay inside it, and sizes are capped
// (docs/security.md §2.3).
package project

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxFileBytes  = 1 << 20
	maxVendordeps = 64
)

// Vendordep is one vendordeps/*.json entry.
type Vendordep struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Version string `json:"version"`
	FRCYear string `json:"frc_year"`
	UUID    string `json:"uuid"`
	JSONURL string `json:"json_url,omitempty"`
}

// Project is a detected pin set.
type Project struct {
	Root       string      `json:"root"`
	Season     string      `json:"frc_season,omitempty"`
	Channel    string      `json:"channel,omitempty"`
	Language   string      `json:"language,omitempty"`
	WPILib     string      `json:"wpilib_version,omitempty"`
	Vendordeps []Vendordep `json:"vendordeps"`
	Files      []string    `json:"files"`
	Warnings   []string    `json:"warnings,omitempty"`
}

var (
	gradleRIO = regexp.MustCompile(`id\s*\(?\s*["'](edu\.wpi\.first|org\.wpilib)\.GradleRIO["']\s*\)?\s*version\s*["']([^"']+)["']`)
	robotpy   = regexp.MustCompile(`(?m)^\s*robotpy_version\s*=\s*["']([^"']+)["']`)
	seasonOf  = regexp.MustCompile(`^(20[2-3][0-9])`)
)

// ErrNotProject means no FRC build file was found under the root.
var ErrNotProject = errors.New("no FRC project found (looked for build.gradle, build.gradle.kts, pyproject.toml)")

// Detect inspects root. It never reads outside root.
func Detect(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return nil, err
	}
	p := &Project{Root: abs, Vendordeps: []Vendordep{}, Files: []string{}}
	read := func(rel string) ([]byte, bool) {
		b, err := readScoped(abs, rel)
		if err != nil {
			return nil, false
		}
		p.Files = append(p.Files, rel)
		return b, true
	}
	found := false
	for _, f := range []string{"build.gradle", "build.gradle.kts"} {
		if b, ok := read(f); ok {
			found = true
			if m := gradleRIO.FindSubmatch(b); m != nil {
				p.WPILib = string(m[2])
			} else {
				p.Warnings = append(p.Warnings, f+": GradleRIO plugin version not found")
			}
			switch {
			case exists(abs, "src/main/cpp") || exists(abs, "src/main/include"):
				p.Language = "cpp"
			default:
				p.Language = "java"
			}
			break
		}
	}
	if b, ok := read("pyproject.toml"); ok {
		if m := robotpy.FindSubmatch(b); m != nil {
			found = true
			p.WPILib = string(m[1])
			p.Language = "python"
		}
	}
	if !found {
		return nil, ErrNotProject
	}
	if m := seasonOf.FindStringSubmatch(p.WPILib); m != nil {
		p.Season = m[1]
	}
	switch v := strings.ToLower(p.WPILib); {
	case strings.Contains(v, "alpha"):
		p.Channel = "alpha"
	case strings.Contains(v, "beta"):
		p.Channel = "beta"
	case v != "":
		p.Channel = "stable"
	}

	entries, _ := os.ReadDir(filepath.Join(abs, "vendordeps"))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) > maxVendordeps {
		p.Warnings = append(p.Warnings, fmt.Sprintf("vendordeps/: %d files, only the first %d were read", len(names), maxVendordeps))
		names = names[:maxVendordeps]
	}
	for _, n := range names {
		b, ok := read("vendordeps/" + n)
		if !ok {
			continue
		}
		var v struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			FRCYear any    `json:"frcYear"`
			UUID    string `json:"uuid"`
			JSONURL string `json:"jsonUrl"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			p.Warnings = append(p.Warnings, "vendordeps/"+n+": invalid JSON")
			continue
		}
		vd := Vendordep{File: "vendordeps/" + n, Name: v.Name, Version: v.Version, FRCYear: fmt.Sprint(v.FRCYear),
			UUID: v.UUID, JSONURL: v.JSONURL}
		if v.FRCYear == nil {
			vd.FRCYear = ""
		}
		p.Vendordeps = append(p.Vendordeps, vd)
		if yr := seasonOf.FindString(vd.FRCYear); p.Season != "" && yr != "" && yr != p.Season {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s (%s) declares frcYear=%s but the project is WPILib %s (season %s); update or re-import it",
				vd.Name, vd.File, vd.FRCYear, p.WPILib, p.Season))
		}
	}
	return p, nil
}

// readScoped reads root/rel if it resolves (after symlinks) inside root and
// is a regular file under the size cap.
func readScoped(root, rel string) ([]byte, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return nil, errors.New("path escapes project root")
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	if r, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(r, "..") {
		return nil, errors.New("path escapes project root")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxFileBytes {
		return nil, errors.New("not a regular file under the size cap")
	}
	return io.ReadAll(io.LimitReader(f, maxFileBytes))
}

func exists(root, rel string) bool {
	st, err := os.Stat(filepath.Join(root, rel))
	return err == nil && st.IsDir()
}

// Pin is the portable pin set carried by the opaque handle.
type Pin struct {
	V        int               `json:"v"`
	Season   string            `json:"s,omitempty"`
	Channel  string            `json:"c,omitempty"`
	Language string            `json:"l,omitempty"`
	Libs     map[string]string `json:"libs,omitempty"`
}

// Encode renders the handle (stateless: any server instance can decode it).
func (p Pin) Encode() string {
	p.V = 1
	b, _ := json.Marshal(p)
	return "pin1." + base64.RawURLEncoding.EncodeToString(b)
}

// DecodePin parses a handle produced by Encode.
func DecodePin(s string) (Pin, error) {
	rest, ok := strings.CutPrefix(s, "pin1.")
	if !ok {
		return Pin{}, errors.New("not a frc_context pin handle")
	}
	b, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(b) > 4096 {
		return Pin{}, errors.New("malformed pin handle")
	}
	var p Pin
	if err := json.Unmarshal(b, &p); err != nil || p.V != 1 {
		return Pin{}, errors.New("malformed pin handle")
	}
	return p, nil
}

// PinOf converts a detected project into a pin.
func (p *Project) PinOf() Pin {
	pin := Pin{Season: p.Season, Channel: p.Channel, Language: p.Language, Libs: map[string]string{}}
	if p.WPILib != "" {
		pin.Libs["wpilib"] = p.WPILib
	}
	for _, v := range p.Vendordeps {
		if v.Name != "" && v.Version != "" {
			pin.Libs[v.Name] = v.Version
		}
	}
	return pin
}
