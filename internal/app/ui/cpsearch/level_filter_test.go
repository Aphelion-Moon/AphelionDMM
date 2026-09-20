package cpsearch

import (
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	mapsearch "sdmm/internal/aphelion/search"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSearchLevelControls(t *testing.T) {
	searchUI(t)
	s, _ := searchOperationFixtureLevels(t, 3)
	io := imgui.CurrentIO()
	frame := func() (checkbox, sliderMin, sliderMax imgui.Vec2) {
		imgui.NewFrame()
		imgui.SetNextWindowPosV(imgui.Vec2{}, imgui.ConditionAlways, imgui.Vec2{})
		imgui.SetNextWindowSizeV(imgui.Vec2{X: 500, Y: 300}, imgui.ConditionAlways)
		imgui.BeginV("Search levels", nil, imgui.WindowFlagsNoSavedSettings)
		checkbox = imgui.CursorScreenPos()
		checkbox.X += 5
		checkbox.Y += imgui.FrameHeight() / 2
		s.showLevelFilter()
		sliderMin, sliderMax = imgui.ItemRectMin(), imgui.ItemRectMax()
		imgui.End()
		imgui.Render()
		return
	}
	frame()
	checkbox, _, _ := frame()
	click := func(point imgui.Vec2) {
		io.SetMousePosition(point)
		io.SetMouseButtonDown(0, true)
		frame()
		io.SetMouseButtonDown(0, false)
		frame()
	}
	click(checkbox)
	if s.filterLevels != (mapsearch.LevelRange{First: 1, Last: 1}) || len(s.results()) != 3 {
		t.Fatal("unchecking All Z levels did not select the active level")
	}
	_, lower, upper := frame()
	// The two-component slider occupies the last item rectangle. Select the
	// upper end of its second component through real ImGui mouse input.
	click(imgui.Vec2{X: upper.X - 5, Y: (lower.Y + upper.Y) / 2})
	if s.filterLevels.Last != 3 || len(s.results()) != 9 {
		t.Fatalf("range slider did not update membership: %+v", s.filterLevels)
	}
	click(checkbox)
	if !s.filterLevels.IsAll() || len(s.results()) != 9 {
		t.Fatal("All Z levels did not clear the range")
	}
}

func TestSearchLevelFilterCombinesBoundsAndSurvivesRefresh(t *testing.T) {
	s, app := searchOperationFixtureSize(t, 2, 2, 3)
	e := s.app.CurrentEditor()
	m := e.Dmm()
	s.SearchByPath("/obj/search")
	s.filterActive = true
	s.filterLevels = mapsearch.LevelRange{First: 2, Last: 3}
	s.selectedResultIdx, s.focusedResultIdx = 5, 5
	s.updateFilteredResults()
	if len(s.results()) != 4 || s.selectedResultIdx != -1 || s.focusedResultIdx != -1 {
		t.Fatal("Z-only filter or navigation reset failed")
	}
	for _, result := range s.results() {
		if result.Coord().Z < 2 {
			t.Fatal("Z-only filter includes another level")
		}
	}
	s.filterBound = util.Bounds{X1: 2, Y1: 1, X2: 2, Y2: 1}
	s.updateFilteredResults()
	if len(s.results()) != 2 || s.results()[0].Coord().Z != 2 || s.results()[1].Coord().Z != 3 {
		t.Fatal("XY/Z intersection changed membership or order")
	}
	e.InstanceReplace(s.results()[0], dmmprefab.New(500, "/obj/changed", dmvars.FromParent(nil)))
	e.CommitOperation("Change fixture row")
	searchAuthority(t, e)
	s.Sync()
	if !s.filterActive || s.filterLevels != (mapsearch.LevelRange{First: 2, Last: 3}) || len(s.results()) != 1 || s.results()[0].Coord().Z != 3 {
		t.Fatal("same-map refresh lost the Z filter")
	}
	// A shrink must not clamp Z=3 to Z=1 and widen a later bulk mutation.
	s.filterLevels = mapsearch.LevelRange{First: 3, Last: 3}
	if err := e.ResizeMap(2, 1, 1); err != nil {
		t.Fatal(err)
	}
	s.Sync()
	if s.filterLevels.First != 3 || len(s.results()) != 0 {
		t.Fatal("shrink broadened the requested levels")
	}
	other := m.Copy()
	app.current = editor.New(app, nil, &other)
	s.ensureCurrent()
	if !s.filterLevels.IsAll() || !s.filterBound.IsEmpty() || len(s.results()) != 2 {
		t.Fatal("map switch retained another map's filter")
	}
}

func TestSearchLevelBulkActionsPreserveOtherLevelsAndUndo(t *testing.T) {
	for name, action := range map[string]func(*Search){"delete": (*Search).doDeleteAll, "replace": (*Search).doReplaceAll} {
		t.Run(name, func(t *testing.T) {
			s, app := searchOperationFixtureLevels(t, 3)
			e := app.current
			before, hash := searchAuthority(t, e)
			s.filterActive, s.filterLevels = true, mapsearch.LevelRange{First: 2, Last: 2}
			s.updateFilteredResults()
			if len(s.results()) != 3 {
				t.Fatal("fixture does not isolate one level")
			}
			action(s)
			after, _ := searchAuthority(t, e)
			if after.Revision != before.Revision+1 || len(app.errors) != 0 || len(s.results()) != 0 || s.filterLevels.First != 2 {
				t.Fatal("filtered action failed or lost its filter")
			}
			for i, tile := range after.Tiles {
				if tile.Coord.Z != 2 && !reflect.DeepEqual(tile, before.Tiles[i]) {
					t.Fatal("filtered action changed another level")
				}
			}
			action(s)
			unchanged, _ := searchAuthority(t, e)
			if unchanged.Revision != after.Revision {
				t.Fatal("repeated bulk action escaped the now-empty filter")
			}
			app.commands.UndoV(e.Dmm().Path.Absolute)
			_, undone := searchAuthority(t, e)
			if undone != hash {
				t.Fatal("filtered bulk undo did not restore exact state")
			}
			s.Sync()
			if len(s.results()) != 3 {
				t.Fatal("undo did not refresh filtered results")
			}
			s.doToggleFilter()
			if !s.filterLevels.IsAll() || len(s.results()) != 9 {
				t.Fatal("turning off filtering did not restore all levels")
			}
		})
	}
}
