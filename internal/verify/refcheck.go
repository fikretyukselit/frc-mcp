package verify

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// Check verifies source code in any supported language against the pinned
// season: Java through JavaWith, C++ and Python through their references
// (qualified names, imports, members on receivers of known type). The same
// severity rules apply to every language.
func Check(ctx context.Context, r Resolver, code, language, season string, opt Options) Result {
	switch language {
	case "java":
		return JavaWith(ctx, r, code, season, opt)
	case "cpp", "python":
		return refCheck(ctx, r, code, language, season)
	}
	return Result{Findings: []Finding{}, Coverage: map[string]string{}}
}

// Libraries by top-level Python module and C++ namespace.
var (
	pyLibs = map[string]string{"wpilib": "wpilib", "wpimath": "wpilib", "wpiutil": "wpilib", "ntcore": "wpilib",
		"commands2": "wpilib", "hal": "wpilib", "cscore": "wpilib", "robotpy_apriltag": "wpilib", "wpinet": "wpilib",
		"wpilib_units": "wpilib", "romi": "wpilib", "xrp": "wpilib", "phoenix6": "phoenix6", "rev": "revlib",
		"photonlibpy": "photonvision", "pathplannerlib": "pathplannerlib", "choreo": "choreolib"}
	cppLibs = map[string]string{"frc": "wpilib", "frc2": "wpilib", "wpi": "wpilib", "units": "wpilib", "hal": "wpilib",
		"ctre": "phoenix6", "rev": "revlib", "photon": "photonvision", "pathplanner": "pathplannerlib", "choreo": "choreolib"}
)

// LibraryOf maps a Python or C++ FQN to its library id ("" if not indexed).
func LibraryOf(fqn, language string) string {
	switch language {
	case "python":
		root, _, _ := strings.Cut(fqn, ".")
		return pyLibs[root]
	case "cpp":
		root, _, _ := strings.Cut(fqn, "::")
		return cppLibs[root]
	}
	if _, lib := vendorRoot(fqn); lib != "" {
		return lib
	}
	if isCovered(fqn) {
		return "wpilib"
	}
	return ""
}

func refCheck(ctx context.Context, r Resolver, code, lang, season string) Result {
	res := Result{Coverage: map[string]string{}, Findings: []Finding{}}
	indexed := map[string]bool{}
	vr, _ := r.(versioner)
	isIndexed := func(lib string) bool {
		ok, seen := indexed[lib]
		if !seen {
			ok = vr != nil && vr.LibraryVersion(ctx, lib, season, lang) != ""
			indexed[lib] = ok
			if ok {
				res.Coverage[lib] = partialRefs
			} else {
				res.Coverage[lib] = "none (no " + season + " " + lang + " API indexed)"
			}
		}
		return ok
	}
	okTypes := map[string]bool{}
	for _, ref := range Refs(code, lang) {
		lib := LibraryOf(ref.Symbol, lang)
		if lib == "" || !isIndexed(lib) {
			continue
		}
		emit := func(f Finding) {
			f.Line, f.Col = ref.Line, ref.Col
			res.Findings = append(res.Findings, f)
		}
		res.Checked++
		if !ref.Member {
			if lang == "python" && movedModule(ctx, r, ref.Symbol, season, emit) {
				continue
			}
			if checkType(ctx, r, ref.Symbol, season, lang, emit) {
				okTypes[ref.Symbol] = true
			}
			continue
		}
		owner, member, _ := strings.Cut(ref.Symbol, "#")
		if !okTypes[owner] && len(exact(ctx, r, owner, season, lang)) == 0 {
			continue // the type itself is reported (or unknown)
		}
		// C++ "Type::Name" and Python "Type.Name" may be nested types.
		sep := "::"
		if lang == "python" {
			sep = "."
		}
		if len(exact(ctx, r, owner+sep+member, season, lang)) > 0 {
			continue
		}
		checkMember(ctx, r, owner, member, season, lang, emit)
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		if res.Findings[i].Line != res.Findings[j].Line {
			return res.Findings[i].Line < res.Findings[j].Line
		}
		return res.Findings[i].Col < res.Findings[j].Col
	})
	for _, f := range res.Findings {
		switch f.Severity {
		case "error":
			res.Errors++
		case "warning":
			res.Warnings++
		}
	}
	return res
}

const partialRefs = "partial (qualified names, imports, members on receivers of known type)"

// movedModule handles Python names imported from a module path the pinned
// season does not use while the same top-level package still exports the
// class elsewhere (wpimath.geometry.Pose2d vs wpimath.Pose2d). Python
// packages re-export freely, so this is a warning with the indexed path,
// never an error.
func movedModule(ctx context.Context, r Resolver, fqn, season string, emit func(Finding)) bool {
	if len(exact(ctx, r, fqn, season, "python")) > 0 {
		return false
	}
	name := index.SimpleName(fqn)
	root, _, _ := strings.Cut(fqn, ".")
	cands, _ := r.Symbols(ctx, index.SymbolQuery{Name: name, Season: season, Language: "python", Limit: 20})
	var paths []string
	for _, c := range cands {
		if c.Season == season && !strings.Contains(c.FQN, "#") && index.SimpleName(c.FQN) == name && strings.HasPrefix(c.FQN, root+".") {
			paths = append(paths, c.FQN)
		}
	}
	if len(paths) == 0 {
		return false
	}
	mod := paths[0][:strings.LastIndexByte(paths[0], '.')]
	emit(Finding{Symbol: fqn, Severity: "warning", Kind: "wrong_season",
		Message: fmt.Sprintf("%s is not at this module path in the %s API; it is %s", fqn, season, paths[0]),
		Fix:     fmt.Sprintf("from %s import %s", mod, name)})
	return true
}
