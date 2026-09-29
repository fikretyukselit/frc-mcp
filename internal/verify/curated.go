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

// restrictedLibs are the API tables whose sources carry a LicenseRef-*
// license (data/sources.yaml), keyed by library and language: REVLib's C++
// headers and RobotPy wheels are BSD-3-Clause, only its Java table is
// restricted. eula marks a license that forbids distribution (CTRE's C++
// header EULA): never in a shared index. The others state no license and
// are served only with --include-unlicensed (docs/sources.md §0.2).
var restrictedLibs = map[[2]string]restriction{
	{"phoenix6", "java"}: {"CTRE", false}, {"phoenix6", "cpp"}: {"CTRE", true}, {"phoenix6", "python"}: {"CTRE", false},
	{"revlib", "java"}: {"REV Robotics", false},
}

type restriction struct {
	publisher string
	eula      bool
}

// RestrictedPublisher names the publisher of a library whose API table in a
// language is license-restricted, or "".
func RestrictedPublisher(lib, lang string) string {
	return restrictedLibs[[2]string{lib, lang}].publisher
}

// NoTableReason says why a license-restricted table may be missing from an
// index ("" for a library that is not restricted).
func NoTableReason(lib, lang string) string {
	r, ok := restrictedLibs[[2]string{lib, lang}]
	switch {
	case !ok:
		return ""
	case r.eula:
		return r.publisher + "'s license forbids redistributing it, so no shared index carries it"
	}
	return r.publisher + " states no redistribution license, so shared servers leave it out unless run with --include-unlicensed"
}

// LibraryName is the display name of a library id ("phoenix6" → "Phoenix 6").
func LibraryName(lib string) string { return libName(lib) }

// noTable is the coverage note for a library without a symbol table.
func noTable(lib, lang, season string) string {
	what := season + " API"
	if lang != "java" {
		what = season + " " + lang + " API"
	}
	if why := NoTableReason(lib, lang); why != "" {
		return fmt.Sprintf("none (no %s table in this index: %s; `frc-mcp index run` builds it locally. "+
			"Curated renames and removals are still checked)", what, why)
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
