// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
package cpsearch

import (
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

func (s *Search) currentEditor() *editor.Editor {
	if s.app == nil {
		return nil
	}
	return s.app.CurrentEditor()
}

// ensureCurrent coalesces changes into one query when the display is ready.
// Automatic same-map refresh retains the user's filter bounds; explicit new
// queries and map switches keep the existing reset behavior.
func (s *Search) ensureCurrent() bool {
	ed := s.currentEditor()
	if ed == nil {
		if s.resultEditor != nil || len(s.resultsAll) != 0 || len(s.resultsFiltered) != 0 {
			s.Free()
		}
		return false
	}
	version, ready := ed.MapViewVersion()
	if !ready {
		return false
	}
	if s.resultEditor == ed && s.resultReady && s.resultVersion == version {
		return true
	}
	sameEditor, bounds, levels := s.resultEditor == ed, s.filterBound, s.filterLevels
	s.searchCurrentMap()
	if sameEditor && (!bounds.IsEmpty() || !levels.IsAll()) {
		s.filterBound, s.filterLevels = bounds, levels
		s.updateFilteredResults()
	}
	return s.resultReady
}

// A row index belongs to the result set that was drawn. Refresh a stale set but
// do not apply that old index (or old bulk action) to newly discovered results.
// Navigation explicitly refreshes first before choosing its next index.
func (s *Search) actionEditor() *editor.Editor {
	ed, version, wasReady := s.resultEditor, s.resultVersion, s.resultReady
	if !s.ensureCurrent() || !wasReady || s.resultEditor != ed || s.resultVersion != version {
		return nil
	}
	return ed
}

func (s *Search) actionResult(idx int) (*editor.Editor, *dmminstance.Instance, bool) {
	ed := s.actionEditor()
	if ed == nil || idx < 0 || idx >= len(s.results()) {
		return nil, nil, false
	}
	return ed, s.results()[idx], true
}

// APHELION EDIT ADDITION END
