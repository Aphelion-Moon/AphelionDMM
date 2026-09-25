package dm

import (
	// APHELION EDIT ADDITION START - SELECTION STAMPS
	"sort"
	// APHELION EDIT ADDITION END
	"strings"

	"github.com/rs/zerolog/log"
)

// APHELION EDIT ADDITION START - SELECTION STAMPS
// HiddenPaths returns independent data suitable for a saved selection's filter.
func (p *PathsFilter) HiddenPaths() []string {
	paths := make([]string, 0, len(p.filteredPaths))
	for path := range p.filteredPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// APHELION EDIT ADDITION END

type PathsFilter struct {
	findDirectChildren func(string) []string
	filteredPaths      map[string]bool
	// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
	hiddenDescendantCount map[string]int
	policyRevision        uint64
	// APHELION EDIT ADDITION END
}

func NewPathsFilter(findDirectChildren func(string) []string) *PathsFilter {
	return &PathsFilter{
		findDirectChildren: findDirectChildren,
		filteredPaths:      make(map[string]bool),
		// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
		hiddenDescendantCount: make(map[string]int),
		// APHELION EDIT ADDITION END
	}
}

func NewPathsFilterEmpty() *PathsFilter {
	return NewPathsFilter(func(string) []string {
		return nil
	})
}

func (p *PathsFilter) Clear() {
	// APHELION EDIT CHANGE - VISIBILITY POLICY CACHE - ORIGINAL: p.filteredPaths = make(map[string]bool)
	_ = p.ApplyHiddenPaths(nil)
}

func (p *PathsFilter) Copy() PathsFilter {
	filteredPaths := make(map[string]bool, len(p.filteredPaths))
	for path := range p.filteredPaths {
		filteredPaths[path] = true
	}
	// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
	hiddenDescendantCount := make(map[string]int, len(p.hiddenDescendantCount))
	for path, count := range p.hiddenDescendantCount {
		hiddenDescendantCount[path] = count
	}
	// APHELION EDIT ADDITION END
	return PathsFilter{
		p.findDirectChildren,
		filteredPaths,
		// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
		hiddenDescendantCount,
		p.policyRevision,
		// APHELION EDIT ADDITION END
	}
}

// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
// PolicyRevision changes once when a filter action publishes a different policy.
func (p *PathsFilter) PolicyRevision() uint64 {
	return p.policyRevision
}

// ApplyHiddenPaths replaces the effective hidden-path set as one policy update.
// The input is copied, so callers may retain or reuse their slice.
func (p *PathsFilter) ApplyHiddenPaths(paths []string) bool {
	filteredPaths := make(map[string]bool, len(paths))
	hiddenDescendantCount := make(map[string]int)
	for _, path := range paths {
		if filteredPaths[path] {
			continue
		}
		filteredPaths[path] = true
		for ancestor := parentTypePath(path); ancestor != ""; ancestor = parentTypePath(ancestor) {
			hiddenDescendantCount[ancestor]++
		}
	}

	if len(filteredPaths) == len(p.filteredPaths) {
		unchanged := true
		for path := range filteredPaths {
			if !p.filteredPaths[path] {
				unchanged = false
				break
			}
		}
		if unchanged {
			return false
		}
	}

	p.filteredPaths = filteredPaths
	p.hiddenDescendantCount = hiddenDescendantCount
	p.policyRevision++
	return true
}

func parentTypePath(path string) string {
	separator := strings.LastIndexByte(path, '/')
	if separator <= 0 {
		return ""
	}
	return path[:separator]
}

// APHELION EDIT ADDITION END

func (p *PathsFilter) IsHiddenPath(path string) bool {
	return p.filteredPaths[path]
}

func (p *PathsFilter) IsVisiblePath(path string) bool {
	return !p.IsHiddenPath(path)
}

/* APHELION EDIT REMOVAL START - VISIBILITY POLICY CACHE
func (p *PathsFilter) HasHiddenChildPath(path string) bool {
	for filteredPath := range p.filteredPaths {
		if strings.HasPrefix(filteredPath, path) {
			return true
		}
	}
	return false
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
// HasHiddenChildPath is a cached strict-descendant summary. Direct visibility
// remains the map lookup above; a hidden path does not count as its own child.
func (p *PathsFilter) HasHiddenChildPath(path string) bool {
	return p.hiddenDescendantCount[path] > 0
}

// APHELION EDIT ADDITION END

func (p *PathsFilter) TogglePath(path string) {
	// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
	changed := p.togglePath(path, p.IsVisiblePath(path))
	if changed {
		p.policyRevision++
	}
	// APHELION EDIT ADDITION END
	log.Printf("toggle [%s] path: [%t]", path, p.IsVisiblePath(path))
}

/* APHELION EDIT REMOVAL START - VISIBILITY POLICY CACHE
func (p *PathsFilter) togglePath(path string, isFilteredOut bool) {
	for _, directChild := range p.findDirectChildren(path) {
		p.togglePath(directChild, isFilteredOut)
	}
	if isFilteredOut {
		p.filteredPaths[path] = true
	} else {
		delete(p.filteredPaths, path)
	}
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - VISIBILITY POLICY CACHE
// A recursive subtree gesture updates the direct map and ancestor summaries,
// then publishes exactly one revision at the public TogglePath boundary.
func (p *PathsFilter) togglePath(path string, isFilteredOut bool) bool {
	changed := false
	for _, directChild := range p.findDirectChildren(path) {
		changed = p.togglePath(directChild, isFilteredOut) || changed
	}
	return p.setPathHidden(path, isFilteredOut) || changed
}

func (p *PathsFilter) setPathHidden(path string, hidden bool) bool {
	if p.IsHiddenPath(path) == hidden {
		return false
	}
	if p.hiddenDescendantCount == nil {
		p.hiddenDescendantCount = make(map[string]int)
	}
	if hidden {
		if p.filteredPaths == nil {
			p.filteredPaths = make(map[string]bool)
		}
		p.filteredPaths[path] = true
	} else {
		delete(p.filteredPaths, path)
	}

	for ancestor := parentTypePath(path); ancestor != ""; ancestor = parentTypePath(ancestor) {
		if hidden {
			p.hiddenDescendantCount[ancestor]++
			continue
		}
		if count := p.hiddenDescendantCount[ancestor]; count <= 1 {
			delete(p.hiddenDescendantCount, ancestor)
		} else {
			p.hiddenDescendantCount[ancestor] = count - 1
		}
	}
	return true
}

// APHELION EDIT ADDITION END
