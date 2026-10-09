package maplint

import (
	"fmt"
	"sort"
	"strings"
)

// AuditRuleFile names audit findings in reports; it is not a repository file.
const AuditRuleFile = "editor audit"

// Audit finds edits no repository rule covers but that change nothing in
// game, which editor features such as rotation can create in bulk:
// a variable set to its type's own default, and a dir on a sprite with a
// single direction. It needs the environment, so it is attached to a rule set
// only for whole-map scans and fixes, never for placement checks.
type Audit struct {
	Types TypeTree
	// Dirs returns an icon state's direction count; ok false means unknown.
	Dirs func(icon, state string) (dirs int, ok bool)
}

// WithAudit returns a rule set that also reports audit findings.
func (rs *RuleSet) WithAudit(a *Audit) *RuleSet {
	if rs == nil || a == nil || a.Types == nil {
		return rs
	}
	c := *rs
	c.audit = a
	return &c
}

func (a *Audit) check(tile []pAtom, out *[]Violation) {
	for idx := range tile {
		atom := &tile[idx]
		if len(atom.Vars) == 0 {
			continue
		}
		seq := 0
		emit := func(kind Kind, rule, variable, message string) {
			*out = append(*out, Violation{
				RuleFile: AuditRuleFile, Rule: rule, Kind: kind, Message: message,
				Subject: atom.Path, AtomIndex: idx, NeighborIndex: -1, Variable: variable,
				Paths: []string{atom.Path},
				// After every repository rule for the same atom.
				order: [4]int{1 << 30, int(kind), idx, seq},
			})
			seq++
		}
		names := sortedKeys(atom.Vars)
		redundant := map[string]bool{}
		for _, name := range names {
			if initial, ok := a.Types.Value(atom.Path, name); ok && sameValue(initial, atom.Vars[name]) {
				redundant[name] = true
			}
		}
		if dir, ok := atom.Vars["dir"]; ok && !redundant["dir"] && a.Dirs != nil && !dirMayBeFunctional(atom.Path) {
			icon, state := a.value(atom.Atom, "icon"), a.value(atom.Atom, "icon_state")
			if dirs, known := a.Dirs(icon, state); known && dirs == 1 {
				emit(KindInertDir, "inert dir", "dir", fmt.Sprintf("Typepath %s has a dir edit its sprite cannot show (%s has one direction): dir = %s", atom.Path, state, dir))
			}
		}
		for _, name := range names {
			if redundant[name] {
				emit(KindRedundantEdit, "redundant edit", name, fmt.Sprintf("Typepath %s has an edit equal to its default: %s = %s", atom.Path, name, atom.Vars[name]))
			}
		}
	}
}

// dirMayBeFunctional mirrors editing.DirMayBeFunctional (maplint stays free of
// editor imports): machinery and mob dir can matter in game whatever the map
// sprite shows, e.g. a thermomachine's pipe side.
func dirMayBeFunctional(path string) bool {
	for _, root := range []string{"/obj/machinery", "/mob"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// value is the atom's effective text value of name with DM quotes removed.
func (a *Audit) value(atom Atom, name string) string {
	raw, ok := atom.Vars[name]
	if !ok {
		raw, _ = a.Types.Value(atom.Path, name)
	}
	return strings.Trim(strings.TrimSpace(raw), `'"`)
}

// sortViolations orders by file, rule, atom and sequence; audit findings
// follow every repository rule.
func sortViolations(out []Violation) {
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a].order, out[b].order
		for k := 0; k < 4; k++ {
			if x[k] != y[k] {
				return x[k] < y[k]
			}
		}
		return false
	})
}
