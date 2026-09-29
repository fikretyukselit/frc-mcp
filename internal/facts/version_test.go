package facts

import (
	"sort"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	ordered := []string{"26.0.0", "26.1.0", "26.1.3", "26.2.0", "26.70.0-alpha-2", "27.0.0-alpha-5", "27.0.0-alpha-6",
		"27.0.0-beta-1", "27.0.0-rc-1", "27.0.0", "2026.0.5", "v2026.1.1", "2026.1.26", "2026.1.26.1", "2026.3.4"}
	for i := range ordered {
		for j := range ordered {
			want := sign(i - j)
			if got := CompareVersions(ordered[i], ordered[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	if CompareVersions("v2026.3.4", "2026.3.4") != 0 {
		t.Error("v prefix must be ignored")
	}
	shuffled := []string{"2026.0.2", "2026.0.10", "2026.0.1"}
	sort.Slice(shuffled, func(i, j int) bool { return CompareVersions(shuffled[i], shuffled[j]) < 0 })
	if shuffled[2] != "2026.0.10" {
		t.Errorf("numeric compare: %v", shuffled)
	}
}

func TestSeason(t *testing.T) {
	for v, want := range map[string]string{"26.1.0": "2026", "2026.0.5": "2026", "v2026.3.4": "2026",
		"27.0.0-alpha-5": "2027", "2027.0.0-alpha-7": "2027", "5.36.0": "", "0.4.0-beta": ""} {
		if got := Season(v); got != want {
			t.Errorf("Season(%s) = %q, want %q", v, got, want)
		}
	}
}

func FuzzCompareVersions(f *testing.F) {
	f.Add("27.0.0-alpha-5", "2026.1.26.1")
	f.Fuzz(func(t *testing.T, a, b string) {
		if CompareVersions(a, b) != -CompareVersions(b, a) {
			t.Fatalf("not antisymmetric: %q %q", a, b)
		}
		if CompareVersions(a, a) != 0 {
			t.Fatalf("not reflexive: %q", a)
		}
	})
}

func TestSeasonFor(t *testing.T) {
	for _, tc := range []struct{ lib, v, want string }{
		{"phoenix6", "26.3.0", "2026"}, {"phoenix6", "26.70.0-alpha-2", "2027"}, {"revlib", "2026.0.5", "2026"},
		{"advantagekit", "27.0.0-alpha-6", "2027"}, {"wpilib", "2027.0.0-alpha-7", "2027"}, {"yagsl", "2026.9.27", "2026"},
		{"photonvision", "Dev", ""},
	} {
		if got := SeasonFor(tc.lib, tc.v); got != tc.want {
			t.Errorf("SeasonFor(%s, %s) = %q, want %q", tc.lib, tc.v, got, tc.want)
		}
	}
}
