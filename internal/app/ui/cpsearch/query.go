// APHELION EDIT ADDITION START - SEARCH QUERY LIFECYCLE
package cpsearch

import (
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	mapsearch "sdmm/internal/aphelion/search"
	"sdmm/internal/dmapi/dmmap"
)

func (s *Search) searchCurrentMap() {
	s.Free()
	ed := s.currentEditor()
	if ed == nil {
		return
	}
	version, ready := ed.MapViewVersion()
	if !ready {
		return
	}
	s.resultEditor, s.resultVersion, s.resultReady = ed, version, true
	if s.prefabId == "" {
		return
	}
	log.Print("searching for:", s.prefabId)
	var ids []uint64
	if strings.HasPrefix(s.prefabId, "/") {
		prefabs := dmmap.PrefabStorage.GetAllByPath(s.prefabId)
		ids = make([]uint64, len(prefabs))
		for i, prefab := range prefabs {
			ids[i] = prefab.Id()
		}
	} else {
		id, err := strconv.ParseUint(s.prefabId, 10, 64)
		if err != nil {
			return
		}
		ids = []uint64{id}
	}
	s.query = mapsearch.NewCursor(ed.Dmm(), ids)
	s.resultReady = false
	s.advanceQuery()
}

func (s *Search) advanceQuery() bool {
	if s.query == nil {
		return s.resultReady
	}
	if !s.query.Step(4096, time.Now().Add(2*time.Millisecond)) {
		return false
	}
	s.resultsAll = s.query.Result()
	s.query = nil
	s.resultReady = true
	if !s.filterBound.IsEmpty() || !s.filterLevels.IsAll() {
		s.updateFilteredResults()
	}
	return true
}

func (s *Search) resetResultNavigation() {
	s.selectedResultIdx, s.focusedResultIdx, s.lastFocusedResultIdx = -1, -1, -1
	s.resultGeneration++
}

// APHELION EDIT ADDITION END
