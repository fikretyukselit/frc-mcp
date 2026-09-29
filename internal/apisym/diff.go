// Package apisym holds cross-version reasoning over API symbol tables.
package apisym

import (
	"strings"

	"github.com/fikretyukselit/frc-mcp/internal/index"
)

// Diff annotates two consecutive versions of one library/language symbol
// table (docs/architecture.md §3.1 item 8):
//
//   - symbols of old missing from new get RemovedIn = newVersion and, when a
//     unique counterpart exists in new (same type name, most similar package —
//     e.g. edu.wpi.first.math.kinematics → org.wpilib.math.kinematics), a
//     Replacement pointing at it;
//   - symbols of new missing from old get Since = newVersion (unless set).
//
// Tables are modified in place. The result is a generated migration map that
// frc_migrate later curates; it never overrides an upstream Replacement.
func Diff(old, new []index.Symbol, newVersion string) (removed, added, mapped int) {
	oldSet := make(map[string]bool, len(old))
	for i := range old {
		oldSet[old[i].FQN] = true
	}
	newSet := make(map[string]bool, len(new))
	byType := map[string][]string{} // simple (possibly nested) type name → new type FQNs
	for i := range new {
		s := &new[i]
		newSet[s.FQN] = true
		if !strings.Contains(s.FQN, "#") {
			byType[typeName(s.FQN)] = append(byType[typeName(s.FQN)], s.FQN)
		}
		if !oldSet[s.FQN] && s.Since == "" {
			s.Since = newVersion
			added++
		}
	}
	for i := range old {
		s := &old[i]
		if newSet[s.FQN] {
			continue
		}
		s.RemovedIn = newVersion
		removed++
		if s.Replacement != "" {
			continue
		}
		owner, member, _ := strings.Cut(s.FQN, "#")
		target := bestMatch(owner, byType[typeName(owner)])
		if target == "" {
			continue
		}
		if member != "" {
			target += "#" + member
		}
		if newSet[target] {
			s.Replacement, s.ReplacementSrc = target, "generated"
			mapped++
		}
	}
	return removed, added, mapped
}

// segments splits a Java ("a.b.C") or C++ ("a::b::C") qualified name.
func segments(fqn string) (parts []string, sep string) {
	if strings.Contains(fqn, "::") {
		return strings.Split(fqn, "::"), "::"
	}
	return strings.Split(fqn, "."), "."
}

// typeName strips the package: lowercase-leading segments are package parts,
// so "org.wpilib.hardware.led.AddressableLED.Buffer" → "AddressableLED.Buffer"
// and "frc::sim::XboxControllerSim" → "XboxControllerSim".
func typeName(fqn string) string {
	parts, sep := segments(fqn)
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			return strings.Join(parts[i:], sep)
		}
	}
	return fqn
}

func pkgOf(fqn string) []string {
	parts, _ := segments(fqn)
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			return parts[:i]
		}
	}
	return parts
}

// bestMatch picks the candidate whose package shares the longest suffix with
// owner's package; ties (ambiguous moves) return "".
func bestMatch(owner string, cands []string) string {
	if len(cands) == 1 {
		return cands[0]
	}
	op := pkgOf(owner)
	best, bestScore, tie := "", -1, false
	for _, c := range cands {
		cp := pkgOf(c)
		n := 0
		for n < len(op) && n < len(cp) && op[len(op)-1-n] == cp[len(cp)-1-n] {
			n++
		}
		switch {
		case n > bestScore:
			best, bestScore, tie = c, n, false
		case n == bestScore:
			tie = true
		}
	}
	if tie || bestScore <= 0 {
		return ""
	}
	return best
}
