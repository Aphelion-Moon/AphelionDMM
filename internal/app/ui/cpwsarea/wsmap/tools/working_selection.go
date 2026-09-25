// APHELION EDIT ADDITION START - PERSISTENT SELECTION
package tools

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

type workingSelectionOwner interface {
	WorkingSelection() *editing.WorkingSelection
	ActiveLevel() int
}

func (t *ToolGrab) SelectingArea() bool { return t.areaQuery != nil }

func SelectionForEditor(owner editor) editing.Selection {
	if source, ok := owner.(workingSelectionOwner); ok {
		return source.WorkingSelection().Get(source.ActiveLevel())
	}
	if ed == owner {
		return tools[TNGrab].(*ToolGrab).Selection()
	}
	return editing.Selection{}
}

func (t *ToolGrab) publishSelection() {
	if source, ok := ed.(workingSelectionOwner); ok {
		selection := t.Selection()
		if selection.Len() == 0 {
			source.WorkingSelection().Clear(source.ActiveLevel())
		} else {
			source.WorkingSelection().Set(selection)
			source.WorkingSelection().SetHistory(selection.Level(), t.selectionHistory)
		}
	}
}

func (t *ToolGrab) restoreSelection() {
	if source, ok := ed.(workingSelectionOwner); ok {
		selection := source.WorkingSelection().Get(source.ActiveLevel())
		if selection.Len() != 0 {
			t.setSelection(selection)
			t.selectionHistory = source.WorkingSelection().History(source.ActiveLevel())
			t.mode = tSelectModeMoveArea
		}
	}
}

// BindSelectionLevel cancels only the departing gesture before restoring the
// destination level. It is safe when a different document owns the tools.
func BindSelectionLevel(owner editor) {
	if ed != owner {
		return
	}
	g := tools[TNGrab].(*ToolGrab)
	g.resetGesture()
	g.restoreSelection()
}

func (t *ToolGrab) CancelGesture() {
	previous := t.gestureSelection
	if previous.Len() == 0 && !t.dragging {
		previous = t.Selection()
	}
	t.resetGesture()
	if source, ok := ed.(workingSelectionOwner); ok {
		previous = source.WorkingSelection().Get(source.ActiveLevel())
	}
	if previous.Len() != 0 {
		t.setSelection(previous)
		t.mode = tSelectModeMoveArea
		if source, ok := ed.(workingSelectionOwner); ok {
			t.selectionHistory = source.WorkingSelection().History(source.ActiveLevel())
		}
	}
}

func (t *ToolGrab) startSelectionGesture(coord util.Point, operation editing.SelectionOperation, area bool, toggle bool) {
	previous := t.Selection()
	t.resetGesture()
	t.gestureSelection, t.selectionOperation, t.toggleClick = previous, operation, toggle
	t.dragging = true
	t.fillStart = coord
	t.selectionAnchor = coord
	t.shape = currentShape()
	if area {
		query, err := editing.NewAreaSelectionQuery(ed.Dmm(), coord, t.AllMatchingAreas)
		if err != nil {
			t.CancelGesture()
			util.ShowErrorDialog(err.Error())
			return
		}
		t.areaQuery = query
		if owner, ok := ed.(interface{ MapViewVersion() (uint64, bool) }); ok {
			t.areaQueryGeneration, _ = owner.MapViewVersion()
		}
		t.setSelection(previous)
		return
	}
	t.onMove(coord)
}

func (t *ToolGrab) processAreaQuery() {
	if t.areaQuery == nil {
		return
	}
	if owner, ok := ed.(interface{ MapViewVersion() (uint64, bool) }); ok {
		generation, ready := owner.MapViewVersion()
		if !ready || generation != t.areaQueryGeneration {
			t.CancelGesture()
			return
		}
	}
	if selection, done := t.areaQuery.Step(512); done {
		t.areaQuery = nil
		t.setSelection(editing.CombineSelection(t.gestureSelection, selection, t.selectionOperation))
		t.mode = tSelectModeMoveArea
		t.dragging = false
		t.publishSelection()
	}
}

// APHELION EDIT ADDITION END
