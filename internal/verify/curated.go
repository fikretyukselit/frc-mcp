package verify

import (
	"fmt"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// curator is the optional part of a Resolver that lists the curated
// migration rules (retrieve.Engine). Rules are the project's own cited data
// and ship in every index, so they check libraries whose API tables do not.
type curator interface {
	Migrations() []index.Migration
}

// restrictedLibs are the libraries whose API tables carry a LicenseRef-*
// license (data/sources.yaml): built locally, left out of the published
// index until the publisher grants redistribution (docs/sources.md §0.2).
var restrictedLibs = map[string]string{"phoenix6": "CTRE", "revlib": "REV Robotics"}

// RestrictedPublisher names the publisher of a library whose API table is
// license-restricted, or "".
func RestrictedPublisher(lib string) string { return restrictedLibs[lib] }

// LibraryName is the display name of a library id ("phoenix6" → "Phoenix 6").
func LibraryName(lib string) string { return libName(lib) }

// noTable is the coverage note for a library without a symbol table.
func noTable(lib, lang, season string) string {
	what := season + " API"
	if lang != "java" {
		what = season + " " + lang + " API"
	}
	if pub := restrictedLibs[lib]; pub != "" {
		return fmt.Sprintf("none (no %s table: %s grants no redistribution license, so the published index leaves it out; "+
			"`frc-mcp index run` builds it locally. Curated renames and removals are still checked)", what, pub)
	}
	return "none (no " + what + " indexed)"
}

// curatedCheck reports a reference to a name that curated rules say is not
// part of the season: renamed or removed in an earlier transition (REVLib's
// CANSparkMax, gone since 2025), or introduced by a later one. It is used
// where no symbol table can decide. Signature and behavior rules keep the
// name and are not findings here.
//
// Without a table the rule is the only evidence, and a renamed name may
// linger deprecated for the season of the change: a change from an earlier
// season is an error, one in this season or a later name a warning.
func curatedCheck(r Resolver, fqn, lang, season string, emit func(Finding)) {
	c, ok := r.(curator)
	if !ok {
		return
	}
	for _, mr := range c.Migrations() {
		if mr.Language != lang || mr.From == mr.To || (mr.Kind != "rename" && mr.Kind != "move" && mr.Kind != "removed") {
			continue
		}
		f := Finding{Symbol: fqn, Severity: "warning", Kind: "wrong_season", SourceURL: mr.Citation}
		switch {
		case mr.From == fqn && mr.ToSeason <= season:
			if mr.ToSeason < season {
				f.Severity = "error"
			}
			if mr.To == "" {
				f.Message = fmt.Sprintf("%s was removed in %s %s (curated rule %s)", fqn, libName(mr.Library), mr.ToSeason, mr.RuleID)
				f.Fix = mr.Notes
			} else {
				f.Message = fmt.Sprintf("%s was renamed to %s in %s %s (curated rule %s)", fqn, mr.To, libName(mr.Library), mr.ToSeason, mr.RuleID)
				f.Fix = "use " + mr.To
			}
		case mr.To == fqn && mr.FromSeason >= season:
			f.Message = fmt.Sprintf("%s is the %s %s name; season %s uses %s (curated rule %s)", fqn, libName(mr.Library), mr.ToSeason, season, mr.From, mr.RuleID)
			f.Fix = "use " + mr.From + ", or pin season " + mr.ToSeason
		default:
			continue
		}
		emit(f)
		return
	}
}
