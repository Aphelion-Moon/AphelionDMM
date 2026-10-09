package maplint

import (
	"math"
	"strconv"
	"strings"
)

// Atom is one object on a tile, independent of the editor's map types.
// Vars holds only the explicit map variable edits, string-encoded as DM source
// text exactly as stored in a DMM file (strings keep their quotes).
type Atom struct {
	Path string
	Vars map[string]string
}

// Kind identifies which rule feature produced a Violation.
type Kind int

const (
	KindBanned Kind = iota
	KindBannedNeighbor
	KindRequiredNeighbor
	KindBannedVariables // banned_variables: true
	KindBannedVariable
	KindInertDir      // audit: dir on a single-direction sprite
	KindRedundantEdit // audit: an edit equal to the type default
)

func (k Kind) String() string {
	switch k {
	case KindBanned:
		return "banned"
	case KindBannedNeighbor:
		return "banned_neighbor"
	case KindRequiredNeighbor:
		return "required_neighbor"
	case KindBannedVariables:
		return "banned_variables"
	case KindBannedVariable:
		return "banned_variable"
	case KindInertDir:
		return "inert_dir"
	case KindRedundantEdit:
		return "redundant_edit"
	}
	return "unknown"
}

// Violation is one rule failure on a tile. Message matches maplint's own text.
type Violation struct {
	RuleFile string // base name of the rule file, e.g. "multiple_windows.yml"
	Rule     string // the rule's typepath key as written in the file
	Help     string // the file's help text, if any
	Kind     Kind
	Message  string

	Subject       string // path of the atom the rule is about
	AtomIndex     int    // index of Subject in the tile slice
	Neighbor      string // offending neighbor path (banned_neighbors) or the missing requirement
	NeighborIndex int    // index of the offending neighbor, -1 when none
	Variable      string // offending variable (banned_variable)

	// Identical is true when the banned neighbor rule used `identical: true`,
	// i.e. the neighbor is an exact duplicate of the subject.
	Identical bool

	// Paths lists the offending paths: the subject, then the neighbor if any.
	Paths []string

	order [4]int // file, rule, atom, sequence
}

// pAtom is an Atom prepared for evaluation.
type pAtom struct {
	Atom
	segs   []string
	consts map[string]constant
}

func (a *pAtom) constOf(name string) (constant, bool) {
	raw, ok := a.Vars[name]
	if !ok {
		return constant{}, false
	}
	if c, ok := a.consts[name]; ok {
		return c, true
	}
	if a.consts == nil {
		a.consts = make(map[string]constant, len(a.Vars))
	}
	c := parseConstant(raw)
	a.consts[name] = c
	return c, true
}

func identicalAtoms(a, b *pAtom) bool {
	if a.Path != b.Path || len(a.Vars) != len(b.Vars) {
		return false
	}
	for name := range a.Vars {
		if _, ok := b.Vars[name]; !ok {
			return false
		}
		ca, _ := a.constOf(name)
		cb, _ := b.constOf(name)
		if !constEqual(ca, cb) {
			return false
		}
	}
	return true
}

func (n *neighbor) matches(self, other *pAtom) bool {
	if n.identical {
		return identicalAtoms(self, other)
	}
	if n.tp != nil && n.tp.matches(other.Path, other.segs) {
		return true
	}
	if n.pattern != nil && n.pattern.MatchString(other.Path) {
		return true
	}
	return false
}

// whenMet mirrors WhenCondition.is_met / WhenGroup.is_met.
func (w *whenNode) met(a *pAtom) bool {
	if !w.leaf {
		if w.all {
			for _, c := range w.children {
				if !c.met(a) {
					return false
				}
			}
			return true
		}
		for _, c := range w.children {
			if c.met(a) {
				return true
			}
		}
		return false
	}
	c, present := a.constOf(w.varName)
	switch w.kind {
	case condSet:
		return present
	case condNotSet:
		return !present
	case condEqual:
		return present && whenValue(c) == strings.TrimSpace(w.value)
	case condNotEqual:
		return !present || whenValue(c) != strings.TrimSpace(w.value)
	case condLike:
		return present && w.like.MatchString(c.pyStr())
	}
	return false
}

// whenValue mirrors the "is" comparison text: integral floats compare as ints.
func whenValue(c constant) string {
	if c.kind == constNum && !math.IsInf(c.num, 0) && !math.IsNaN(c.num) && c.num == math.Trunc(c.num) {
		return strconv.FormatFloat(c.num, 'f', 0, 64)
	}
	return strings.TrimSpace(c.pyStr())
}
func (c *choices) check(v constant, allowMode bool) (string, bool) {
	// returns the failure reason and whether the value failed
	if c.isList {
		in := false
		for _, item := range c.list {
			if constEqual(item, v) {
				in = true
				break
			}
		}
		strs := make([]string, len(c.list))
		for i, item := range c.list {
			strs[i] = item.pyStr()
		}
		if allowMode && !in {
			return "Must be one of " + strings.Join(strs, ", "), true
		}
		if !allowMode && in {
			return "Must not be one of " + strings.Join(strs, ", "), true
		}
		return "", false
	}
	m := c.pattern.MatchString(v.pyStr())
	if allowMode && !m {
		return "Must match " + c.src, true
	}
	if !allowMode && m {
		return "Must not match " + c.src, true
	}
	return "", false
}

// run mirrors BannedVariable.run for a variable known to be set.
func (b *bannedVariable) run(v constant) (string, bool) {
	if b.allow != nil {
		return b.allow.check(v, true)
	}
	if b.deny != nil {
		return b.deny.check(v, false)
	}
	return "This variable is not allowed for this type.", true
}

func (r *rule) skipped(file string) bool {
	if file == "" || len(r.skipFiles) == 0 {
		return false
	}
	norm := strings.ReplaceAll(file, "\\", "/")
	for _, sf := range r.skipFiles {
		if sf.re != nil {
			if sf.re.MatchString(norm) {
				return true
			}
		} else if strings.Contains(norm, sf.substr) {
			return true
		}
	}
	return false
}

// eval mirrors Rules.run for the atom at idx. out receives violations.
func (r *rule) eval(lf *lintFile, fileIdx, ruleIdx int, file string, tile []pAtom, idx int, out *[]Violation) {
	if r.skipped(file) {
		return
	}
	a := &tile[idx]
	if r.when != nil && !r.when.met(a) {
		return
	}
	whenText := ""
	if r.when != nil {
		whenText = " when " + r.when.matchString(true)
	}
	seq := 0
	emit := func(k Kind, msg string, nbIdx int, nbPath, variable string) {
		v := Violation{
			RuleFile: lf.name, Rule: r.key, Help: lf.help, Kind: k, Message: msg,
			Subject: a.Path, AtomIndex: idx, Neighbor: nbPath, NeighborIndex: nbIdx, Variable: variable,
			Paths: []string{a.Path},
			order: [4]int{fileIdx, ruleIdx, idx, seq},
		}
		if nbPath != "" && nbIdx >= 0 {
			v.Paths = append(v.Paths, nbPath)
		}
		seq++
		*out = append(*out, v)
	}

	if r.banned {
		emit(KindBanned, "Typepath "+a.Path+" is banned"+whenText+".", -1, "", "")
	}

	if len(r.bannedNeighbors) > 0 {
		ignored := false
		if len(r.ignored) > 0 {
			for j := range tile {
				if j == idx {
					continue
				}
				for k := range r.ignored {
					if r.ignored[k].matches(tile[j].Path, tile[j].segs) {
						ignored = true
						break
					}
				}
				if ignored {
					break
				}
			}
		}
		if !ignored {
			for i := range r.bannedNeighbors {
				nb := &r.bannedNeighbors[i]
				for j := range tile {
					if j == idx || !nb.matches(a, &tile[j]) {
						continue
					}
					emit(KindBannedNeighbor, "Typepath "+a.Path+" has a banned path on the same tile"+whenText+": "+tile[j].Path, j, tile[j].Path, "")
					(*out)[len(*out)-1].Identical = nb.identical
				}
			}
		}
	}

	for i := range r.required {
		nb := &r.required[i]
		found := false
		for j := range tile {
			if j != idx && nb.matches(a, &tile[j]) {
				found = true
				break
			}
		}
		if !found {
			emit(KindRequiredNeighbor, "Typepath "+a.Path+" is missing a required neighbor"+whenText+": "+nb.str(), -1, nb.str(), "")
		}
	}

	if r.bannedVarsAll {
		if len(a.Vars) > 0 {
			emit(KindBannedVariables, "Typepath "+a.Path+" should not have any variable edits"+whenText+".", -1, "", "")
		}
		return
	}
	for i := range r.bannedVars {
		bv := &r.bannedVars[i]
		c, ok := a.constOf(bv.name)
		if !ok {
			continue
		}
		reason, failed := bv.run(c)
		if !failed {
			continue
		}
		emit(KindBannedVariable, "Typepath "+a.Path+" has a banned variable (set to "+c.pyStr()+")"+whenText+": "+bv.name+". "+reason, -1, "", bv.name)
	}
}

func prepare(atoms []Atom, extra *Atom) []pAtom {
	n := len(atoms)
	if extra != nil {
		n++
	}
	out := make([]pAtom, 0, n)
	for _, a := range atoms {
		out = append(out, pAtom{Atom: a, segs: splitPath(a.Path)})
	}
	if extra != nil {
		out = append(out, pAtom{Atom: *extra, segs: splitPath(extra.Path)})
	}
	return out
}

func (rs *RuleSet) checkPrepared(file string, tile []pAtom) []Violation {
	var out []Violation
	var cand []ruleRef
	for i := range tile {
		cand = rs.idx.candidates(tile[i].segs, tile[i].Path, cand[:0])
		for _, ref := range cand {
			ref.rule.eval(ref.file, ref.fileIdx, ref.ruleIdx, file, tile, i, &out)
		}
	}
	if rs.audit != nil {
		rs.audit.check(tile, &out)
	}
	sortViolations(out)
	return out
}

// CheckTile evaluates every rule against one tile's atoms (all of them, in
// order). Rules with skip_files are evaluated as if the file name were unknown.
func (rs *RuleSet) CheckTile(atoms []Atom) []Violation {
	return rs.CheckTileInFile("", atoms)
}

// CheckTileInFile is CheckTile with the map file name, which rules'
// skip_files entries are matched against (substring or regex, after
// normalising backslashes to slashes).
func (rs *RuleSet) CheckTileInFile(file string, atoms []Atom) []Violation {
	if rs == nil || len(atoms) == 0 {
		return nil
	}
	return rs.checkPrepared(file, prepare(atoms, nil))
}

// CheckPlacement returns the violations introduced by adding placed to a tile
// that already holds existing: violations of the resulting tile that were not
// already present, whether the placed atom or an existing atom is the subject.
// placed gets AtomIndex len(existing). existing is not modified.
func (rs *RuleSet) CheckPlacement(existing []Atom, placed Atom) []Violation {
	return rs.CheckPlacementInFile("", existing, placed)
}

// CheckPlacementInFile is CheckPlacement with a map file name for skip_files.
func (rs *RuleSet) CheckPlacementInFile(file string, existing []Atom, placed Atom) []Violation {
	if rs == nil {
		return nil
	}
	after := rs.checkPrepared(file, prepare(existing, &placed))
	if len(after) == 0 {
		return nil
	}
	before := map[string]int{}
	for _, v := range rs.checkPrepared(file, prepare(existing, nil)) {
		before[violationKey(v)]++
	}
	var out []Violation
	for _, v := range after {
		k := violationKey(v)
		if before[k] > 0 {
			before[k]--
			continue
		}
		out = append(out, v)
	}
	return out
}

func violationKey(v Violation) string {
	return v.RuleFile + "\x00" + v.Rule + "\x00" + strconv.Itoa(int(v.Kind)) + "\x00" + strconv.Itoa(v.AtomIndex) + "\x00" + strconv.Itoa(v.NeighborIndex) + "\x00" + v.Message
}
