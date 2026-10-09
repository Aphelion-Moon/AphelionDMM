package maplint

import (
	"fmt"
	"strings"
)

// PlacementReport is the transient feedback for one placement gesture (Add
// stroke, Fill, shape fill or paste). It is presentation data: it never
// changes the map and never crosses the collaboration protocol.
type PlacementReport struct {
	// X, Y, Z locate the most recent tile that received a warning.
	X, Y, Z int
	// Placed is the atom that was placed; the replace action re-resolves it.
	Placed Atom
	// Summary names the broken rules of the most recent warned tile.
	Summary string
	// Warned counts tiles that were placed despite a violation.
	Warned int
	// Skipped counts tiles left untouched because they already held an
	// identical atom under an `identical: true` rule.
	Skipped     int
	SkippedPath string
	// CanReplace is true when the most recent warned tile has existing atoms
	// that "Replace existing" can remove.
	CanReplace bool
	// Paste marks a report that came from a multi-atom paste.
	Paste bool
}

// Empty reports whether there is nothing to show.
func (r PlacementReport) Empty() bool { return r.Warned == 0 && r.Skipped == 0 }

// Message is the one-line feedback text.
func (r PlacementReport) Message() string {
	var parts []string
	if r.Warned > 0 {
		verb := "Placed with a lint warning"
		if r.Paste {
			verb = "Pasted with a lint warning"
		}
		text := fmt.Sprintf("%s at %d,%d,%d: %s", verb, r.X, r.Y, r.Z, r.Summary)
		if r.Warned > 1 {
			text += fmt.Sprintf(" (+%d more tiles)", r.Warned-1)
		}
		parts = append(parts, text)
	}
	if r.Skipped > 0 {
		noun := "tiles"
		if r.Skipped == 1 {
			noun = "tile"
		}
		what := "an identical object"
		if r.SkippedPath != "" {
			what = "an identical " + r.SkippedPath
		}
		parts = append(parts, fmt.Sprintf("Skipped %d %s that already hold %s.", r.Skipped, noun, what))
	}
	return strings.Join(parts, " ")
}

// SameAtom reports whether two atoms have the same path and variable text.
func SameAtom(a, b Atom) bool {
	if a.Path != b.Path || len(a.Vars) != len(b.Vars) {
		return false
	}
	for name, value := range a.Vars {
		if other, ok := b.Vars[name]; !ok || other != value {
			return false
		}
	}
	return true
}

// NewViolations returns the violations of the after tile that the before tile
// did not already have. Atom indices refer to after.
func (rs *RuleSet) NewViolations(file string, before, after []Atom) []Violation {
	if rs == nil || len(after) == 0 {
		return nil
	}
	now := rs.checkPrepared(file, prepare(after, nil))
	if len(now) == 0 {
		return nil
	}
	seen := map[string]int{}
	if len(before) != 0 {
		for _, v := range rs.checkPrepared(file, prepare(before, nil)) {
			seen[newViolationKey(v)]++
		}
	}
	var out []Violation
	for _, v := range now {
		k := newViolationKey(v)
		if seen[k] > 0 {
			seen[k]--
			continue
		}
		out = append(out, v)
	}
	return out
}

// newViolationKey ignores tile indices so reordering a tile does not make an
// old violation look new.
func newViolationKey(v Violation) string {
	return v.RuleFile + "\x00" + v.Rule + "\x00" + v.Message
}

// Describe is the shared summary text for any set of violations.
func Describe(violations []Violation) string { return Verdict{Violations: violations}.Summary() }
