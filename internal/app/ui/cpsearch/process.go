package cpsearch

import (
	"fmt"
	/* APHELION EDIT REMOVAL START - SEARCH QUERY LIFECYCLE
	"strconv"
	"strings"
	APHELION EDIT REMOVAL END */

	"sdmm/internal/app/ui/layout/lnode"
	// APHELION EDIT ADDITION START - SEARCH CAPTURE OWNERSHIP
	"sdmm/internal/dmapi/dmmap/dmminstance"
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - SEARCH QUERY LIFECYCLE
	"sdmm/internal/dmapi/dmmap"
	APHELION EDIT REMOVAL END */
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

func (s *Search) Process(int32) {
	// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: if s.app.CurrentEditor() == nil {
	if s.currentEditor() == nil {
		// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
		s.ensureCurrent()
		// APHELION EDIT ADDITION END
		imgui.TextDisabled("No map opened")
		return
	}

	s.showControls()
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	if !s.ensureCurrent() {
		// APHELION EDIT CHANGE - BOUNDED QUERY - ORIGINAL: imgui.TextDisabled("Finish or cancel the current edit to update search results")
		if s.query != nil {
			imgui.TextDisabled("Updating search results...")
		} else {
			imgui.TextDisabled("Finish or cancel the current edit to update search results")
		}
		return
	}
	// APHELION EDIT ADDITION END

	imgui.Separator()

	s.showResultsControls()

	imgui.Separator()

	if s.filterActive {
		s.showFilter()
		imgui.Separator()
	}

	if imgui.BeginChild("results") {
		s.showResults()
	}
	imgui.EndChild()
}

func (s *Search) showControls() {
	w.Layout{
		w.Button(icon.Search, s.doSearch).
			Round(true).
			Tooltip("Search"),
		w.SameLine(),
		w.InputTextWithHint("##search", "Type or Prefab ID", &s.prefabId).
			ButtonClear().
			Width(-1).
			OnDeactivatedAfterEdit(func() {
				s.doSearch()
			}),
	}.Build()
}

func (s *Search) doSearch() {
	// APHELION EDIT ADDITION START - SEARCH QUERY LIFECYCLE
	s.searchCurrentMap()
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - SEARCH QUERY LIFECYCLE
	if len(s.prefabId) == 0 {
		s.Free()
		return
	}

	s.Free()

	log.Print("searching for:", s.prefabId)

	if strings.HasPrefix(s.prefabId, "/") {
		for _, prefab := range dmmap.PrefabStorage.GetAllByPath(s.prefabId) {
			s.resultsAll = append(s.resultsAll, s.app.CurrentEditor().InstancesFindByPrefabId(prefab.Id())...)
		}
	} else {
		prefabId, err := strconv.ParseUint(s.prefabId, 10, 64)
		if err != nil {
			return
		}
		s.resultsAll = s.app.CurrentEditor().InstancesFindByPrefabId(prefabId)
	}

	log.Print("found search results:", len(s.resultsAll))
	APHELION EDIT REMOVAL END */
}

func (s *Search) showResultsControls() {
	w.Layout{
		w.Line(
			s.filterButton(),
			w.TextDisabled("|"),
			s.modifyButtons(),
		),
		w.SameLine(),
		w.Layout{
			w.AlignRight,
			w.Line(
				w.Text(fmt.Sprintf("%d/%d", s.selectedResultIdx+1, len(s.results()))),
				w.TextDisabled("|"),
				s.jumpButtons(),
			),
		},
	}.Build()
}

const resultsTableFlags = imgui.TableFlagsBordersInner | imgui.TableFlagsResizable | imgui.TableFlagsNoSavedSettings

func (s *Search) showResults() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	if !s.ensureCurrent() {
		return
	}
	// APHELION EDIT ADDITION END
	if imgui.BeginTableV("search_result", 2, resultsTableFlags, imgui.Vec2{}, 0) {
		// APHELION EDIT ADDITION START - SEARCH TABLE CLIPPING
		results, generation := s.results(), s.resultGeneration
		rowHeight := imgui.FrameHeight() + 2*imgui.CurrentStyle().CellPadding().Y
		if s.focusedResultIdx >= 0 && s.focusedResultIdx < len(results) && s.focusedResultIdx != s.lastFocusedResultIdx {
			imgui.SetScrollY(imgui.CursorPosY() + float32(s.focusedResultIdx)*rowHeight)
			s.lastFocusedResultIdx = s.focusedResultIdx
		}
		var clipper imgui.ListClipper
		clipper.BeginV(len(results), rowHeight)
		for clipper.Step() {
			// APHELION EDIT CHANGE - SEARCH TABLE CLIPPING - ORIGINAL: for idx, instance := range s.results() {
			for idx := clipper.DisplayStart; idx < clipper.DisplayEnd; idx++ {
				instance := results[idx]
				// APHELION EDIT ADDITION END
				/* APHELION EDIT REMOVAL START - SEARCH TABLE CLIPPING
				if idx == s.focusedResultIdx && idx != s.lastFocusedResultIdx {
					imgui.SetScrollHereY(0)
					s.lastFocusedResultIdx = s.focusedResultIdx
				}
				APHELION EDIT REMOVAL END */

				imgui.TableNextColumn()

				selected := idx == s.selectedResultIdx
				if selected {
					imgui.PushStyleColor(imgui.StyleColorText, style.ColorGold)
				}
				imgui.AlignTextToFramePadding()
				imgui.Text(fmt.Sprintf("X:%03d Y:%03d Z:%d", instance.Coord().X, instance.Coord().Y, instance.Coord().Z))
				if selected {
					imgui.PopStyleColor()
				}

				imgui.TableNextColumn()

				w.Layout{
					w.Line(
						w.Button(fmt.Sprint(icon.Search+"##jump_to_", instance.Id()), func() {
							s.jumpTo(idx, false)
						}).Round(true).Tooltip("Jump To"),
						w.Button(fmt.Sprint(icon.EyeDropper+"##select_", instance.Id()), func() {
							s.selectInstance(idx)
						}).Round(true).Tooltip("Select"),
						w.Button(fmt.Sprint(icon.Eraser+"##delete_", instance.Id()), func() {
							s.deleteInstance(idx)
						}).Round(true).Tooltip("Delete"),
						w.Button(fmt.Sprint(icon.Repeat+"##replace_", instance.Id()), func() {
							s.replaceInstance(idx)
						}).Round(true).Tooltip("Replace with Selected"),
					),
				}.Build()
				// APHELION EDIT ADDITION START - SEARCH TABLE CLIPPING
				if generation != s.resultGeneration {
					clipper.End()
					imgui.EndTable()
					return
				}
				// APHELION EDIT ADDITION END
			}
			// APHELION EDIT ADDITION START - SEARCH TABLE CLIPPING
		}
		// APHELION EDIT ADDITION END

		imgui.EndTable()
	}
}

func (s *Search) modifyButtons() w.Layout {
	return w.Layout{
		w.Line(
			w.Button(icon.Eraser, s.doDeleteAll).
				Round(true).
				Tooltip("Delete All"),
			w.Button(icon.Repeat, s.doReplaceAll).
				Round(true).
				Tooltip("Replace All with Selected"),
		),
	}
}

func (s *Search) doDeleteAll() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor := s.actionEditor()
	if editor == nil || !editor.CanStartMapEdit() || len(s.results()) == 0 {
		return
	}
	// APHELION EDIT ADDITION END
	log.Print("do delete all")
	// APHELION EDIT ADDITION START - SEARCH CAPTURE OWNERSHIP
	editor.CommitInstanceBatch(s.results(), nil, "Delete All")
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - SEARCH CAPTURE OWNERSHIP
	for _, instance := range s.results() {
		// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: s.app.CurrentEditor().InstanceDelete(instance)
		editor.InstanceDelete(instance)
	}
	// APHELION EDIT REMOVAL - SEARCH VIEW OWNERSHIP - ORIGINAL: s.Sync()
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: s.app.CurrentEditor().CommitChanges("Delete All")
	// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: s.app.CurrentEditor().CommitOperation("Delete All")
	editor.CommitOperation("Delete All")
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	s.Sync()
	// APHELION EDIT ADDITION END
}

func (s *Search) doReplaceAll() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor := s.actionEditor()
	if editor == nil || !editor.CanStartMapEdit() || len(s.results()) == 0 {
		return
	}
	// APHELION EDIT ADDITION END
	log.Print("do replace all")
	// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: if selectedPrefab, ok := s.app.CurrentEditor().SelectedPrefab(); ok {
	if selectedPrefab, ok := editor.SelectedPrefab(); ok {
		// APHELION EDIT ADDITION START - SEARCH CAPTURE OWNERSHIP
		editor.CommitInstanceBatch(s.results(), selectedPrefab, "Replace All")
		// APHELION EDIT ADDITION END
		/* APHELION EDIT REMOVAL START - SEARCH CAPTURE OWNERSHIP
		for _, instance := range s.results() {
			// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: s.app.CurrentEditor().InstanceReplace(instance, selectedPrefab)
			editor.InstanceReplace(instance, selectedPrefab)
		}
		// APHELION EDIT REMOVAL - SEARCH VIEW OWNERSHIP - ORIGINAL: s.Sync()
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: s.app.CurrentEditor().CommitChanges("Replace All")
		// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: s.app.CurrentEditor().CommitOperation("Replace All")
		editor.CommitOperation("Replace All")
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
		s.Sync()
		// APHELION EDIT ADDITION END
	}
}

func (s *Search) jumpButtons() w.Layout {
	return w.Layout{
		w.Button(icon.ArrowUpward, s.jumpToUp).
			Round(true).
			Tooltip("Previous Result (Shift+F3)"),
		w.SameLine(),
		w.Button(icon.ArrowDownward, s.jumpToDown).
			Round(true).
			Tooltip("Next Result (F3)"),
	}
}

func (s *Search) selectInstance(idx int) {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor, instance, ok := s.actionResult(idx)
	if !ok {
		return
	}
	// APHELION EDIT ADDITION END
	log.Print("do select instance:", idx)
	/* APHELION EDIT REMOVAL START - SEARCH VIEW OWNERSHIP
	instance := s.results()[idx]
	editor := s.app.CurrentEditor()
	APHELION EDIT REMOVAL END */
	editor.OverlaySetTileFlick(instance.Coord())
	editor.OverlaySetInstanceFlick(instance)
	s.app.ShowLayout(lnode.NameVariables, true)
	s.app.DoEditInstance(instance)
	s.selectedResultIdx = idx
}

func (s *Search) deleteInstance(idx int) {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor, instance, ok := s.actionResult(idx)
	if !ok || !editor.CanStartMapEdit() {
		return
	}
	// APHELION EDIT ADDITION END
	log.Print("do delete instance:", idx)
	/* APHELION EDIT REMOVAL START - SEARCH VIEW OWNERSHIP
	instance := s.results()[idx]
	editor := s.app.CurrentEditor()
	APHELION EDIT REMOVAL END */
	// APHELION EDIT CHANGE - SEARCH CAPTURE OWNERSHIP - ORIGINAL: editor.InstanceDelete(instance)
	editor.CommitInstanceBatch([]*dmminstance.Instance{instance}, nil, "Delete Instance")
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: editor.CommitChanges("Delete Instance")
	// APHELION EDIT REMOVAL - SEARCH CAPTURE OWNERSHIP - ORIGINAL: editor.CommitOperation("Delete Instance")
	s.selectedResultIdx = -1
	s.Sync()
}

func (s *Search) replaceInstance(idx int) {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor, instance, valid := s.actionResult(idx)
	if !valid || !editor.CanStartMapEdit() {
		return
	}
	// APHELION EDIT ADDITION END
	log.Print("do replace instance:", idx)
	// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: if selectedPrefab, ok := s.app.CurrentEditor().SelectedPrefab(); ok {
	if selectedPrefab, ok := editor.SelectedPrefab(); ok {
		/* APHELION EDIT REMOVAL START - SEARCH VIEW OWNERSHIP
		instance := s.results()[idx]
		editor := s.app.CurrentEditor()
		APHELION EDIT REMOVAL END */
		// APHELION EDIT CHANGE - SEARCH CAPTURE OWNERSHIP - ORIGINAL: editor.InstanceReplace(instance, selectedPrefab)
		editor.CommitInstanceBatch([]*dmminstance.Instance{instance}, selectedPrefab, "Replace Instance")
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: editor.CommitChanges("Replace Instance")
		// APHELION EDIT REMOVAL - SEARCH CAPTURE OWNERSHIP - ORIGINAL: editor.CommitOperation("Replace Instance")
		s.selectedResultIdx = -1
		s.Sync()
	}
}

func (s *Search) jumpTo(idx int, focus bool) {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	editor, instance, ok := s.actionResult(idx)
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SEARCH VIEW OWNERSHIP - ORIGINAL: if idx < 0 || idx >= len(s.results()) {
	if !ok {
		return
	}

	/* APHELION EDIT REMOVAL START - SEARCH VIEW OWNERSHIP
	instance := s.results()[idx]
	editor := s.app.CurrentEditor()
	APHELION EDIT REMOVAL END */

	editor.FocusCamera(instance)
	editor.OverlaySetTileFlick(instance.Coord())
	editor.OverlaySetInstanceFlick(instance)

	s.selectedResultIdx = idx

	if focus {
		s.focusedResultIdx = idx
	}
}

func (s *Search) jumpToUp() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	if !s.ensureCurrent() {
		return
	}
	// APHELION EDIT ADDITION END
	idx := s.selectedResultIdx - 1
	if idx < 0 {
		idx = len(s.results()) - 1
	}
	s.jumpTo(idx, true)
}

func (s *Search) jumpToDown() {
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	if !s.ensureCurrent() {
		return
	}
	// APHELION EDIT ADDITION END
	idx := s.selectedResultIdx + 1
	if idx >= len(s.results()) {
		idx = 0
	}
	s.jumpTo(idx, true)
}
