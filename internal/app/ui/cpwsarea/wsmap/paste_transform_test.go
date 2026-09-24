package wsmap

import (
	"context"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
)

func TestPasteTransformShortcutsBeforeConfirmation(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/area/foo")
	app.Clipboard().Copy(filter, e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	clipboard := app.Clipboard().Buffer().Buffer[0].Copy()
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	g := tools.Selected().(*tools.ToolGrab)
	displayBefore := e.Dmm().Copy()
	id := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2].StableID()
	hiddenID := e.Dmm().GetTile(util.Point{X: 2, Y: 3, Z: 1}).Instances()[0].StableID()
	pressSelectionShortcut(glfw.KeyRightBracket)
	settlePastePreview(t, ws, app)
	if !g.Placing() || g.Bounds() != (util.Bounds{X1: 2, Y1: 2, X2: 2, Y2: 3}) {
		t.Fatal("rotation shortcut did not rotate the floating template")
	}
	if !reflect.DeepEqual(displayBefore, e.Dmm().Copy()) {
		t.Fatal("floating rotation mutated committed data")
	}
	pressSelectionShortcut(glfw.KeyV)
	settlePastePreview(t, ws, app)
	if !reflect.DeepEqual(displayBefore, e.Dmm().Copy()) {
		t.Fatal("floating vertical mirror mutated committed data")
	}
	pressSelectionShortcut(glfw.KeyH)
	settlePastePreview(t, ws, app)
	if !reflect.DeepEqual(displayBefore, e.Dmm().Copy()) {
		t.Fatal("floating horizontal mirror mutated committed data")
	}
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) || !reflect.DeepEqual(clipboard, app.Clipboard().Buffer().Buffer[0].Copy()) {
		t.Fatal("preview transform created history or changed clipboard")
	}
	if e.Dmm().GetTile(util.Point{X: 2, Y: 3, Z: 1}).Instances()[0].StableID() != hiddenID {
		t.Fatal("transform replaced hidden destination identity")
	}
	pressSelectionShortcut(glfw.KeyEnter)
	settlePastePreview(t, ws, app)
	after := resizeSnapshot(t, e)
	if got := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2]; got.StableID() == id || got.Prefab().Vars().ValueV("dir", "") != "4" {
		t.Fatal("committed rotated/mirrored data or copied identity is wrong")
	}
	if g.Placing() || after.Revision != before.Revision+1 || !saveForTest(t, ws, app.jobs) {
		t.Fatal("transformed paste did not confirm as one saveable revision")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("transformed paste undo did not restore exact original hash")
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) {
		t.Fatal("transformed paste redo changed hash or identities")
	}
}

func TestPasteTransformNetworkOutcomes(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject", true: "accept"}[accept], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			before := document.Snapshot()
			app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
			ws.Map().CanvasState().SetMousePosition(32, 32, 1)
			e.TilePasteSelected()
			settlePastePreview(t, ws, app)
			pressSelectionShortcut(glfw.KeyRightBracket)
			settlePastePreview(t, ws, app)
			pressSelectionShortcut(glfw.KeyH)
			settlePastePreview(t, ws, app)
			select {
			case <-transport.sent:
				t.Fatal("floating transform submitted an operation")
			default:
			}
			if _, err := e.SaveSnapshot(context.Background()); err != nil {
				t.Fatal("isolated transformed preview blocked committed Save", err)
			}
			g := tools.Selected().(*tools.ToolGrab)
			if !g.ConfirmPlacement() {
				t.Fatal("transformed paste did not confirm")
			}
			op := transport.next(t)
			if len(op.Changes) != 2 {
				t.Fatalf("paste submitted %d changes", len(op.Changes))
			}
			for _, change := range op.Changes {
				if change.Coord.X != 2 || change.Coord.Y < 2 || change.Coord.Y > 3 {
					t.Fatal("operation included abandoned preview tiles")
				}
			}
			if accept {
				acceptSelection(t, network, document, op)
			} else {
				receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: op.OperationID, Code: "precondition_failed", Message: "forced transformed paste conflict", Revision: before.Revision, MapHash: resizeHash(t, before)})
			}
			runSelectionJob(t, app)
			settlePastePreview(t, ws, app)
			e.ProcessCollaborationUpdates()
			after := document.Snapshot()
			if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) || app.commands.HasUndoV(e.Dmm().Path.Absolute) != accept {
				t.Fatal("transformed paste outcome disagrees with authority/history")
			}
			if !accept && (!g.Placing() || !e.HasPastePlacement() || len(network.Conflicts()) != 1) {
				t.Fatal("rejection lost usable intent or conflict")
			}
			if accept {
				app.commands.UndoV(e.Dmm().Path.Absolute)
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				settlePastePreview(t, ws, app)
				if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
					t.Fatal("network undo did not restore exact map")
				}
				app.commands.RedoV(e.Dmm().Path.Absolute)
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				settlePastePreview(t, ws, app)
				if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) {
					t.Fatal("network redo changed pasted IDs or orientation")
				}
			}
		})
	}
}

func TestPasteTransformTextModifiersAndLevelCancellation(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := e.Dmm().Copy()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	preview := e.Dmm().Copy()
	for _, pair := range [][2]glfw.Key{{glfw.KeyLeftControl, glfw.KeyRightBracket}, {glfw.KeyRightAlt, glfw.KeyH}, {glfw.KeyLeftShift, glfw.KeyV}} {
		pressSelectionShortcut(pair[0], pair[1])
		settlePastePreview(t, ws, app)
		if !reflect.DeepEqual(preview, e.Dmm().Copy()) {
			t.Fatal("modified shortcut transformed preview")
		}
	}
	io := imgui.CurrentIO()
	input := "typing"
	keys := []glfw.Key{glfw.KeyLeftBracket, glfw.KeyRightBracket, glfw.KeyH, glfw.KeyV}
	for frame := 0; frame < len(keys)+2; frame++ {
		if frame >= 2 {
			io.KeyPress(int(keys[frame-2]))
		}
		imgui.NewFrame()
		imgui.Begin("Paste transform input")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame >= 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text fixture was not active")
			}
			shortcut.Process()
			if !reflect.DeepEqual(preview, e.Dmm().Copy()) {
				t.Fatal("text entry transformed preview")
			}
		}
		imgui.End()
		imgui.EndFrame()
		if frame >= 2 {
			io.KeyRelease(int(keys[frame-2]))
		}
	}
	imgui.NewFrame()
	imgui.EndFrame()
	pressSelectionShortcut(glfw.KeyRightBracket)
	settlePastePreview(t, ws, app)
	if tools.Selected().(*tools.ToolGrab).Bounds().Y2 != 3 {
		t.Fatal("rotation did not resume after text input")
	}
	ws.Map().SetActiveLevel(2)
	e.ProcessCollaborationUpdates()
	pressSelectionShortcut(glfw.KeyH)
	settlePastePreview(t, ws, app)
	if e.HasPastePlacement() || !samePasteDisplay(t, before, e.Dmm()) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("level switch left transformed preview or stale shortcut mutation")
	}
}

func TestPasteTransformRepairsInvalidTargetAndCancels(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := e.Dmm().Copy()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(3*32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	g := tools.Selected().(*tools.ToolGrab)
	if g.PlacementError() == nil {
		t.Fatal("fixture must initially be out of bounds")
	}
	pressSelectionShortcut(glfw.KeyLeftBracket)
	settlePastePreview(t, ws, app)
	if g.PlacementError() != nil || g.Bounds() != (util.Bounds{X1: 4, Y1: 1, X2: 4, Y2: 2}) {
		t.Fatal("rotation did not fit previously invalid target")
	}
	preview := e.Dmm().Copy()
	pressSelectionShortcut(glfw.KeyLeftBracket)
	settlePastePreview(t, ws, app)
	if g.PlacementError() == nil || g.ConfirmPlacement() || !reflect.DeepEqual(preview, e.Dmm().Copy()) {
		t.Fatal("invalid rotation changed or confirmed the displayed preview")
	}
	pressSelectionShortcut(glfw.KeyEscape)
	settlePastePreview(t, ws, app)
	if g.Placing() || !samePasteDisplay(t, before, e.Dmm()) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("cancelled transformed preview changed map/history")
	}
}

func TestPasteTransformNeverCapturesOrRepairsDestinationDisplay(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	before := document.Snapshot()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	invalid := e.Dmm().GetTile(util.Point{X: 2, Y: 3, Z: 1}).Instances()[2]
	originalID := invalid.StableID()
	invalid.SetStableID("invalid-transform-destination")
	display := e.Dmm().Copy()
	pressSelectionShortcut(glfw.KeyRightBracket)
	settlePastePreview(t, ws, app)
	g := tools.Selected().(*tools.ToolGrab)
	if g.PlacementError() != nil || !reflect.DeepEqual(display, e.Dmm().Copy()) {
		t.Fatal("isolated transform captured or rewrote destination display")
	}
	invalid.SetStableID(originalID)
	display = e.Dmm().Copy()
	pressSelectionShortcut(glfw.KeyH)
	settlePastePreview(t, ws, app)
	if g.PlacementError() != nil || !reflect.DeepEqual(display, e.Dmm().Copy()) {
		t.Fatal("isolated mirror captured or rewrote destination display")
	}
	pressSelectionShortcut(glfw.KeyEscape)
	settlePastePreview(t, ws, app)
	if snapshot, err := e.SaveSnapshot(context.Background()); err != nil || resizeHash(t, snapshot) != resizeHash(t, before) {
		t.Fatal("presentation contaminated committed save authority", err)
	}
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("fault created history")
	}
	select {
	case <-transport.sent:
		t.Fatal("fault submitted durable data")
	default:
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatalf("transform retained captures blocking recovery: %v", err)
	}
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) || !saveForTest(t, ws, app.jobs) {
		t.Fatal("validated recovery failed exact original save")
	}
}
