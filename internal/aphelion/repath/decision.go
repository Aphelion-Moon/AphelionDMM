package repath

import (
	"fmt"
	"maps"
	"slices"

	"sdmm/internal/aphelion/repath/updatepaths"
)

type Kind uint8

const (
	Keep   Kind = iota // leave the unknown path untouched
	Apply              // rewrite through the decision's resolver
	Delete             // remove instances; only ever chosen by a person
)

// Via selects how an Apply decision computes outputs for each instance.
type Via uint8

const (
	ViaRule       Via = iota // Rule, a single explicit rule
	ViaScripts               // the codebase script set, stopping at known paths
	ViaScriptsAll            // the codebase script set, every rule
	ViaRemembered            // remembered rules, chained to a known path
)

type Origin uint8

const (
	OriginHuman Origin = iota
	OriginAuto
)

type Decision struct {
	Kind   Kind
	Via    Via
	Rule   updatepaths.Rule
	Origin Origin
	// AllowCrossBase permits outputs whose root type differs from the input.
	AllowCrossBase bool
	// Summary describes the choice for history labels and exported comments.
	Summary string
}

// Plan holds one decision per unknown path, with optional overrides for
// individual variable variants (keyed by VariantKey).
type Plan struct {
	Paths    map[string]Decision
	Variants map[string]map[string]Decision
}

func NewPlan() Plan {
	return Plan{Paths: map[string]Decision{}, Variants: map[string]map[string]Decision{}}
}

func (p Plan) SetVariant(path, key string, decision Decision) {
	if p.Variants[path] == nil {
		p.Variants[path] = map[string]Decision{}
	}
	p.Variants[path][key] = decision
}

func (p Plan) decision(path, key string) (Decision, bool) {
	if decision, ok := p.Variants[path][key]; ok {
		return decision, true
	}
	decision, ok := p.Paths[path]
	return decision, ok
}

// Active reports whether any decision changes the map.
func (p Plan) Active() bool {
	for _, decision := range p.Paths {
		if decision.Kind != Keep {
			return true
		}
	}
	for _, variants := range p.Variants {
		for _, decision := range variants {
			if decision.Kind != Keep {
				return true
			}
		}
	}
	return false
}

// RepathRule builds the rule for a person's choice of a single target. People
// keep map edits by default, unlike bare script rules.
func RepathRule(from, to string, vars map[string]string) updatepaths.Rule {
	output := updatepaths.Output{Path: to, Props: []updatepaths.Prop{{Kind: updatepaths.PropAllOld}}}
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		output.Props = append(output.Props, updatepaths.Prop{Name: name, Value: vars[name]})
	}
	return updatepaths.Rule{Match: updatepaths.Pattern{Path: from}, Outputs: []updatepaths.Output{output}}
}

func DeleteRule(from string) updatepaths.Rule {
	return updatepaths.Rule{Match: updatepaths.Pattern{Path: from}, Delete: true}
}

// Resolvers are the rule sources a plan may refer to.
type Resolvers struct {
	Scripts    *updatepaths.Set
	Remembered []updatepaths.Rule
}

// Compile validates a plan against the target environment. unknown lists the
// paths that were unknown when the plan was made; only they are rewritten.
func Compile(plan Plan, target *Index, unknown []string, resolvers Resolvers) (*Transformer, error) {
	t := &Transformer{
		plan:      plan,
		known:     target.Exists,
		unknown:   make(map[string]struct{}, len(unknown)),
		resolvers: resolvers,
	}
	for _, path := range unknown {
		if target.Exists(path) {
			return nil, fmt.Errorf("%s is defined by the environment", path)
		}
		t.unknown[path] = struct{}{}
	}
	check := func(path string, decision Decision) error {
		if _, ok := t.unknown[path]; !ok {
			return fmt.Errorf("%s is not an unknown path of this map", path)
		}
		switch decision.Kind {
		case Keep:
			return nil
		case Delete:
			if decision.Origin != OriginHuman {
				return fmt.Errorf("%s: deletion must be confirmed by a person", path)
			}
			return nil
		}
		switch decision.Via {
		case ViaScripts, ViaScriptsAll:
			if resolvers.Scripts.Len() == 0 {
				return fmt.Errorf("%s: no scripts are loaded", path)
			}
			return nil
		case ViaRemembered:
			if len(resolvers.Remembered) == 0 {
				return fmt.Errorf("%s: no remembered decisions are loaded", path)
			}
			return nil
		}
		rule := decision.Rule
		if rule.Delete || len(rule.Outputs) == 0 {
			return fmt.Errorf("%s: the rule has no outputs", path)
		}
		if rule.Match.Path != path || rule.Match.Subtypes {
			return fmt.Errorf("%s: the rule matches %s", path, rule.Match.Path)
		}
		for _, output := range rule.Outputs {
			to := output.Path
			if output.Kind == updatepaths.OutputKeepPath {
				to = path
			}
			if output.Kind == updatepaths.OutputSubtypes || !target.Exists(to) {
				return fmt.Errorf("%s: target %s is not defined by the environment", path, to)
			}
			if !decision.AllowCrossBase && !SameBase(path, to) {
				return fmt.Errorf("%s: %s changes the root type; allow cross-type changes to use it", path, to)
			}
		}
		return nil
	}
	for _, path := range slices.Sorted(maps.Keys(plan.Paths)) {
		if err := check(path, plan.Paths[path]); err != nil {
			return nil, err
		}
	}
	for _, path := range slices.Sorted(maps.Keys(plan.Variants)) {
		for _, key := range slices.Sorted(maps.Keys(plan.Variants[path])) {
			if err := check(path, plan.Variants[path][key]); err != nil {
				return nil, err
			}
		}
	}
	return t, nil
}
