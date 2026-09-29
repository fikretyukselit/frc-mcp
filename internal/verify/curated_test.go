package verify

import (
	"maps"
	"slices"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// restrictedLibs must name exactly the libraries whose API sources carry a
// LicenseRef-* license, so the coverage note never lies about the reason.
func TestRestrictedLibsMatchSources(t *testing.T) {
	reg, err := sources.Load("../../data/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range reg.Sources {
		if s.Language != "" && index.RestrictedLicense(s.License) {
			got[s.Library] = true
		}
	}
	if want := slices.Sorted(maps.Keys(restrictedLibs)); !slices.Equal(slices.Sorted(maps.Keys(got)), want) {
		t.Errorf("license-restricted API libraries in sources.yaml = %v, restrictedLibs = %v", slices.Sorted(maps.Keys(got)), want)
	}
}
