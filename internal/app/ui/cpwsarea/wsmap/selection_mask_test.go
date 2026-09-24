package wsmap

import (
	"reflect"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestSparsePasteSelectionCopyDeleteUndoAndRotation(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	source := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}}
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), source)
	target := util.Point{X: 1, Y: 2, Z: 1}
	hole := util.Point{X: 2, Y: 2, Z: 1}
	holeBefore, err := mapadapter.CaptureTile(e.Dmm().GetTile(hole))
	if err != nil {
		t.Fatal(err)
	}
	ws.Map().CanvasState().SetMousePosition(0, 32, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app, target)
	grab := tools.Selected().(*tools.ToolGrab)
	if !grab.ConfirmPlacement() {
		t.Fatal("paste not confirmed")
	}
	settlePastePreview(t, ws, app, target)
	want := []util.Point{{X: 1, Y: 2, Z: 1}, {X: 3, Y: 2, Z: 1}}
	if got := tools.SelectedTiles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("paste footprint expanded: %v", got)
	}
	e.TileCopySelected()
	if len(app.Clipboard().Buffer().Buffer) != 2 {
		t.Fatal("copy included mask hole")
	}
	before := resizeSnapshot(t, e)
	e.TileDeleteSelected()
	e.CommitOperation("Delete mask")
	holeAfter, err := mapadapter.CaptureTile(e.Dmm().GetTile(hole))
	if err != nil || !holeBefore.Equal(holeAfter) {
		t.Fatal("delete changed mask hole", err)
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	after := resizeSnapshot(t, e)
	if !reflect.DeepEqual(before.Tiles, after.Tiles) {
		t.Fatal("delete undo did not restore exact map")
	}
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	rotated := []util.Point{{X: 1, Y: 2, Z: 1}, {X: 1, Y: 4, Z: 1}}
	if !reflect.DeepEqual(tools.SelectedTiles(), rotated) {
		t.Fatal("rotation lost mask")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if !reflect.DeepEqual(tools.SelectedTiles(), want) {
		t.Fatal("undo lost sparse membership")
	}
	if len(app.errors) != 0 {
		t.Fatal(app.errors)
	}
}

func TestMaskHistoryRestoresMembershipWhenBoundsAreUnchanged(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	points := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 1, Y: 2, Z: 1}, {X: 2, Y: 1, Z: 1}}
	if err := grab.SelectMask(points); err != nil {
		t.Fatal(err)
	}
	before := grab.Bounds()
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	if grab.Bounds() != before {
		t.Fatal("fixture must retain bounds")
	}
	if reflect.DeepEqual(tools.SelectedTiles(), points) {
		t.Fatal("rotation did not change membership")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if !reflect.DeepEqual(tools.SelectedTiles(), points) {
		t.Fatal("same-bounds undo retained rotated mask")
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	if reflect.DeepEqual(tools.SelectedTiles(), points) {
		t.Fatal("same-bounds redo lost mask")
	}
}
