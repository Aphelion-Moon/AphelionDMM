package cpsearch

import (
	"testing"

	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestLargeSearchYieldsAndFencesReplacementDuringScan(t *testing.T) {
	s := searchFixture(t, 10000, 2)
	s.SearchByPath("/obj/search")
	if s.query == nil || s.resultReady || len(s.resultsAll) != 0 {
		t.Fatal("large query published partial or synchronous results")
	}
	a := s.app.(*searchTestApp)
	other := a.current.Dmm().Copy()
	other.Tiles = other.Tiles[:1]
	other.MaxX = 1
	a.current = editor.New(a, nil, &other)
	if !s.ensureCurrent() || len(s.resultsAll) != 1 || s.resultsAll[0] != other.Tiles[0].Instances()[0] {
		t.Fatal("stale query crossed editor generation")
	}
}

func TestSearchActionsAfterMapClose(t *testing.T) {
	searchUI(t)
	for name, action := range map[string]func(*Search){
		"select":      func(s *Search) { s.selectInstance(0) },
		"delete":      func(s *Search) { s.deleteInstance(0) },
		"replace":     func(s *Search) { s.replaceInstance(0) },
		"delete all":  (*Search).doDeleteAll,
		"replace all": (*Search).doReplaceAll,
		"next":        (*Search).jumpToDown,
		"previous":    (*Search).jumpToUp,
	} {
		t.Run(name, func(t *testing.T) {
			s := searchFixture(t, 3, 1)
			s.SearchByPath("/obj/search")
			s.app.(*searchTestApp).current = nil
			defer func() {
				if failure := recover(); failure != nil {
					t.Errorf("closed-map search action panicked: %v", failure)
				}
			}()
			action(s)
			if len(s.results()) != 0 {
				t.Error("closed-map action retained old results")
			}
		})
	}
}

func TestSearchChangedRowIsRefreshedWithoutRetargeting(t *testing.T) {
	searchUI(t)
	s, _ := searchOperationFixture(t)
	s.SearchByPath("/obj/search")
	ed := s.app.CurrentEditor()
	ed.InstanceReplace(s.results()[0], dmmprefab.New(500, "/obj/changed", dmvars.FromParent(nil)))
	ed.CommitOperation("Change fixture row")
	searchAuthority(t, ed)
	s.selectInstance(0)
	if len(ed.FlickInstance()) != 0 {
		t.Error("stale row action retargeted the refreshed list")
	}
	if len(s.results()) != 2 || s.results()[0].Coord().X != 2 {
		t.Fatal("query did not reflect the current display edit")
	}
	s.selectInstance(0)
	if len(ed.FlickInstance()) != 1 || ed.FlickInstance()[0].Instance != s.results()[0] {
		t.Error("fresh row was not usable after refresh")
	}
}

func TestSearchAutomaticRefreshPreservesFilterBounds(t *testing.T) {
	s, _ := searchOperationFixtureSize(t, 5, 1, 1)
	s.SearchByPath("/obj/search")
	bounds := util.Bounds{X1: 2, Y1: 1, X2: 3, Y2: 1}
	s.filterActive, s.filterBound = true, bounds
	s.updateFilteredResults()
	ed := s.app.CurrentEditor()
	ed.InstanceReplace(s.results()[0], dmmprefab.New(500, "/obj/changed", dmvars.FromParent(nil)))
	ed.CommitOperation("Change fixture row")
	searchAuthority(t, ed)
	if !s.ensureCurrent() || s.filterBound != bounds || !s.filterActive {
		t.Fatal("automatic refresh discarded the current-map filter")
	}
	if len(s.results()) != 1 || s.results()[0].Coord().X != 3 || s.selectedResultIdx != -1 {
		t.Fatal("filter membership/navigation did not follow changed results")
	}
}

func TestSearchUnchangedViewDoesNotRebuildQuery(t *testing.T) {
	s := searchFixture(t, 10000, 16)
	s.SearchByPath("/obj/search")
	for !s.ensureCurrent() {
	}
	firstSlot := &s.resultsAll[0]
	allocs := testing.AllocsPerRun(100, func() {
		if !s.ensureCurrent() {
			t.Fatal("unchanged map became unavailable")
		}
	})
	if allocs != 0 || &s.resultsAll[0] != firstSlot {
		t.Fatalf("unchanged frames rebuilt search results: allocs=%v", allocs)
	}
}

func TestSearchRowDoesNotCrossEditor(t *testing.T) {
	searchUI(t)
	s := searchFixture(t, 3, 1)
	s.SearchByPath("/obj/search")
	a := s.app.(*searchTestApp)
	otherMap := a.current.Dmm().Copy()
	other := editor.New(a, nil, &otherMap)
	a.current = other
	s.selectInstance(0)
	if len(other.FlickInstance()) != 0 {
		t.Error("old result selected an instance through the newly active editor")
	}
	if len(s.results()) != 3 || s.results()[0] != other.Dmm().Tiles[0].Instances()[0] {
		t.Error("map switch did not replace search results with current instances")
	}
}
