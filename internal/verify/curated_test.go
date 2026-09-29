package verify

import (
	"slices"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// restrictedLibs must name exactly the (library, language) API tables whose
// sources carry a LicenseRef-* license, so the coverage note never lies
// about the reason.
func TestRestrictedLibsMatchSources(t *testing.T) {
	reg, err := sources.Load("../../data/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range reg.Sources {
		k := s.Library + "/" + s.Language + map[bool]string{true: " eula"}[sources.Prohibited(s.License)]
		if s.Language != "" && index.RestrictedLicense(s.License) && !slices.Contains(got, k) {
			got = append(got, k)
		}
	}
	slices.Sort(got)
	var want []string
	for k, r := range restrictedLibs {
		want = append(want, k[0]+"/"+k[1]+map[bool]string{true: " eula"}[r.eula])
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("license-restricted API tables in sources.yaml = %v, restrictedLibs = %v", got, want)
	}
}
