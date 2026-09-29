package facts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// Catalog is the vendordep catalog across seasons, built from shard fact
// tables. It is immutable and safe for concurrent use.
type Catalog struct {
	bySeason map[string][]index.Vendordep // season → all versions
}

// NewCatalog indexes vendordep rows.
func NewCatalog(rows []index.Vendordep) *Catalog {
	c := &Catalog{bySeason: map[string][]index.Vendordep{}}
	for _, r := range rows {
		c.bySeason[r.Season] = append(c.bySeason[r.Season], r)
	}
	for s := range c.bySeason {
		rs := c.bySeason[s]
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].Name != rs[j].Name {
				return rs[i].Name < rs[j].Name
			}
			return CompareVersions(rs[i].Version, rs[j].Version) < 0
		})
	}
	return c
}

// Empty reports whether no catalog data is loaded.
func (c *Catalog) Empty() bool { return c == nil || len(c.bySeason) == 0 }

// aliases map common names, products and abbreviations to catalog names.
var aliases = map[string]string{
	"rev": "REVLib", "revlib": "REVLib", "sparkmax": "REVLib", "sparkflex": "REVLib", "neo": "REVLib", "vortex": "REVLib",
	"phoenix6": "CTRE-Phoenix (v6)", "phoenix": "CTRE-Phoenix (v6)", "ctre": "CTRE-Phoenix (v6)", "talonfx": "CTRE-Phoenix (v6)",
	"kraken": "CTRE-Phoenix (v6)", "falcon": "CTRE-Phoenix (v6)", "cancoder": "CTRE-Phoenix (v6)", "pigeon2": "CTRE-Phoenix (v6)",
	"phoenix5": "CTRE-Phoenix (v5)", "talonsrx": "CTRE-Phoenix (v5)", "victorspx": "CTRE-Phoenix (v5)",
	"photon": "photonlib", "photonvision": "photonlib", "photonlib": "photonlib",
	"pathplanner": "PathplannerLib", "pathplannerlib": "PathplannerLib",
	"choreo": "ChoreoLib", "choreolib": "ChoreoLib",
	"advantagekit": "AdvantageKit", "akit": "AdvantageKit",
	"yagsl": "Yet Another Generic Swerve Library (YAGSL)", "yams": "Yet Another Mechanism System",
	"navx": "Studica", "studica": "Studica", "redux": "ReduxLib", "reduxlib": "ReduxLib", "canandmag": "ReduxLib",
	"thrifty": "ThriftyLib", "thriftylib": "ThriftyLib", "nova": "ThriftyLib",
	"lasercan": "libgrapplefrc", "grapple": "libgrapplefrc", "maplesim": "maplesim", "maple": "maplesim",
	"urcl": "URCL", "doglog": "DogLog", "pwf": "PlayingWithFusion", "playingwithfusion": "PlayingWithFusion",
	"andymark": "AndyMark AM Library", "amlib": "AndyMark AM Library", "lumyn": "LumynLabs",
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Match is a resolved library in one season.
type Match struct {
	Latest   index.Vendordep
	Versions []string // ascending
}

// Resolve finds a library by name, alias, file name or uuid in a season.
// Exact/alias matches win; otherwise a unique substring match is accepted.
// It returns candidate names when ambiguous.
func (c *Catalog) Resolve(query, season string) (*Match, []string) {
	rows := c.bySeason[season]
	q := norm(query)
	if q == "" {
		return nil, nil
	}
	target := ""
	if a, ok := aliases[q]; ok {
		target = a
	}
	byUUID := map[string][]index.Vendordep{}
	for _, r := range rows {
		byUUID[r.UUID] = append(byUUID[r.UUID], r)
	}
	var exact, partial []string // uuids
	for u, vs := range byUUID {
		n := vs[0].Name
		switch {
		case u == query || (target != "" && n == target) || norm(n) == q || norm(strings.TrimSuffix(vs[0].FileName, ".json")) == q:
			exact = append(exact, u)
		case strings.Contains(norm(n), q):
			partial = append(partial, u)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = partial
	}
	if len(pick) != 1 {
		// Prefer the non-replay variant when a vendor ships a replay twin.
		var nonReplay []string
		for _, u := range pick {
			if !strings.Contains(strings.ToLower(byUUID[u][0].Name), "replay") {
				nonReplay = append(nonReplay, u)
			}
		}
		if len(nonReplay) == 1 {
			pick = nonReplay
		}
	}
	if len(pick) != 1 {
		var names []string
		for _, u := range append(exact, partial...) {
			names = append(names, byUUID[u][0].Name)
		}
		sort.Strings(names)
		return nil, names
	}
	vs := byUUID[pick[0]]
	m := &Match{Latest: vs[len(vs)-1]}
	for _, v := range vs {
		m.Versions = append(m.Versions, v.Version)
	}
	return m, nil
}

// ByUUID returns the library's versions in a season (ascending), or nil.
func (c *Catalog) ByUUID(uuid, season string) *Match {
	var vs []index.Vendordep
	for _, r := range c.bySeason[season] {
		if r.UUID == uuid {
			vs = append(vs, r)
		}
	}
	if len(vs) == 0 {
		return nil
	}
	m := &Match{Latest: vs[len(vs)-1]}
	for _, v := range vs {
		m.Versions = append(m.Versions, v.Version)
	}
	return m
}

// Names lists catalog libraries in a season (for instructive errors).
func (c *Catalog) Names(season string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range c.bySeason[season] {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Installed is a vendordep present in a project.
type Installed struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	UUID    string `json:"uuid,omitempty"`
	FRCYear string `json:"frc_year,omitempty"`
}

// ParseInstalled accepts either a raw vendordep JSON document or
// "name@version" shorthand.
func ParseInstalled(s string) (Installed, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		var v struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			UUID    string `json:"uuid"`
			FRCYear any    `json:"frcYear"`
			// 2027 vendordeps name it wpilibYear ("2027_alpha7").
			WPILibYear any `json:"wpilibYear"`
		}
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return Installed{}, fmt.Errorf("vendordep JSON: %w", err)
		}
		return Installed{Name: v.Name, Version: v.Version, UUID: v.UUID, FRCYear: VendordepYear(v.FRCYear, v.WPILibYear)}, nil
	}
	name, ver, ok := strings.Cut(s, "@")
	if !ok || name == "" || ver == "" {
		return Installed{}, fmt.Errorf("%q: use a vendordep JSON document or name@version", s)
	}
	return Installed{Name: name, Version: ver}, nil
}

// Finding is one compatibility verdict for an installed vendordep.
type Finding struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest,omitempty"`
	Status    string `json:"status"` // ok | outdated | wrong_year | newer_than_catalog | unknown | conflict
	Message   string `json:"message"`
	Fix       string `json:"fix,omitempty"`
}

// Check evaluates an installed set against the catalog for a season.
func (c *Catalog) Check(installed []Installed, season string) []Finding {
	var out []Finding
	uuids := map[string]string{} // uuid → name, for conflict detection
	for _, in := range installed {
		m := (*Match)(nil)
		if in.UUID != "" {
			m = c.ByUUID(in.UUID, season)
		}
		if m == nil {
			m, _ = c.Resolve(in.Name, season)
		}
		f := Finding{Name: in.Name, Installed: in.Version}
		switch m {
		case nil:
			f.Status = "unknown"
			f.Message = fmt.Sprintf("%s is not in the WPILib %s vendordep catalog; verify it with its vendor", in.Name, season)
		default:
			uuids[m.Latest.UUID] = m.Latest.Name
			f.Latest = m.Latest.Version
			if m.Latest.JSONURL != "" {
				f.Fix = "./gradlew vendordep --url=" + m.Latest.JSONURL
			}
			yr := DeclaredSeason(in.FRCYear)
			if yr == "" {
				yr = in.FRCYear
			}
			switch cmp := CompareVersions(in.Version, m.Latest.Version); {
			case in.FRCYear != "" && yr != season:
				f.Status = "wrong_year"
				f.Message = fmt.Sprintf("%s %s declares year %s but the project targets %s; install the %s version (%s)",
					in.Name, in.Version, in.FRCYear, season, season, m.Latest.Version)
			case cmp < 0:
				f.Status = "outdated"
				f.Message = fmt.Sprintf("%s %s is installed; %s is the newest %s version in the catalog", in.Name, in.Version, m.Latest.Version, season)
			case cmp > 0:
				f.Status = "newer_than_catalog"
				f.Message = fmt.Sprintf("%s %s is newer than the catalog's %s (fine if the vendor published it; the catalog may lag)", in.Name, in.Version, m.Latest.Version)
				f.Fix = ""
			default:
				f.Status = "ok"
				f.Message = fmt.Sprintf("%s %s is the newest %s version", in.Name, in.Version, season)
				f.Fix = ""
			}
		}
		out = append(out, f)
	}
	// Declared conflicts between installed libraries.
	for _, in := range installed {
		m := c.ByUUID(in.UUID, season)
		if m == nil {
			m, _ = c.Resolve(in.Name, season)
		}
		if m == nil {
			continue
		}
		for _, cf := range m.Latest.Conflicts {
			if other, ok := uuids[cf.UUID]; ok {
				out = append(out, Finding{Name: in.Name, Installed: in.Version, Status: "conflict",
					Message: fmt.Sprintf("%s conflicts with %s: %s", m.Latest.Name, other, cf.ErrorMessage)})
			}
		}
	}
	return out
}
