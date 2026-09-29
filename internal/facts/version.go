// Package facts holds exact, relational FRC facts (vendordeps, compatibility)
// and the version logic they need. Nothing here is similarity-searched
// (CLAUDE.md invariant 6).
package facts

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareVersions orders FRC-style version strings:
//
//	"26.1.0" < "26.1.3" < "26.2.0";  "2026.0.5" < "2026.1.1";  "v2026.3.4" = "2026.3.4"
//	"27.0.0-alpha-5" < "27.0.0-alpha-6" < "27.0.0-beta-1" < "27.0.0-rc-1" < "27.0.0"
//	"2026.1.26" < "2026.1.26.1"
//
// Numeric segments compare numerically; a pre-release (-alpha/-beta/-rc)
// sorts before the release; unknown suffixes compare lexically. It returns
// -1, 0 or +1.
func CompareVersions(a, b string) int {
	ca, pa := split(a)
	cb, pb := split(b)
	for i := 0; i < max(len(ca), len(cb)); i++ {
		var x, y int
		if i < len(ca) {
			x = ca[i]
		}
		if i < len(cb) {
			y = cb[i]
		}
		if x != y {
			return sign(x - y)
		}
	}
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1 // release > pre-release
	case pb == "":
		return -1
	}
	ra, na := preRank(pa)
	rb, nb := preRank(pb)
	if ra != rb {
		return sign(ra - rb)
	}
	if na != nb {
		return sign(na - nb)
	}
	return strings.Compare(pa, pb)
}

func split(v string) (core []int, pre string) {
	v = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(v)), "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			// non-numeric core segment (rare): treat the rest as pre-release text
			if pre == "" {
				pre = p
			}
			break
		}
		core = append(core, n)
	}
	return core, pre
}

// preRank maps "alpha-5" → (0, 5), "beta-2" → (1, 2), "rc-1" → (2, 1).
func preRank(p string) (rank, n int) {
	kind, num, _ := strings.Cut(p, "-")
	if num == "" {
		// "alpha5" / "rc1"
		i := strings.IndexAny(kind, "0123456789")
		if i > 0 {
			kind, num = kind[:i], kind[i:]
		}
	}
	n, _ = strconv.Atoi(strings.Split(num, "-")[0])
	switch kind {
	case "alpha":
		return 0, n
	case "beta":
		return 1, n
	case "rc":
		return 2, n
	}
	return 1, n // unknown pre-release tags sort between alpha and rc
}

func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}

// Season extracts the FRC season implied by a version: "2026.x" → 2026,
// "26.x" (CTRE / AdvantageKit scheme) → 2026, "27.0.0-alpha" → 2027.
func Season(v string) string {
	core, _ := split(v)
	if len(core) == 0 {
		return ""
	}
	switch y := core[0]; {
	case y >= 2020 && y <= 2039:
		return strconv.Itoa(y)
	case y >= 20 && y <= 39:
		return strconv.Itoa(2000 + y)
	}
	return ""
}

// SeasonFor is Season with vendor numbering quirks: CTRE numbers the next
// season's alphas as YY.70+ of the current year (Phoenix 6 26.70.0-alpha-2
// is the 2027 alpha), so a minor ≥ 50 on a two-digit year means next season.
func SeasonFor(library, v string) string {
	s := Season(v)
	if s == "" || (library != "phoenix6" && library != "phoenix5") {
		return s
	}
	core, _ := split(v)
	if len(core) >= 2 && core[0] >= 20 && core[0] <= 39 && core[1] >= 50 {
		return strconv.Itoa(2001 + core[0])
	}
	return s
}

// VendordepYear is a vendordep's declared year: frcYear ("2026"), or the
// wpilibYear that replaces it from 2027 ("2027_alpha7"); "" when neither.
func VendordepYear(frcYear, wpilibYear any) string {
	for _, v := range []any{frcYear, wpilibYear} {
		if v != nil {
			if s := fmt.Sprint(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// DeclaredSeason is the season of a declared vendordep year ("2026",
// "2027_alpha7" → "2027"), or "".
func DeclaredSeason(year string) string {
	if len(year) >= 4 && (len(year) == 4 || year[4] < '0' || year[4] > '9') {
		if y, err := strconv.Atoi(year[:4]); err == nil && y >= 2020 && y <= 2039 {
			return year[:4]
		}
	}
	return ""
}
