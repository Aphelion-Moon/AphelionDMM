package updatepaths

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Matches reports whether the rule's old path and filters accept the instance.
func (r Rule) Matches(in Instance) bool {
	if _, ok := r.Match.suffix(in.Path); !ok {
		return false
	}
	for _, filter := range r.Match.Filters {
		value, set := in.Vars[filter.Name]
		switch filter.Kind {
		case FilterNoEdits:
			if len(in.Vars) != 0 {
				return false
			}
		case FilterUnset:
			if set {
				return false
			}
		case FilterAny:
			if !set {
				return false
			}
		default:
			if !set || value != filter.Value {
				return false
			}
		}
	}
	return true
}

// suffix returns the matched subtype suffix ("" for the exact path).
func (p Pattern) suffix(path string) (string, bool) {
	if path == p.Path {
		return "", true
	}
	if !p.Subtypes || !strings.HasPrefix(path, p.Path+"/") {
		return "", false
	}
	rest := path[len(p.Path):]
	return rest, typePath.MatchString(rest)
}

// Apply rewrites one instance. A matched @DELETE rule returns no instances.
func (r Rule) Apply(in Instance) ([]Instance, bool) {
	if !r.Matches(in) {
		return nil, false
	}
	if r.Delete {
		return nil, true
	}
	suffix, _ := r.Match.suffix(in.Path)
	out := make([]Instance, 0, len(r.Outputs))
	for _, output := range r.Outputs {
		path := output.Path
		switch output.Kind {
		case OutputKeepPath:
			path = in.Path
		case OutputSubtypes:
			path += suffix
		}
		vars := map[string]string{}
		for _, prop := range output.Props {
			switch prop.Kind {
			case PropAllOld:
				vars = maps.Clone(in.Vars)
				if vars == nil {
					vars = map[string]string{}
				}
			case PropSkip:
				delete(vars, prop.Name)
			case PropOld, PropOldOf:
				from := prop.Name
				if prop.Kind == PropOldOf {
					from = prop.From
				}
				// The reference tool raises on a missing source; an absent
				// map edit has nothing to copy.
				if value, ok := in.Vars[from]; ok {
					vars[prop.Name] = value
				}
			default:
				vars[prop.Name] = prop.Value
			}
		}
		out = append(out, Instance{Path: path, Vars: vars})
	}
	return out, true
}

// Set is an ordered collection of scripts: ascending pull request number,
// unnumbered scripts first, then by name.
type Set struct {
	Scripts []Script
}

func NewSet(scripts ...Script) *Set {
	sorted := slices.Clone(scripts)
	slices.SortStableFunc(sorted, func(a, b Script) int {
		if a.Order != b.Order {
			if a.Order < b.Order {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return &Set{Scripts: sorted}
}

func (s *Set) Len() (rules int) {
	if s == nil {
		return 0
	}
	for _, script := range s.Scripts {
		rules += len(script.Rules)
	}
	return rules
}

// Apply runs every rule in order over the instance and its outputs. When known
// is non-nil, an instance whose path is known is no longer rewritten. The
// trace lists each rule that changed something.
func (s *Set) Apply(in Instance, known func(string) bool) ([]Instance, []Pos) {
	current := []Instance{in}
	var trace []Pos
	if s == nil {
		return current, nil
	}
	for _, script := range s.Scripts {
		for _, rule := range script.Rules {
			var next []Instance
			matched := false
			for _, instance := range current {
				if known != nil && known(instance.Path) {
					next = append(next, instance)
					continue
				}
				out, ok := rule.Apply(instance)
				if !ok {
					next = append(next, instance)
					continue
				}
				matched = true
				next = append(next, out...)
			}
			if matched {
				trace = append(trace, rule.Pos)
			}
			current = next
			if len(current) == 0 {
				return nil, trace
			}
		}
	}
	return current, trace
}

const maxChain = 64

// ResolveChain applies unordered rules (remembered decisions): the first rule
// matching an unknown path applies, repeatedly, until every output is known or
// no rule matches. An unmatched input returns nil without error.
func ResolveChain(rules []Rule, in Instance, known func(string) bool) ([]Instance, []Pos, error) {
	var trace []Pos
	var resolve func(Instance, []string) ([]Instance, error)
	resolve = func(instance Instance, seen []string) ([]Instance, error) {
		if known(instance.Path) {
			return []Instance{instance}, nil
		}
		if slices.Contains(seen, instance.Path) {
			return nil, fmt.Errorf("rule cycle: %s -> %s", strings.Join(seen, " -> "), instance.Path)
		}
		if len(seen) >= maxChain {
			return nil, fmt.Errorf("rule chain longer than %d steps from %s", maxChain, seen[0])
		}
		for _, rule := range rules {
			out, ok := rule.Apply(instance)
			if !ok {
				continue
			}
			trace = append(trace, rule.Pos)
			seen = append(seen, instance.Path)
			var result []Instance
			for _, next := range out {
				resolved, err := resolve(next, slices.Clone(seen))
				if err != nil {
					return nil, err
				}
				result = append(result, resolved...)
			}
			return result, nil
		}
		return []Instance{instance}, nil
	}
	for _, rule := range rules {
		if rule.Matches(in) {
			out, err := resolve(in, nil)
			if err != nil {
				return nil, nil, err
			}
			return out, trace, nil
		}
	}
	return nil, nil, nil
}
