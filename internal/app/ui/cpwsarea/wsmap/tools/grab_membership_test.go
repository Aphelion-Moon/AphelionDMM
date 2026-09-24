package tools

import (
	"reflect"
	"testing"

	"sdmm/internal/util"
)

type preparedMembershipEditor struct{ *lifecycleEditor }

func (*preparedMembershipEditor) UpdatePastePlacement(p util.Point) (util.Bounds, bool, error) {
	return util.Bounds{X1: float32(p.X), Y1: 1, X2: float32(p.X + 1), Y2: 1}, true, nil
}
func (e *preparedMembershipEditor) ConfirmPastePlacement() bool { e.commits++; return true }
func (*preparedMembershipEditor) CancelPastePlacement()         {}
func (*preparedMembershipEditor) PastePlacementClosed() bool    { return false }

func TestPreparedEnterConfirmationKeepsAllSelectedCoordinates(t *testing.T) {
	g, base := lifecycleFixture(t)
	owner := &preparedMembershipEditor{base}
	ed = owner
	previous, name, state := tools[TNGrab], selectedToolName, cs
	t.Cleanup(func() { tools[TNGrab], selectedToolName, cs = previous, name, state })
	tools[TNGrab], selectedToolName = g, TNGrab
	cs = &placementCanvas{point: util.Point{X: 4, Y: 1, Z: 1}}
	if !g.StartPreparedPlacement(owner, util.Point{X: 1, Y: 1, Z: 1}) || !g.ConfirmPlacement() {
		t.Fatal("prepared placement was not confirmed")
	}
	want := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}}
	if got := SelectedTiles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("immediate selection command targets %v, want %v", got, want)
	}
}

func TestSelectionReleaseDoesNotReadObjectContents(t *testing.T) {
	g, owner := lifecycleFixture(t)
	// The geometry is valid but content is intentionally absent. Selecting
	// coordinates must not dereference/copy objects before a command needs them.
	owner.m.Tiles = nil
	g.startSelectArea(util.Point{X: 1, Y: 1, Z: 1})
	g.onMove(util.Point{X: 4, Y: 1, Z: 1})
	g.onStop(util.Point{X: 4, Y: 1, Z: 1})
	if !g.HasSelectedArea() || g.Bounds().X2 != 4 {
		t.Fatal("selection geometry was lost")
	}
}

func TestGrabMaskHoleIsNotMoveTargetAndCannotLeakToAnotherMap(t *testing.T) {
	g, owner := lifecycleFixture(t)
	if err := g.SelectMask([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := g.selectedCoordinates(); len(got) != 2 {
		t.Fatal(got)
	}
	g.onStart(util.Point{X: 2, Y: 1, Z: 1})
	if g.previewMove != nil || g.mode != tSelectModeSelectArea {
		t.Fatal("hole started moving mask")
	}
	g.onStop(util.Point{X: 2, Y: 1, Z: 1})
	other := *owner
	mapCopy := *owner.m
	other.m = &mapCopy
	ed = &other
	if len(g.selectedCoordinates()) != 0 {
		t.Fatal("selection leaked to another document")
	}
}
