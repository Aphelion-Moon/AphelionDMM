package cpsearch

import (
	"math"
	// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
	mapsearch "sdmm/internal/aphelion/search"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/imguiext"
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

func (s *Search) filterButton() w.Layout {
	var bntStyle w.ButtonStyle
	if s.filterActive {
		bntStyle = style.ButtonGreen{}
	} else {
		bntStyle = style.ButtonDefault{}
	}

	return w.Layout{
		w.Button(icon.FilterAlt, s.doToggleFilter).
			Style(bntStyle).
			Round(true),
		w.Tooltip(w.AlignTextToFramePadding(), w.Line(w.Text("Filter"), w.TextFrame("F"))),
	}
}

func (s *Search) showFilter() {
	s.fetchGrabToolFilterBounds()

	imgui.AlignTextToFramePadding()

	imgui.TextDisabled(icon.Help)
	imguiext.SetItemHoveredTooltip(
		"Filter results with bounds\n" +
			"Control with sliders: X1, Y1, X2, Y2\n" +
			"Or select an area with the \"Grab\" tool",
	)

	imgui.SameLine()

	w.Button(icon.Delete+"##reset_results", s.doResetFilter).
		Style(style.ButtonRed{}).
		Tooltip("Reset").
		Build()

	imgui.SameLine()

	var bounds [4]int32
	bounds[0] = int32(s.filterBound.X1)
	bounds[1] = int32(s.filterBound.Y1)
	bounds[2] = int32(s.filterBound.X2)
	bounds[3] = int32(s.filterBound.Y2)

	max := math.Max(float64(s.app.CurrentEditor().Dmm().MaxX), float64(s.app.CurrentEditor().Dmm().MaxY))

	imgui.SetNextItemWidth(-1)
	if imgui.SliderInt4("##bounds", &bounds, 0, int(max)) {
		s.filterBound.X1 = float32(bounds[0])
		s.filterBound.Y1 = float32(bounds[1])
		s.filterBound.X2 = float32(bounds[2])
		s.filterBound.Y2 = float32(bounds[3])
		s.updateFilteredResults()
	}
	// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
	s.showLevelFilter()
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
func (s *Search) showLevelFilter() {
	ed := s.currentEditor()
	if ed == nil {
		return
	}
	all := s.filterLevels.IsAll()
	if imgui.Checkbox("All Z levels", &all) {
		if all {
			s.filterLevels = mapsearch.LevelRange{}
		} else {
			level := ed.ActiveLevel()
			s.filterLevels = mapsearch.LevelRange{First: level, Last: level}
		}
		s.updateFilteredResults()
	}
	if !all {
		levels := [2]int32{int32(s.filterLevels.First), int32(s.filterLevels.Last)}
		imgui.Text("Z range (first, last)")
		imgui.SetNextItemWidth(-1)
		if imgui.SliderInt2("##z_range", &levels, 1, ed.Dmm().MaxZ) {
			first, last := max(1, min(int(levels[0]), ed.Dmm().MaxZ)), max(1, min(int(levels[1]), ed.Dmm().MaxZ))
			s.filterLevels = mapsearch.LevelRange{First: min(first, last), Last: max(first, last)}
			s.updateFilteredResults()
		}
		if s.filterLevels.First > ed.Dmm().MaxZ {
			imgui.TextDisabled("Selected Z range is outside this map")
		}
	}
}

// APHELION EDIT ADDITION END

func (s *Search) doToggleFilter() {
	s.filterActive = !s.filterActive

	log.Print("filter toggled:", s.filterActive)

	if !s.filterActive {
		s.doResetFilter()
	}
}

func (s *Search) fetchGrabToolFilterBounds() {
	if !tools.IsSelected(tools.TNGrab) {
		return
	}

	grab := tools.Selected().(*tools.ToolGrab)

	if !grab.HasSelectedArea() {
		return
	}

	if s.filterBound != grab.Bounds() {
		s.filterBound = grab.Bounds()
		s.updateFilteredResults()
	}
}

func (s *Search) doResetFilter() {
	// APHELION EDIT CHANGE - SEARCH RETENTION - ORIGINAL: s.resultsFiltered = s.resultsFiltered[:0]
	s.resultsFiltered = nil
	// APHELION EDIT ADDITION START - SEARCH QUERY LIFECYCLE
	s.resetResultNavigation()
	// APHELION EDIT ADDITION END
	s.filterBound = util.Bounds{}
	// APHELION EDIT ADDITION START - SEARCH LEVEL FILTER
	s.filterLevels = mapsearch.LevelRange{}
	// APHELION EDIT ADDITION END
	log.Print("search filter reset")
}

func (s *Search) updateFilteredResults() {
	// APHELION EDIT ADDITION START - SEARCH QUERY LIFECYCLE
	clear(s.resultsFiltered)
	s.resetResultNavigation()
	// APHELION EDIT ADDITION END
	s.resultsFiltered = s.resultsFiltered[:0]
	for _, result := range s.resultsAll {
		// APHELION EDIT CHANGE - SEARCH LEVEL FILTER - ORIGINAL: if s.filterBound.Contains(float32(result.Coord().X), float32(result.Coord().Y)) {
		if (s.filterBound.IsEmpty() || s.filterBound.Contains(float32(result.Coord().X), float32(result.Coord().Y))) && s.filterLevels.Contains(result.Coord().Z) {
			s.resultsFiltered = append(s.resultsFiltered, result)
		}
	}
}
