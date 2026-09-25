package wsmap

import (
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestFloatingPasteRefreshesFilterBeforeConfirmation(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	target := util.Point{X: 2, Y: 1, Z: 1}
	before := e.Dmm().GetTile(target).Instances()[2].StableID()
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	app.PathsFilter().TogglePath("/obj/foo")
	if e.ConfirmPastePlacement() {
		t.Fatal("stale policy accepted a confirmation")
	}
	settlePastePreview(t, ws, app)
	if !tools.Selected().(*tools.ToolGrab).ConfirmPlacement() {
		t.Fatal("refreshed preview cannot be confirmed")
	}
	settlePastePreview(t, ws, app)
	instances := e.Dmm().GetTile(target).Instances()
	objects := 0
	for _, instance := range instances {
		if instance.Prefab().Path() == "/obj/foo" {
			objects++
			if instance.StableID() != before {
				t.Fatal("filter change replaced hidden identity")
			}
		}
	}
	if objects != 1 {
		t.Fatalf("filter change wrote hidden instances: count=%d", objects)
	}
}
