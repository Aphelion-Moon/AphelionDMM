package wsmap

import (
	"context"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
)

func TestRepeatTransformKeepsActionAfterNoopAndRespectsInput(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	g := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	// Normalize the fixture's explicit inherited direction before the no-op.
	pressSelectionShortcut(glfw.KeyH)
	pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyRight)
	before := resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyH) // A south-facing single column does not change.
	if resizeSnapshot(t, e).Revision != before.Revision {
		t.Fatal("mirror fixture was not a no-op")
	}
	pressSelectionShortcut(glfw.KeyLeftControl, glfw.KeyF4)
	shortcut.SetModalOpen(true)
	pressSelectionShortcut(glfw.KeyF4)
	shortcut.SetModalOpen(false)
	input := "value"
	io := imgui.CurrentIO()
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyF4))
		}
		imgui.NewFrame()
		imgui.Begin("Repeat input verification")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("input fixture was not focused")
			}
			shortcut.Process()
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyF4))
	if !reflect.DeepEqual(resizeSnapshot(t, e), before) {
		t.Fatal("repeat stole modified, modal or text input")
	}
	// Release the text widget's active ID before testing normal shortcut input.
	imgui.NewFrame()
	imgui.EndFrame()
	pressSelectionShortcut(glfw.KeyF4)
	if g.Bounds() != (util.Bounds{X1: 3, Y1: 1, X2: 3, Y2: 1}) {
		t.Fatal("no-op mirror replaced the move recipe", g.Bounds())
	}
}

func TestRepeatTransformMirrorsCurrentContents(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	pressSelectionShortcut(glfw.KeyRightBracket) // West, so horizontal reflection changes direction.
	before := resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyH)
	pressSelectionShortcut(glfw.KeyF4)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("repeated horizontal mirror did not restore exact contents")
	}
	pressSelectionShortcut(glfw.KeyRightBracket) // North, so vertical reflection changes direction.
	before = resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyV)
	pressSelectionShortcut(glfw.KeyF4)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("repeated vertical mirror did not restore exact contents")
	}
}

func TestRepeatTransformUsesCurrentSelectionAndHistory(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	g := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	initial := resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyF4)
	if resizeSnapshot(t, e).Revision != initial.Revision {
		t.Fatal("repeat without a transform changed authority")
	}
	pressSelectionShortcut(glfw.KeyRightBracket)
	pressSelectionShortcut(glfw.KeyF4)
	if resizeSnapshot(t, e).Revision != initial.Revision+2 {
		t.Fatal("F4 did not repeat the rotation")
	}
	first := e.Dmm().Tiles[0].Instances()[2]
	if first.Prefab().Vars().ValueV("dir", "") != "1" {
		t.Fatal("repeat did not rotate current contents")
	}
	g.Reset()
	g.SelectArea([]util.Point{{X: 2, Y: 2, Z: 1}})
	secondID := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2].StableID()
	before := resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyF4)
	after := resizeSnapshot(t, e)
	second := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2]
	if second.StableID() != secondID || second.Prefab().Vars().ValueV("dir", "") != "8" || first.Prefab().Vars().ValueV("dir", "") != "1" {
		t.Fatal("repeat reused old selection or contents")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("repeat undo did not restore exact map")
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) {
		t.Fatal("repeat redo changed identities or values")
	}
	resumed := resizeSnapshot(t, e) // Undo and redo each advance the durable revision.
	if err := e.DetachCollaborationExecutor(context.Background()); err != nil {
		t.Fatal(err)
	}
	pressSelectionShortcut(glfw.KeyF4)
	if !reflect.DeepEqual(resizeSnapshot(t, e), resumed) {
		t.Fatal("repeat leaked across attachment change")
	}
}

func TestRepeatTransformKeepsMoveDistanceAndCustomBinding(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	app.selectionStep = 2
	g := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyRight)
	app.selectionStep = 1
	g.Reset()
	g.SelectArea([]util.Point{{X: 1, Y: 2, Z: 1}})
	if err := shortcut.SetBindings("pmap#repeatTransform", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shortcut.ResetBindings("pmap#repeatTransform") })
	pressSelectionShortcut(glfw.KeyF4)
	if g.Bounds().X1 != 1 {
		t.Fatal("custom repeat retained default binding")
	}
	pressSelectionShortcut(glfw.KeyF8)
	if g.Bounds() != (util.Bounds{X1: 3, Y1: 2, X2: 3, Y2: 2}) {
		t.Fatal("repeat changed recorded move distance", g.Bounds())
	}
	after := resizeSnapshot(t, e)
	pressSelectionShortcut(glfw.KeyF8)
	if resizeSnapshot(t, e).Revision != after.Revision {
		t.Fatal("out-of-bounds repeat partially committed")
	}
}

func TestRepeatTransformRotatesFloatingPasteWithoutCommitting(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	g := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	clipboard := app.Clipboard().Buffer().Buffer[0].Copy()
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	e.TilePasteSelected()
	id := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2].StableID()
	pressSelectionShortcut(glfw.KeyRightBracket)
	pressSelectionShortcut(glfw.KeyF4)
	if !g.Placing() || g.Bounds() != (util.Bounds{X1: 2, Y1: 2, X2: 3, Y2: 2}) {
		t.Fatal("repeat failed to transform floating paste", g.Bounds())
	}
	rotated := e.Dmm().GetTile(util.Point{X: 3, Y: 2, Z: 1}).Instances()[2]
	if rotated.StableID() != id || rotated.Prefab().Vars().ValueV("dir", "") != "1" {
		t.Fatal("repeat changed copied identity or lost orientation")
	}
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) || !reflect.DeepEqual(clipboard, app.Clipboard().Buffer().Buffer[0].Copy()) {
		t.Fatal("repeat committed preview or altered clipboard")
	}
	pressSelectionShortcut(glfw.KeyEnter)
	if resizeSnapshot(t, e).Revision != before.Revision+1 {
		t.Fatal("paste confirmation did not make exactly one operation")
	}
}

func TestRepeatTransformIgnoresRejectedNetworkAction(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	pressSelectionShortcut(glfw.KeyRightBracket)
	first := transport.next(t)
	pressSelectionShortcut(glfw.KeyF4)
	select {
	case <-transport.sent:
		t.Fatal("repeat remembered an unacknowledged first action")
	default:
	}
	acceptSelection(t, network, document, first)
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	pressSelectionShortcut(glfw.KeyH)
	rejected := transport.next(t)
	before := document.Snapshot()
	receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: rejected.OperationID, Code: "precondition_failed", Message: "forced repeat rejection", Revision: before.Revision, MapHash: resizeHash(t, before)})
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	pressSelectionShortcut(glfw.KeyF4)
	repeated := transport.next(t)
	acceptSelection(t, network, document, repeated)
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if e.Dmm().Tiles[0].Instances()[2].Prefab().Vars().ValueV("dir", "") != "1" {
		t.Fatal("rejected mirror replaced accepted repeat action")
	}
}
