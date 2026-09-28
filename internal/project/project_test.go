package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		dir, season, channel, lang, wpilib string
		vendordeps, warnings               int
	}{
		{"java2026", "2026", "stable", "java", "2026.2.1", 2, 1},
		{"py2026", "2026", "stable", "python", "2026.2.1.1", 0, 0},
		{"cpp2027", "2027", "alpha", "cpp", "2027.0.0-alpha-7", 0, 0},
		{"props2026", "2026", "stable", "java", "2026.2.1", 0, 0},
	} {
		p, err := Detect(filepath.Join("testdata", tc.dir))
		if err != nil {
			t.Fatalf("%s: %v", tc.dir, err)
		}
		if p.Season != tc.season || p.Channel != tc.channel || p.Language != tc.lang || p.WPILib != tc.wpilib ||
			len(p.Vendordeps) != tc.vendordeps || len(p.Warnings) != tc.warnings {
			t.Errorf("%s: %+v", tc.dir, p)
		}
	}
	p, _ := Detect("testdata/java2026")
	if !strings.Contains(p.Warnings[0], "PathplannerLib") || !strings.Contains(p.Warnings[0], "frcYear=2025") {
		t.Errorf("warning = %q", p.Warnings[0])
	}
	if _, err := Detect(t.TempDir()); !errors.Is(err, ErrNotProject) {
		t.Errorf("empty dir: %v", err)
	}
}

func TestPinRoundTrip(t *testing.T) {
	p, _ := Detect("testdata/java2026")
	h := p.PinOf().Encode()
	got, err := DecodePin(h)
	if err != nil || got.Season != "2026" || got.Language != "java" || got.Libs["CTRE-Phoenix (v6)"] != "26.1.0" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"", "pin1.!!!", "nope", "pin1." + strings.Repeat("A", 10000)} {
		if _, err := DecodePin(bad); err == nil {
			t.Errorf("DecodePin(%q) accepted", bad[:min(len(bad), 20)])
		}
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.gradle")
	if err := os.WriteFile(outside, []byte(`id "edu.wpi.first.GradleRIO" version "2026.2.1"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "build.gradle")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := Detect(root); !errors.Is(err, ErrNotProject) {
		t.Fatalf("symlink outside root must not be read: %v", err)
	}
}
