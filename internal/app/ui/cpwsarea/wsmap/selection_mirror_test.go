package wsmap

import (
	"context"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/util"
)

func TestSelectionMirrorDoesNotTakeTextFieldInput(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 2, Z: 1}})
	input := "text"
	io := imgui.CurrentIO()
	for frame := 0; frame < 4; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyH))
		}
		if frame == 3 {
			io.KeyRelease(int(glfw.KeyH))
			io.KeyPress(int(glfw.KeyV))
		}
		imgui.NewFrame()
		imgui.Begin("Mirror input verification")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame >= 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text-field fixture is not active")
			}
			shortcut.Process()
			if app.commands.HasUndoV(ws.Map().Editor().Dmm().Path.Absolute) {
				t.Fatal("mirror shortcut took text-field input")
			}
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyV))
}

func TestSelectionNoopMirrorDoesNotHideNudgeUndo(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	// The fixture explicitly overrides an inherited south direction. Normalize
	// that representation first, then start a fresh selection for the no-op case.
	if err := grab.Mirror(editing.MirrorHorizontal, e.MirrorSelection); err != nil {
		t.Fatal(err)
	}
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
	origin := grab.Bounds()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, _ := initial.Hash()
	if err := grab.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	settleSelectionMove(t, ws, app)
	nudged, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// One column, south-facing instances and no X offsets: H changes nothing.
	if err := grab.Mirror(editing.MirrorHorizontal, e.MirrorSelection); err != nil {
		t.Fatal(err)
	}
	unchanged, err := e.SaveSnapshot(context.Background())
	if err != nil || unchanged.Revision != nudged.Revision {
		t.Fatal("fixture mirror was not a no-op")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	restored, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ := restored.Hash()
	if gotHash != wantHash || grab.Bounds() != origin {
		t.Fatal("no-op mirror prevented exact map and selection restoration on undo")
	}
}

func TestSelectionMirrorWorkspaceShortcutsAndUndo(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 2, Z: 1}})
	e := ws.Map().Editor()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, _ := initial.Hash()
	area, id := grab.Bounds(), e.Dmm().Tiles[0].Instances()[2].StableID()
	pressSelectionShortcut(glfw.KeyLeftControl, glfw.KeyH)
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("modified H invoked a bare mirror shortcut")
	}
	pressSelectionShortcut(glfw.KeyH)
	if e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()[2].StableID() != id {
		t.Fatal("H did not mirror the selected content left/right")
	}
	pressSelectionShortcut(glfw.KeyV)
	instance := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2]
	if instance.StableID() != id || instance.Prefab().Vars().ValueV("dir", "") != "1" || grab.Bounds() != area {
		t.Fatal("V did not reflect top/bottom, direction and stable selection bounds")
	}
	transformed, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	transformedHash, _ := transformed.Hash()
	app.commands.UndoV(e.Dmm().Path.Absolute)
	app.commands.UndoV(e.Dmm().Path.Absolute)
	restored, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ := restored.Hash()
	if gotHash != wantHash || grab.Bounds() != area {
		t.Fatal("mirror undo did not restore the exact initial map")
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	app.commands.RedoV(e.Dmm().Path.Absolute)
	redone, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ = redone.Hash()
	if gotHash != transformedHash {
		t.Fatal("mirror redo did not restore the accepted map")
	}
	for key, action := range map[string]string{"H": "Mirror selection horizontally", "V": "Mirror selection vertically"} {
		found := false
		for _, entry := range shortcut.Reference() {
			if entry.Keys == key && entry.Action == action {
				found = true
			}
		}
		if !found {
			t.Fatalf("live shortcut reference is missing %s", action)
		}
	}
	ws.Map().SetActiveLevel(2)
	pressSelectionShortcut(glfw.KeyH)
	blocked, err := e.SaveSnapshot(context.Background())
	if err != nil || blocked.Revision != redone.Revision {
		t.Fatal("mirror acted on a non-visible selection level")
	}
}

func TestSelectionMirrorNetworkAcknowledgementAndRejection(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 2, Z: 1}})
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	area := grab.Bounds()
	if err := grab.Mirror(editing.MirrorHorizontal, e.MirrorSelection); err != nil {
		t.Fatal(err)
	}
	operation := transport.next(t)
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("unacknowledged mirror entered undo")
	}
	acceptSelection(t, network, document, operation)
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	acceptedHash, _ := document.Snapshot().Hash()
	if err := grab.Mirror(editing.MirrorVertical, e.MirrorSelection); err != nil {
		t.Fatal(err)
	}
	rejected := transport.next(t)
	receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: rejected.OperationID, Code: "precondition_failed", Message: "controlled mirror conflict", Revision: 1, MapHash: acceptedHash})
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	snapshot, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, _ := snapshot.Hash()
	if gotHash != acceptedHash || grab.Bounds() != area || len(app.errors) != 1 {
		t.Fatal("rejected mirror changed accepted contents or selection geometry")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("rejected mirror left a second history entry")
	}
}
