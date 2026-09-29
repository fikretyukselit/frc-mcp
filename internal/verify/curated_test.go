package verify

import (
	"maps"
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
	key := func(k [2]string) string { return k[0] + "/" + k[1] }
	var got []string
	for _, s := range reg.Sources {
		if s.Language != "" && index.RestrictedLicense(s.License) && !slices.Contains(got, s.Library+"/"+s.Language) {
			got = append(got, s.Library+"/"+s.Language)
		}
	}
	slices.Sort(got)
	var want []string
	for k := range maps.Keys(restrictedLibs) {
		want = append(want, key(k))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("license-restricted API tables in sources.yaml = %v, restrictedLibs = %v", got, want)
	}
}
