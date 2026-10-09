package maplint

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// FixKind is one family of automatic lint fixes.
type FixKind int

const (
	// FixRemoveDuplicate removes the later copy of an `identical: true` pair.
	FixRemoveDuplicate FixKind = iota
	// FixRemoveSuperseded removes an unedited atom when a subtype of it on the
	// same tile breaks a banned_neighbors rule (lattice under a catwalk).
	FixRemoveSuperseded
	// FixPromoteSubtype replaces banned variable edits with the unique subtype
	// that declares exactly those values (pixel_y = 32 -> directional/north).
	FixPromoteSubtype
	// FixStripVariables deletes banned variable edits. Names and descriptions
	// are never stripped; positional variables only from areas.
	FixStripVariables
	// FixRemoveBanned deletes a banned object. Turfs and areas are kept.
	FixRemoveBanned
	// FixCapitalizeText title-cases a name or description a pattern rejects.
	FixCapitalizeText
	// FixStripPositional deletes banned placement edits (pixel offsets, dir,
	// piping and cable layer) from non-area atoms. It is opt-in.
	FixStripPositional
	// FixStripRedundant deletes edits equal to the type default (audit).
	FixStripRedundant
	// FixStripInertDir deletes a dir edit on a single-direction sprite (audit).
	FixStripInertDir
	// FixMoveFixtureLight moves light_range, light_power and light_color on a
	// light fixture to brightness, bulb_power and bulb_colour, and removes
	// light_on. It is opt-in: the fixture then looks as the edit intended.
	FixMoveFixtureLight

	fixKindCount
)

// FixKindList is every kind in presentation order.
var FixKindList = []FixKind{FixRemoveDuplicate, FixRemoveSuperseded, FixPromoteSubtype, FixCapitalizeText, FixStripVariables, FixStripRedundant, FixStripInertDir, FixRemoveBanned, FixStripPositional, FixMoveFixtureLight}

func (k FixKind) String() string {
	switch k {
	case FixRemoveDuplicate:
		return "Remove identical duplicates"
	case FixRemoveSuperseded:
		return "Remove atoms superseded by a subtype"
	case FixPromoteSubtype:
		return "Replace variable edits with a subtype"
	case FixStripVariables:
		return "Strip banned variable edits"
	case FixRemoveBanned:
		return "Remove banned objects"
	case FixCapitalizeText:
		return "Capitalize names"
	case FixStripPositional:
		return "Strip placement edits"
	case FixStripRedundant:
		return "Strip edits equal to the default"
	case FixStripInertDir:
		return "Strip dir on single-direction sprites"
	case FixMoveFixtureLight:
		return "Move light edits on fixtures to bulb variables"
	}
	return ""
}

// Description explains what the kind changes on the map.
func (k FixKind) Description() string {
	switch k {
	case FixRemoveDuplicate:
		return "Deletes the later of two atoms with the same path and variables."
	case FixRemoveSuperseded:
		return "Deletes an unedited atom when a subtype of it on the same tile replaces it, e.g. a lattice under a catwalk."
	case FixPromoteSubtype:
		return "Swaps an atom to the only subtype whose defaults equal the banned edits, e.g. pixel_y = 32 to directional/north."
	case FixStripVariables:
		return "Deletes banned variable edits so the type default applies. Names and descriptions are never deleted; pixel offsets and dir only from areas, which ignore them."
	case FixRemoveBanned:
		return "Deletes objects whose type a rule bans outright. Turfs and areas are never deleted."
	case FixCapitalizeText:
		return "Title-cases a name or description a rule rejects, e.g. \"chemistry shutters\" to \"Chemistry Shutters\"."
	case FixStripPositional:
		return "Deletes banned pixel offset, dir and piping/cable layer edits when no subtype matches. This can move or turn objects or detach them from a network; review the result."
	case FixStripRedundant:
		return "Deletes variable edits whose value is already the type's default. Nothing changes in game; the map file gets smaller."
	case FixStripInertDir:
		return "Deletes dir edits on atoms whose icon state has one direction, such as walls and plain floors after a rotation. The sprite cannot show a facing."
	case FixMoveFixtureLight:
		return "Light fixtures overwrite light_range, light_power and light_color in game with brightness, bulb_power and bulb_colour. This moves each edit to its bulb variable (an existing bulb edit wins) and removes light_on, which fixtures ignore. The fixture then shows the colour or power the edit asked for; review the result."
	}
	return ""
}

// FixKinds is a set of enabled fix kinds.
type FixKinds uint32

// AllFixes enables every kind.
const AllFixes FixKinds = 1<<fixKindCount - 1

// DefaultFixes leaves out the kinds that can change how the map looks in game.
const DefaultFixes = AllFixes &^ (1 << FixStripPositional) &^ (1 << FixMoveFixtureLight)

func (s FixKinds) Has(k FixKind) bool         { return s&(1<<k) != 0 }
func (s FixKinds) With(k FixKind) FixKinds    { return s | 1<<k }
func (s FixKinds) Without(k FixKind) FixKinds { return s &^ (1 << k) }
func (s FixKinds) Toggle(k FixKind) FixKinds  { return s ^ 1<<k }

// TypeTree answers the environment questions subtype promotion needs. Paths
// are DM type paths; a type's parent is its path minus the last segment.
type TypeTree interface {
	// Subtypes lists every strict descendant of path in a stable order.
	Subtypes(path string) []string
	// OwnVars lists the variables path's own definition sets.
	OwnVars(path string) []string
	// Value is the initial value of name on path, as DM source text.
	Value(path, name string) (string, bool)
}

// Fix is one accepted change.
type Fix struct {
	Kind     FixKind
	RuleFile string
	Rule     string
	Detail   string
}

// TileFix is the planned result for one tile.
type TileFix struct {
	// Atoms is the fixed tile. Source[i] is the index in the input of the atom
	// Atoms[i] came from, so callers can keep instance identity.
	Atoms  []Atom
	Source []int
	Fixes  []Fix
	// Remaining are the violations the plan could not resolve.
	Remaining []Violation
}

// Changed reports whether the plan alters the tile.
func (f TileFix) Changed() bool { return len(f.Fixes) != 0 }

// positionalVars place or connect an object. Deleting an edit to one moves or
// turns it, or detaches it from its pipe or cable network, so outside areas
// they are resolved by promotion or by the opt-in FixStripPositional.
var positionalVars = map[string]bool{"pixel_x": true, "pixel_y": true, "pixel_w": true, "pixel_z": true, "dir": true, "piping_layer": true, "cable_layer": true}

// FixTile plans automatic fixes for one tile. Each candidate is applied to a
// copy and kept only when the tile then has strictly fewer violations and no
// violation it did not have before, so a fix never trades one problem for
// another. atoms is not modified. types may be nil, which disables promotion.
func (rs *RuleSet) FixTile(file string, atoms []Atom, types TypeTree, kinds FixKinds) TileFix {
	cur := tileState{atoms: cloneAtoms(atoms), source: make([]int, len(atoms))}
	for i := range cur.source {
		cur.source[i] = i
	}
	var fixes []Fix
	violations := rs.CheckTileInFile(file, cur.atoms)
	// Every accepted step lowers the violation count, so this terminates; the
	// bound only guards against a rule set that defeats that argument.
	for steps := 0; len(violations) != 0 && steps < 4*len(atoms)+16; steps++ {
		next, applied, ok := rs.improve(file, cur, violations, types, kinds)
		if !ok {
			break
		}
		cur, fixes = next, append(fixes, applied...)
		violations = rs.CheckTileInFile(file, cur.atoms)
	}
	return TileFix{Atoms: cur.atoms, Source: cur.source, Fixes: fixes, Remaining: violations}
}

type tileState struct {
	atoms  []Atom
	source []int
}

func (t tileState) without(index int) tileState {
	return tileState{
		atoms:  append(append([]Atom(nil), t.atoms[:index]...), t.atoms[index+1:]...),
		source: append(append([]int(nil), t.source[:index]...), t.source[index+1:]...),
	}
}

func (t tileState) replacing(index int, a Atom) tileState {
	atoms := append([]Atom(nil), t.atoms...)
	atoms[index] = a
	return tileState{atoms: atoms, source: append([]int(nil), t.source...)}
}

type candidate struct {
	next  tileState
	fixes []Fix
}

// improve returns the first candidate, in violation order, that validates.
func (rs *RuleSet) improve(file string, cur tileState, violations []Violation, types TypeTree, kinds FixKinds) (tileState, []Fix, bool) {
	for _, v := range violations {
		for _, c := range candidatesFor(v, cur, types, kinds) {
			after := rs.CheckTileInFile(file, c.next.atoms)
			if len(after) < len(violations) && len(rs.NewViolations(file, cur.atoms, c.next.atoms)) == 0 {
				return c.next, c.fixes, true
			}
		}
	}
	return cur, nil, false
}

func candidatesFor(v Violation, cur tileState, types TypeTree, kinds FixKinds) []candidate {
	fix := func(k FixKind, detail string) Fix {
		return Fix{Kind: k, RuleFile: v.RuleFile, Rule: v.Rule, Detail: detail}
	}
	subject := v.AtomIndex
	if subject < 0 || subject >= len(cur.atoms) {
		return nil
	}
	var out []candidate
	switch v.Kind {
	case KindBannedNeighbor:
		other := v.NeighborIndex
		if other < 0 || other >= len(cur.atoms) {
			return nil
		}
		if v.Identical {
			if kinds.Has(FixRemoveDuplicate) {
				later := max(subject, other)
				out = append(out, candidate{cur.without(later), []Fix{fix(FixRemoveDuplicate, "Removed duplicate "+cur.atoms[later].Path)}})
			}
			return out
		}
		if !kinds.Has(FixRemoveSuperseded) {
			return nil
		}
		for _, pair := range [2][2]int{{subject, other}, {other, subject}} {
			general, specific := cur.atoms[pair[0]], cur.atoms[pair[1]]
			if len(general.Vars) == 0 && isStrictSubtype(specific.Path, general.Path) && removable(general.Path) {
				out = append(out, candidate{cur.without(pair[0]), []Fix{fix(FixRemoveSuperseded, "Removed "+general.Path+" under "+specific.Path)}})
			}
		}
	case KindBannedVariable, KindBannedVariables:
		a := cur.atoms[subject]
		banned := []string{v.Variable}
		if v.Kind == KindBannedVariables {
			banned = sortedKeys(a.Vars)
		}
		if kinds.Has(FixPromoteSubtype) && types != nil {
			if path, vars, ok := promote(a, banned, types); ok {
				out = append(out, collapse(cur, subject, Atom{Path: path, Vars: vars}, kinds, fix(FixPromoteSubtype, "Replaced "+describeEdits(a, banned)+" on "+a.Path+" with "+path)))
			}
		}
		if kinds.Has(FixCapitalizeText) && v.Kind == KindBannedVariable && textVars[v.Variable] {
			for _, value := range capitalizedVariants(a.Vars[v.Variable]) {
				vars := maps.Clone(a.Vars)
				vars[v.Variable] = value
				out = append(out, collapse(cur, subject, Atom{Path: a.Path, Vars: vars}, kinds, fix(FixCapitalizeText, fmt.Sprintf("Changed %s on %s from %s to %s", v.Variable, a.Path, a.Vars[v.Variable], value))))
			}
		}
		strip := FixStripVariables
		if anyPositional(banned) && !isArea(a.Path) {
			strip = FixStripPositional
		}
		if kinds.Has(strip) && !anyText(banned) {
			vars := maps.Clone(a.Vars)
			for _, name := range banned {
				delete(vars, name)
			}
			out = append(out, collapse(cur, subject, Atom{Path: a.Path, Vars: nilIfEmpty(vars)}, kinds, fix(strip, "Removed "+describeEdits(a, banned)+" from "+a.Path)))
		}
	case KindRedundantEdit, KindInertDir:
		kind := FixStripRedundant
		if v.Kind == KindInertDir {
			kind = FixStripInertDir
		}
		a := cur.atoms[subject]
		if _, ok := a.Vars[v.Variable]; !kinds.Has(kind) || !ok {
			return nil
		}
		vars := maps.Clone(a.Vars)
		delete(vars, v.Variable)
		out = append(out, collapse(cur, subject, Atom{Path: a.Path, Vars: nilIfEmpty(vars)}, kinds, fix(kind, "Removed "+describeEdits(a, []string{v.Variable})+" from "+a.Path)))
	case KindFixtureLight:
		a := cur.atoms[subject]
		value, ok := a.Vars[v.Variable]
		if !kinds.Has(FixMoveFixtureLight) || !ok {
			return nil
		}
		vars := maps.Clone(a.Vars)
		delete(vars, v.Variable)
		detail := "Removed " + describeEdits(a, []string{v.Variable}) + " from " + a.Path
		if bulb := fixtureLightVars[v.Variable]; bulb != "" {
			_, edited := vars[bulb]
			initial, declared := "", false
			if types != nil {
				initial, declared = types.Value(a.Path, bulb)
			}
			// An existing bulb edit wins; a value equal to the bulb default is
			// dropped rather than restated as a redundant edit.
			if redundant := declared && sameValue(initial, value); !edited && !redundant {
				vars[bulb] = value
				detail = fmt.Sprintf("Moved %s = %s to %s on %s", v.Variable, value, bulb, a.Path)
			}
		}
		out = append(out, collapse(cur, subject, Atom{Path: a.Path, Vars: nilIfEmpty(vars)}, kinds, fix(FixMoveFixtureLight, detail)))
	case KindBanned:
		if kinds.Has(FixRemoveBanned) && removable(cur.atoms[subject].Path) {
			out = append(out, candidate{cur.without(subject), []Fix{fix(FixRemoveBanned, "Removed banned "+cur.atoms[subject].Path)}})
		}
	}
	return out
}

// collapse replaces the atom at index with a. When a is then an exact copy of
// another atom on the tile and duplicates may be removed, the fixed atom is
// dropped instead: one validated step rather than a strip that creates a
// duplicate and a removal that would only validate afterwards.
func collapse(cur tileState, index int, a Atom, kinds FixKinds, f Fix) candidate {
	if kinds.Has(FixRemoveDuplicate) {
		for i, other := range cur.atoms {
			if i != index && SameAtom(other, a) {
				return candidate{cur.without(index), []Fix{f, {Kind: FixRemoveDuplicate, RuleFile: f.RuleFile, Rule: f.Rule, Detail: "Removed duplicate " + a.Path}}}
			}
		}
	}
	return candidate{cur.replacing(index, a), []Fix{f}}
}

// promote finds the unique subtype of a.Path whose initial values equal every
// banned edit and that otherwise differs only in variables a still edits
// explicitly (or in dir, when the edits being replaced are pixel offsets).
// Edits equal to the subtype's defaults are dropped as redundant.
func promote(a Atom, banned []string, types TypeTree) (string, map[string]string, bool) {
	if len(banned) == 0 {
		return "", nil, false
	}
	pixel := false
	for _, name := range banned {
		if _, ok := a.Vars[name]; !ok {
			return "", nil, false
		}
		pixel = pixel || strings.HasPrefix(name, "pixel_")
	}
	bannedSet := make(map[string]bool, len(banned))
	for _, name := range banned {
		bannedSet[name] = true
	}
	best, bestScore, ties := "", -1, 0
	for _, sub := range promotionCandidates(types, a.Path) {
		if isAbstract(types, sub) {
			continue
		}
		ok := true
		for _, name := range banned {
			value, found := types.Value(sub, name)
			if !found || !sameValue(value, a.Vars[name]) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		diff := differingVars(types, a.Path, sub)
		for _, name := range diff {
			_, edited := a.Vars[name]
			if !bannedSet[name] && !edited && (!pixel || name != "dir") {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		switch score := len(diff); {
		case bestScore < 0 || score < bestScore:
			best, bestScore, ties = sub, score, 1
		case score == bestScore:
			ties++
		}
	}
	if ties != 1 {
		return "", nil, false
	}
	vars := map[string]string{}
	for name, value := range a.Vars {
		if bannedSet[name] {
			continue
		}
		if initial, ok := types.Value(best, name); ok && sameValue(initial, value) {
			continue
		}
		vars[name] = value
	}
	return best, nilIfEmpty(vars), true
}

// promotionCandidates are path's subtypes and, for a tgstation directional
// helper (`<family>/directional/<dir>`), the other helpers of its family, so a
// helper edited to face another way can become the matching sibling.
func promotionCandidates(types TypeTree, path string) []string {
	out := types.Subtypes(path)
	if i := strings.LastIndex(path, "/directional/"); i > 0 {
		own := map[string]bool{path: true}
		for _, sub := range out {
			own[sub] = true
		}
		for _, other := range types.Subtypes(path[:i]) {
			if !own[other] {
				out = append(out, other)
			}
		}
	}
	return out
}

// commonAncestor is the deepest type path both a and b descend from.
func commonAncestor(a, b string) string {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return strings.Join(as[:n], "/")
}

// differingVars lists the variables whose initial value on to differs from
// from in a way an instance can observe. Only variables set on a type below
// their common ancestor can differ.
func differingVars(types TypeTree, from, to string) []string {
	lca := commonAncestor(from, to)
	seen := map[string]bool{}
	var out []string
	for _, end := range []string{to, from} {
		for p := end; p != lca && strings.HasPrefix(p, lca+"/"); p = p[:strings.LastIndex(p, "/")] {
			for _, name := range types.OwnVars(p) {
				if seen[name] || name == "abstract_type" {
					continue
				}
				seen[name] = true
				was, hadFrom := types.Value(from, name)
				now, hadTo := types.Value(to, name)
				if hadFrom == hadTo && (!hadFrom || sameValue(was, now)) {
					continue
				}
				if name == "name" && synthesizedName(now, lca, to) {
					continue
				}
				if catalogVar(types, name, lca, to) {
					continue
				}
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// appearanceVars change what an instance looks like or how it collides.
var appearanceVars = map[string]bool{
	"name": true, "desc": true, "icon": true, "icon_state": true, "color": true, "alpha": true,
	"layer": true, "plane": true, "density": true, "opacity": true, "dir": true,
	"pixel_x": true, "pixel_y": true, "pixel_w": true, "pixel_z": true,
}

// catalogVar reports whether name's value on sub comes from an abstract type
// between base and sub and does not affect appearance. Abstract helper roots
// (`.../directional`) set catalogue data such as `printable`, not instance
// state, so that difference does not stop a promotion.
func catalogVar(types TypeTree, name, base, sub string) bool {
	if appearanceVars[name] {
		return false
	}
	for p := sub; p != base && strings.HasPrefix(p, base+"/"); p = p[:strings.LastIndex(p, "/")] {
		for _, own := range types.OwnVars(p) {
			if own == name {
				// The nearest definition decides where the value comes from.
				return p != sub && isAbstract(types, p)
			}
		}
	}
	return false
}

// isAbstract follows the tgstation convention: a type whose abstract_type is
// itself is never placed on a map.
func isAbstract(types TypeTree, path string) bool {
	value, ok := types.Value(path, "abstract_type")
	return ok && parseConstant(value).s == path
}

// synthesizedName reports whether value is the name the environment loader
// invents for a type without one: the last segment of a type between base and
// sub, quoted. Such a name is not a real difference between the types.
func synthesizedName(value, base, sub string) bool {
	for p := sub; p != base && strings.HasPrefix(p, base+"/"); p = p[:strings.LastIndex(p, "/")] {
		if value == `"`+p[strings.LastIndex(p, "/")+1:]+`"` {
			return true
		}
	}
	return false
}

func sameValue(a, b string) bool { return constEqual(parseConstant(a), parseConstant(b)) }

func isStrictSubtype(path, of string) bool { return strings.HasPrefix(path, of+"/") }

// removable reports whether a fix may delete an atom of this path: every tile
// keeps its turf and area.
func removable(path string) bool {
	return !isArea(path) && path != "/turf" && !strings.HasPrefix(path, "/turf/")
}

func anyPositional(names []string) bool {
	for _, name := range names {
		if positionalVars[name] {
			return true
		}
	}
	return false
}

// textVars hold mapper-written text. Deleting one loses content.
var textVars = map[string]bool{"name": true, "desc": true}

func anyText(names []string) bool {
	for _, name := range names {
		if textVars[name] {
			return true
		}
	}
	return false
}

func isArea(path string) bool { return path == "/area" || strings.HasPrefix(path, "/area/") }

// minorWords stay lower case inside a title, as door_name_capitalization allows.
var minorWords = map[string]bool{"of": true, "and": true, "to": true}

// capitalizedVariants returns title-cased forms of a plain DM string literal:
// first keeping minor words lower case, then capitalizing every word. Strings
// with escapes or embedded expressions are not rewritten.
func capitalizedVariants(raw string) []string {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || strings.ContainsAny(raw[1:len(raw)-1], "\"[]\\") {
		return nil
	}
	words := strings.Split(raw[1:len(raw)-1], " ")
	title := make([]string, len(words))
	every := make([]string, len(words))
	for i, w := range words {
		every[i] = capitalizeWord(w)
		title[i] = every[i]
		if i != 0 && minorWords[w] {
			title[i] = w
		}
	}
	var out []string
	for _, variant := range []string{`"` + strings.Join(title, " ") + `"`, `"` + strings.Join(every, " ") + `"`} {
		if variant != raw && (len(out) == 0 || out[0] != variant) {
			out = append(out, variant)
		}
	}
	return out
}

func capitalizeWord(w string) string {
	if w == "" || w[0] < 'a' || w[0] > 'z' {
		return w
	}
	return string(w[0]-'a'+'A') + w[1:]
}

func describeEdits(a Atom, names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s = %s", name, a.Vars[name]))
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func cloneAtoms(atoms []Atom) []Atom {
	out := make([]Atom, len(atoms))
	for i, a := range atoms {
		out[i] = Atom{Path: a.Path, Vars: maps.Clone(a.Vars)}
	}
	return out
}
