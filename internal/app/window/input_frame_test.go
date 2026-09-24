package window_test

import (
	"context"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
	"testing"
)

func TestCurrentFrameErasePressAndRelease(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	tools.SetSelected(tools.TNDelete)
	frame(false, 1, 1)
	frame(false, 1, 1)
	frame(true, 1, 1)
	if len(e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()) != 2 {
		t.Fatal("press used previous-frame button or pick")
	}
	if app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("stroke committed before release")
	}
	frame(false, 2, 1)
	if !app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("release was delayed despite consumed samples")
	}
	snapshot, err := e.SaveSnapshot(context.Background())
	if err != nil || snapshot.Revision != 1 {
		t.Fatal("release did not publish one coherent revision", err)
	}
	if len(e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()) != 2 {
		t.Fatal("release omitted its final pointer position")
	}
}
