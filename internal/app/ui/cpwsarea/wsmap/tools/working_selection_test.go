package tools

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
	"testing"
)

func TestGrabAddToggleAndCancelledExtension(t *testing.T) {
	g, e := lifecycleFixture(t)
	p := func(x int) util.Point { return util.Point{X: x, Y: 1, Z: 1} }
	g.startSelectionGesture(p(3), editing.SelectionAdd, false, true)
	g.onStop(p(3))
	if g.Selection().Len() != 2 || g.Selection().Contains(p(2)) {
		t.Fatal("add filled gap or lost prior member")
	}
	g.startSelectionGesture(p(3), editing.SelectionAdd, false, true)
	g.onStop(p(3))
	if g.Selection().Len() != 1 || !g.Selection().Contains(p(1)) {
		t.Fatal("toggle removed unrelated membership")
	}
	g.startSelectionGesture(p(3), editing.SelectionAdd, false, false)
	g.onMove(p(4))
	g.CancelGesture()
	if g.Selection().Len() != 1 || !g.Selection().Contains(p(1)) || e.commits != 0 {
		t.Fatal("cancel changed previous selection or map")
	}
}

type workingEditor struct {
	*lifecycleEditor
	selection editing.WorkingSelection
	level     int
}

func (e *workingEditor) WorkingSelection() *editing.WorkingSelection { return &e.selection }
func (e *workingEditor) ActiveLevel() int                            { return e.level }

func TestGrabMembershipSurvivesToolsAndLevels(t *testing.T) {
	g, base := lifecycleFixture(t)
	owner := &workingEditor{lifecycleEditor: base, level: 1}
	ed = owner
	previous, name := tools[TNGrab], selectedToolName
	t.Cleanup(func() { tools[TNGrab], selectedToolName = previous, name })
	tools[TNGrab], selectedToolName = g, TNGrab
	g.publishSelection()
	SetSelected(TNPick)
	if len(SelectedTiles()) != 1 || !owner.selection.Get(1).Contains(util.Point{X: 1, Y: 1, Z: 1}) {
		t.Fatal("switch erased selection")
	}
	owner.level = 2
	BindSelectionLevel(owner)
	if g.HasSelectedArea() {
		t.Fatal("selection crossed Z")
	}
	owner.level = 1
	BindSelectionLevel(owner)
	if !g.HasSelectedArea() || g.SelectionLevel() != 1 {
		t.Fatal("level selection did not restore")
	}
}
