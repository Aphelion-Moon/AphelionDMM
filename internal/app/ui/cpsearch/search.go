package cpsearch

import (
	"strconv"
	// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
	mapsearch "sdmm/internal/aphelion/search"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"

	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

type App interface {
	CurrentEditor() *editor.Editor
	DoEditInstance(*dmminstance.Instance)
	ShowLayout(name string, focus bool)
}

type Search struct {
	component.Component

	app App

	shortcuts shortcut.Shortcuts

	prefabId string

	selectedResultIdx    int
	focusedResultIdx     int
	lastFocusedResultIdx int

	filterActive bool
	filterBound  util.Bounds
	// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
	filterLevels mapsearch.LevelRange
	// APHELION EDIT ADDITION END

	resultsAll      []*dmminstance.Instance
	resultsFiltered []*dmminstance.Instance
	// APHELION EDIT ADDITION START - SEARCH QUERY LIFECYCLE
	resultGeneration uint64
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	resultEditor  *editor.Editor
	resultVersion uint64
	resultReady   bool
	// APHELION EDIT ADDITION END
}

func (s *Search) Init(app App) {
	s.app = app

	s.selectedResultIdx = -1
	s.focusedResultIdx = -1
	s.lastFocusedResultIdx = -1

	s.addShortcuts()

	s.AddOnFocused(func(focused bool) {
		s.shortcuts.SetVisible(focused)
	})
}

func (s *Search) Free() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	s.resultEditor, s.resultVersion, s.resultReady = nil, 0, false
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SEARCH RETENTION - ORIGINAL: s.resultsAll = s.resultsAll[:0]
	s.resultsAll = nil
	s.selectedResultIdx = -1
	s.focusedResultIdx = -1
	s.lastFocusedResultIdx = -1
	s.doResetFilter()
	log.Print("search free")
}

func (s *Search) Sync() {
	// APHELION EDIT CHANGE - SEARCH LEVEL FILTER - ORIGINAL: s.doSearch()
	s.ensureCurrent()
}

func (s *Search) Search(prefabId uint64) {
	s.prefabId = strconv.FormatUint(prefabId, 10)
	s.doSearch()
}

func (s *Search) SearchByPath(path string) {
	s.prefabId = path
	s.doSearch()
}

func (s *Search) results() []*dmminstance.Instance {
	// APHELION EDIT CHANGE - SEARCH LEVEL FILTER - ORIGINAL: if !s.filterBound.IsEmpty() {
	if !s.filterBound.IsEmpty() || !s.filterLevels.IsAll() {
		return s.resultsFiltered
	}
	return s.resultsAll
}
