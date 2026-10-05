package repath

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"sdmm/internal/aphelion/repath/updatepaths"
)

// Tier orders confidence. Only Certain and, by setting, High candidates are
// ever selected without a person.
type Tier uint8

const (
	Lossy Tier = iota
	Low
	Medium
	High
	Certain
)

func (t Tier) String() string {
	return [...]string{"Lossy", "Low", "Medium", "High", "Certain"}[t]
}

type Candidate struct {
	Decision  Decision
	Target    string // first output path of the most common variant
	Tier      Tier
	Score     float64
	Generator string
	Reasons   []string
	// MissingVars are map-edited variables the target type does not declare.
	MissingVars []string
	// Conflicts are map-edited values the candidate would overwrite.
	Conflicts []string
	CrossBase bool
	// Resolved counts the instances the candidate resolves, out of Entry.Count.
	Resolved int
}

func (c Candidate) key() string {
	switch {
	case c.Decision.Kind == Delete:
		return "delete"
	case c.Decision.Via != ViaRule:
		return fmt.Sprintf("via:%d", c.Decision.Via)
	}
	return c.Decision.Rule.String()
}

type Proposal struct {
	Entry      Entry
	Candidates []Candidate // best first
	Auto       int         // index of the automatically selected candidate, or -1
}

type AutoLevel uint8

const (
	AutoOff AutoLevel = iota
	AutoCertain
	AutoHigh
)

// Sources are everything Suggest may consult. Reference and the rule sources
// are optional.
type Sources struct {
	Target     *Index
	Reference  *Index
	Scripts    *updatepaths.Set
	Remembered []updatepaths.Rule
}

func (s Sources) resolvers() Resolvers {
	return Resolvers{Scripts: s.Scripts, Remembered: s.Remembered}
}

const (
	autoHighScore  = 0.85
	autoHighMargin = 0.10
	maxCandidates  = 8
)

// Suggest ranks candidates for every inventory entry and applies the auto
// policy. Results are deterministic for equal inputs.
func Suggest(ctx context.Context, inventory Inventory, sources Sources, level AutoLevel) ([]Proposal, error) {
	proposals := make([]Proposal, 0, len(inventory.Entries))
	for _, entry := range inventory.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw []Candidate
		for _, generate := range generators {
			raw = append(raw, generate(entry, sources)...)
		}
		candidates := evaluate(entry, raw, sources)
		proposals = append(proposals, Proposal{Entry: entry, Candidates: candidates, Auto: autoSelect(candidates, entry, level)})
	}
	return proposals, nil
}

// evaluate measures each candidate against every recorded variant, merges
// duplicates and orders the result.
func evaluate(entry Entry, raw []Candidate, sources Sources) []Candidate {
	merged := map[string]*Candidate{}
	var order []string
	for _, candidate := range raw {
		measure(&candidate, entry, sources)
		if candidate.Decision.Kind == Apply && candidate.Resolved == 0 {
			continue
		}
		key := candidate.key()
		existing, ok := merged[key]
		if !ok {
			copy := candidate
			merged[key] = &copy
			order = append(order, key)
			continue
		}
		better := candidate.Tier > existing.Tier || candidate.Tier == existing.Tier && candidate.Score > existing.Score
		reasons := append(slices.Clone(existing.Reasons), candidate.Reasons...)
		generator := existing.Generator + "+" + candidate.Generator
		if better {
			*existing = candidate
		}
		existing.Reasons, existing.Generator = slices.Compact(reasons), generator
		// Agreement between independent generators raises confidence.
		existing.Score = min(1, existing.Score+0.03)
	}
	candidates := make([]Candidate, 0, len(order))
	for _, key := range order {
		candidates = append(candidates, *merged[key])
	}
	slices.SortStableFunc(candidates, func(a, b Candidate) int {
		return cmp.Or(cmp.Compare(b.Tier, a.Tier), cmp.Compare(b.Score, a.Score), strings.Compare(a.Target, b.Target), strings.Compare(a.Generator, b.Generator))
	})
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	return candidates
}

func measure(c *Candidate, entry Entry, sources Sources) {
	if c.Decision.Kind == Delete {
		return
	}
	known := sources.Target.Exists
	resolved, total := 0, entry.Count-entry.Overflow
	missing, conflicts := map[string]bool{}, map[string]bool{}
	for n, variant := range entry.Variants {
		in := updatepaths.Instance{Path: entry.Path, Vars: variant.Vars}
		out, _, err := resolve(c.Decision, in, sources.resolvers(), known)
		if err != nil {
			c.Reasons = append(c.Reasons, err.Error())
			continue
		}
		for _, output := range out {
			if !SameBase(entry.Path, output.Path) {
				c.CrossBase = true
			}
		}
		decision := c.Decision
		decision.AllowCrossBase = true // reported through CrossBase instead
		if !accepts(decision, in, out, known) {
			continue
		}
		resolved += variant.Count
		if n == 0 && c.Target == "" {
			c.Target = out[0].Path
		}
		for _, output := range out {
			for name, value := range output.Vars {
				if !sources.Target.Declares(output.Path, name) {
					missing[name] = true
				}
				if old, ok := variant.Vars[name]; ok && old != value {
					conflicts[fmt.Sprintf("%s: %s -> %s", name, old, value)] = true
				}
			}
		}
	}
	c.Resolved = resolved
	c.MissingVars = slices.Sorted(maps.Keys(missing))
	c.Conflicts = slices.Sorted(maps.Keys(conflicts))
	if len(c.MissingVars) != 0 {
		c.Score *= 1 - 0.5*float64(len(c.MissingVars))/float64(len(c.MissingVars)+4)
	}
	if total > 0 && resolved < total {
		c.Reasons = append(c.Reasons, fmt.Sprintf("resolves %d of %d instances", resolved, total))
		c.Tier = min(c.Tier, Medium)
	}
	if c.CrossBase {
		c.Reasons = append(c.Reasons, "changes the root type")
	}
}

func autoSelect(candidates []Candidate, entry Entry, level AutoLevel) int {
	if level == AutoOff || len(candidates) == 0 {
		return -1
	}
	best := candidates[0]
	if best.Decision.Kind != Apply || best.CrossBase || len(best.Conflicts) != 0 || entry.Overflow != 0 || best.Resolved != entry.Count {
		return -1
	}
	switch {
	case best.Tier == Certain:
		return 0
	case best.Tier == High && level == AutoHigh:
		if best.Score < autoHighScore || len(best.MissingVars) != 0 {
			return -1
		}
		if len(candidates) > 1 && candidates[1].Tier >= Medium && best.Score-candidates[1].Score < autoHighMargin {
			return -1
		}
		return 0
	}
	return -1
}

// DefaultPlan holds every automatic selection.
func DefaultPlan(proposals []Proposal) Plan {
	plan := NewPlan()
	for _, proposal := range proposals {
		if proposal.Auto >= 0 {
			decision := proposal.Candidates[proposal.Auto].Decision
			decision.Origin = OriginAuto
			plan.Paths[proposal.Entry.Path] = decision
		}
	}
	return plan
}
