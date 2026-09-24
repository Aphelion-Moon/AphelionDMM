// APHELION EDIT ADDITION START - SELECTION HISTORY
package tools

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

type selectionTransformObserver interface {
	TrackSelectionTransform(func(bool), func() error) error
}

func (t *ToolGrab) trackSelectionTransform(before util.Bounds, discardUnchanged bool, action func() (util.Bounds, error)) error {
	selection := t.Selection()
	a := selection.Bounds()
	selection = selection.Translate(util.Point{X: int(before.X1 - a.X1), Y: int(before.Y1 - a.Y1)})
	return t.trackSelectionMask(selection, discardUnchanged, func() (editing.Selection, error) {
		after, err := action()
		if err != nil {
			return selection, err
		}
		return selection.Translate(util.Point{X: int(after.X1 - before.X1), Y: int(after.Y1 - before.Y1)}), nil
	})
}

func (t *ToolGrab) trackSelectionMask(before editing.Selection, discardUnchanged bool, action func() (editing.Selection, error)) error {
	if t.selectionHistory == nil {
		t.selectionHistory = editing.NewMaskSelectionHistory(before)
	}
	history, owner := t.selectionHistory, ed.Dmm()
	change := history.Add(before.Bounds())
	change.SetSelection(before)
	changed := func(applied bool) {
		change.SetApplied(applied)
		// A new explicit selection or another map owns its own geometry. A newer
		// open mouse gesture keeps its preview until release/cancellation.
		if t.selectionHistory == history && ed != nil && ed.Dmm() == owner && !t.dragging {
			t.refreshSelectionHistory(false)
		}
	}
	apply := func() error {
		area, err := action()
		if err != nil || (discardUnchanged && area.Bounds() == before.Bounds()) {
			change.SetApplied(false)
		} else {
			change.SetSelection(area)
		}
		return err
	}
	var err error
	if observer, ok := ed.(selectionTransformObserver); ok {
		err = observer.TrackSelectionTransform(changed, apply)
	} else {
		err = apply()
	}
	history.DiscardUnappliedTail(change)
	t.refreshSelectionHistory(true)
	return err
}

func (t *ToolGrab) refreshSelectionHistory(refreshContents bool) {
	if t.selectionHistory == nil || !t.HasSelectedArea() {
		return
	}
	area := t.selectionHistory.Bounds()
	if !refreshContents && !t.selectionHistory.Selection().Sparse() && area == t.fillArea && area == t.fillAreaInit {
		return
	}
	t.fillArea = area
	if s := t.selectionHistory.Selection(); s.Len() > 0 {
		t.setSelection(s)
	}
	t.fillStart.X, t.fillStart.Y = int(t.fillArea.X1), int(t.fillArea.Y1)
	t.mode = tSelectModeMoveArea
	t.stopMoveArea()
}

// APHELION EDIT ADDITION END
