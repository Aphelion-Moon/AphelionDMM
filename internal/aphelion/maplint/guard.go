package maplint

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Status describes the rule load for the current environment.
type Status struct {
	Dir         string   // lint directory, empty when the repository ships none
	Loading     bool     // a background load has not been published yet
	Files       []string // rule files that loaded
	RuleCount   int
	Unsupported []string // rules dropped because their semantics are not implemented
	Errors      []string // files that could not be loaded
	Generation  uint64
}

// Active reports whether the environment ships a lint directory.
func (s Status) Active() bool { return s.Dir != "" }

// Guard owns the precomputed rule index for the loaded environment. Loading
// happens off the caller's thread; the result is published through a caller
// supplied scheduler (the UI thread) and fenced by a generation so a slow load
// for a previous environment can never replace a newer one.
type Guard struct {
	mu     sync.RWMutex
	gen    uint64
	rules  *RuleSet
	status Status
}

func NewGuard() *Guard { return &Guard{} }

var active = NewGuard()

// Active returns the process-wide guard the editor consults.
func Active() *Guard { return active }

// Reset discards the current rules and invalidates any load in flight.
func (g *Guard) Reset() {
	g.mu.Lock()
	g.gen++
	g.rules = nil
	g.status = Status{Generation: g.gen}
	g.mu.Unlock()
}

// Begin starts loading the rules shipped beside environmentPath. A repository
// without a lint directory leaves the guard silently inactive. schedule runs
// the publication step; it must not run it synchronously on the loading
// goroutine unless the caller tolerates that.
func (g *Guard) Begin(environmentPath string, schedule func(func())) {
	g.mu.Lock()
	g.gen++
	gen := g.gen
	g.rules = nil
	g.status = Status{Generation: gen}
	dir, ok := FindLintDir(environmentPath)
	if !ok {
		g.mu.Unlock()
		return
	}
	g.status.Dir, g.status.Loading = dir, true
	g.mu.Unlock()

	go func() {
		rs, errs := Load(dir)
		next := Status{Dir: dir, Generation: gen, Files: rs.Files(), RuleCount: rs.RuleCount()}
		for _, err := range errs {
			if u, ok := err.(*UnsupportedError); ok {
				next.Unsupported = append(next.Unsupported, u.Error())
			} else {
				next.Errors = append(next.Errors, err.Error())
			}
		}
		schedule(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.gen != gen {
				return
			}
			g.rules, g.status = rs, next
		})
	}()
}

// Status returns a copy of the load status.
func (g *Guard) Status() Status {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := g.status
	s.Files = append([]string(nil), s.Files...)
	s.Unsupported = append([]string(nil), s.Unsupported...)
	s.Errors = append([]string(nil), s.Errors...)
	return s
}

// RuleSet returns the published rules, or nil while loading or inactive.
func (g *Guard) RuleSet() *RuleSet {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.rules
}

// Ready reports whether published rules can be evaluated.
func (g *Guard) Ready() bool { return g.RuleSet() != nil && g.RuleSet().RuleCount() > 0 }

// Verdict is the outcome of checking one placement.
type Verdict struct {
	Violations []Violation
	// Skip is true when the placed atom is an exact duplicate of an existing
	// atom under an `identical: true` banned_neighbors rule.
	Skip bool
	// Replace lists indices into the existing slice of atoms involved in a
	// non-identical conflict with the placed atom, ascending and unique.
	Replace []int
}

// Evaluate checks placing placed on a tile that holds existing.
func (g *Guard) Evaluate(file string, existing []Atom, placed Atom) Verdict {
	return EvaluateRules(g.RuleSet(), file, existing, placed)
}

// EvaluateRules is Evaluate against an explicit rule set, so workers can pin
// the rules they started with.
func EvaluateRules(rs *RuleSet, file string, existing []Atom, placed Atom) Verdict {
	violations := rs.CheckPlacementInFile(file, existing, placed)
	v := Verdict{Violations: violations}
	if len(violations) == 0 {
		return v
	}
	seen := map[int]bool{}
	for _, violation := range violations {
		if violation.Identical {
			v.Skip = true
			continue
		}
		for _, index := range [...]int{violation.AtomIndex, violation.NeighborIndex} {
			if index >= 0 && index < len(existing) && !seen[index] {
				seen[index] = true
				v.Replace = append(v.Replace, index)
			}
		}
	}
	sort.Ints(v.Replace)
	return v
}

// Summary names the broken rules and their help text for display.
func (v Verdict) Summary() string {
	const shown = 2
	var parts []string
	seen := map[string]bool{}
	for _, violation := range v.Violations {
		text := violation.Help
		if text == "" {
			text = violation.Message
		}
		part := violation.RuleFile + ": " + strings.Join(strings.Fields(text), " ")
		if !seen[part] {
			seen[part] = true
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts[:min(len(parts), shown)], "; ")
	if len(parts) > shown {
		out += " (+" + strconv.Itoa(len(parts)-shown) + " more)"
	}
	return out
}

// MapFile is the name rules' skip_files are matched against: the map path
// relative to the repository root with forward slashes, or the slashed
// absolute path when the map lives outside the repository.
func MapFile(absolute, root string) string {
	if root != "" {
		if rel, err := filepath.Rel(root, absolute); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(absolute)
}
