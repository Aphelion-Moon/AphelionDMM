package maplint

// EditViolations reports the rules about an atom alone (banned and
// banned_variables) that after breaks and before did not: what an edit that
// rewrites one atom in place, such as rotation, would add to the map.
// Neighbor rules need the whole tile and are never reported. Rules with
// skip_files apply as if the map file were unknown, the stricter reading.
func (rs *RuleSet) EditViolations(before, after Atom) []Violation {
	if rs == nil {
		return nil
	}
	existing := map[string]int{}
	for _, v := range rs.AtomViolations(before) {
		existing[editKey(v)]++
	}
	var out []Violation
	for _, v := range rs.AtomViolations(after) {
		// A turn between two values one rule already rejects adds nothing.
		if k := editKey(v); existing[k] > 0 {
			existing[k]--
			continue
		}
		out = append(out, v)
	}
	return out
}

// AtomViolations reports the rules about an atom alone that it breaks.
func (rs *RuleSet) AtomViolations(a Atom) []Violation {
	if rs == nil {
		return nil
	}
	var out []Violation
	for _, v := range rs.checkPrepared("", prepare([]Atom{a}, nil)) {
		switch v.Kind {
		case KindBanned, KindBannedVariables, KindBannedVariable:
			out = append(out, v)
		}
	}
	return out
}

func editKey(v Violation) string {
	return v.RuleFile + "\x00" + v.Rule + "\x00" + v.Subject + "\x00" + v.Kind.String() + "\x00" + v.Variable
}
