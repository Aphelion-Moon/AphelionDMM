package wsmap

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// Drive the same UI-owned work queue and bounded preview batches as a pane
// frame. Workers never mutate OpenGL or the display map from this helper.
func settlePastePreview(t *testing.T, ws *WsMap, app *selectionTestApp, target ...util.Point) {
	t.Helper()
	e := ws.Map().Editor()
	point := ws.Map().CanvasState().HoveredTile()
	if len(target) != 0 {
		point = target[0]
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case job := <-app.jobs:
			job()
		default:
		}
		e.ProcessPasteWork()
		if grab, ok := tools.Selected().(*tools.ToolGrab); ok && grab.Placing() {
			grab.UpdatePlacement(point)
		}
		if e.PastePlacementProgress() == "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("paste work did not settle: %s", e.PastePlacementProgress())
}

// Rollback projects authoritative states into new renderer instances. Compare
// every ordered prefab, raw variable and durable StableID, plus map metadata;
// ephemeral renderer instance IDs and prefab cache IDs may be regenerated.
func samePasteDisplay(t *testing.T, expected dmmap.Dmm, actual *dmmap.Dmm) bool {
	t.Helper()
	wantMetadata, gotMetadata := expected, *actual
	wantMetadata.Tiles, gotMetadata.Tiles = nil, nil
	if !reflect.DeepEqual(wantMetadata, gotMetadata) || len(expected.Tiles) != len(actual.Tiles) {
		return false
	}
	for i, tile := range expected.Tiles {
		if tile.Coord != actual.Tiles[i].Coord {
			return false
		}
		want, wantErr := mapadapter.CaptureTile(tile)
		got, gotErr := mapadapter.CaptureTile(actual.Tiles[i])
		if wantErr != nil || gotErr != nil || !want.Equal(got) {
			t.Logf("display differs at %+v: expected=%+v actual=%+v errors=%v / %v", tile.Coord, want, got, wantErr, gotErr)
			return false
		}
	}
	return true
}

func TestPastePreviewKeyboardConfirmationAndTextInput(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	g := tools.Selected().(*tools.ToolGrab)
	pressSelectionShortcut(glfw.KeyLeftControl, glfw.KeyEnter)
	settlePastePreview(t, ws, app)
	if !g.Placing() {
		t.Fatal("modified Enter confirmed paste")
	}
	io := imgui.CurrentIO()
	input := "text"
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyEnter))
		}
		imgui.NewFrame()
		imgui.Begin("Paste input verification")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text fixture not active")
			}
			shortcut.Process()
			if !g.Placing() {
				t.Fatal("text Enter placed paste")
			}
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyEnter))
	imgui.NewFrame()
	imgui.EndFrame()
	// The text window is no longer submitted; the next key belongs to the map.
	pressSelectionShortcut(glfw.KeyEnter)
	settlePastePreview(t, ws, app)
	if g.Placing() || resizeSnapshot(t, e).Revision != 1 {
		t.Fatal("bare Enter did not confirm paste through registry")
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("confirmed paste failed real workspace Save")
	}
}

func TestPastePreviewInvalidStartLifecycleGuards(t *testing.T) {
	for _, end := range []string{"escape", "tool", "tab", "level", "close"} {
		t.Run(end, func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			activateSelectionWorkspace(t, ws)
			e := ws.Map().Editor()
			before := e.Dmm().Copy()
			app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
			ws.Map().CanvasState().SetMousePosition(3*32, 0, 1)
			e.TilePasteSelected()
			settlePastePreview(t, ws, app)
			g := tools.Selected().(*tools.ToolGrab)
			if g.ConfirmPlacement() || !e.HasPastePlacement() {
				t.Fatal("invalid paste ended or confirmed")
			}
			if _, err := e.SaveSnapshot(context.Background()); err != nil {
				t.Fatal("isolated preview blocked committed Save", err)
			}
			if err := e.RefreshCollaborationSnapshot(context.Background()); err != nil {
				t.Fatal("isolated preview blocked committed refresh", err)
			}
			if err := e.DetachCollaborationExecutor(context.Background()); err == nil {
				t.Fatal("empty-journal preview allowed detach")
			}
			switch end {
			case "escape":
				ws.Map().DoDeselect()
			case "tool":
				tools.SetSelected(tools.TNAdd)
			case "tab":
				ws.Map().OnDeactivate()
			case "level":
				ws.Map().SetActiveLevel(2)
				e.ProcessCollaborationUpdates()
			case "close":
				e.Close()
			}
			settlePastePreview(t, ws, app)
			if e.HasPastePlacement() || !samePasteDisplay(t, before, e.Dmm()) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
				t.Fatal("lifecycle left placement ownership or changed map/history")
			}
		})
	}
}

func TestPastePreviewOtherCommandsCannotChangeTemplate(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	before := e.Dmm().Copy()
	e.InstanceDelete(e.Dmm().Tiles[1].Instances()[2])
	e.TileCutSelected()
	e.TileDelete(util.Point{X: 2, Y: 1, Z: 1})
	e.CommitOperation("unrelated command")
	if !reflect.DeepEqual(e.Dmm().Copy(), before) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("another command changed or committed unfinished paste")
	}
	tools.Selected().(*tools.ToolGrab).CancelPlacement()
	settlePastePreview(t, ws, app)
}

func TestPastePreviewNetworkOutcomes(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject", true: "accept"}[accept], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			before := document.Snapshot()
			app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
			ws.Map().CanvasState().SetMousePosition(32, 0, 1)
			e.TilePasteSelected()
			settlePastePreview(t, ws, app)
			select {
			case <-transport.sent:
				t.Fatal("preview sent a durable operation")
			default:
			}
			g := tools.Selected().(*tools.ToolGrab)
			if !g.ConfirmPlacement() {
				t.Fatal("paste did not confirm")
			}
			op := transport.next(t)
			if len(op.Changes) != 1 || op.Changes[0].Coord.X != 2 {
				t.Fatal("paste did not submit exact destination")
			}
			g.CancelPlacement()
			ws.Map().DoDeselect()
			if !g.Placing() || !e.PastePlacementPending() {
				t.Fatal("Cancel hid a submitted operation before its durable outcome")
			}
			if accept {
				acceptSelection(t, network, document, op)
			} else {
				receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: op.OperationID, Code: "precondition_failed", Message: "forced paste conflict", Revision: before.Revision, MapHash: resizeHash(t, before)})
			}
			runSelectionJob(t, app)
			settlePastePreview(t, ws, app)
			e.ProcessCollaborationUpdates()
			if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, document.Snapshot()) {
				t.Fatal("paste outcome diverged from authority")
			}
			if app.commands.HasUndoV(e.Dmm().Path.Absolute) != accept {
				t.Fatal("wrong paste history outcome")
			}
			if !accept && (!g.Placing() || !e.HasPastePlacement() || len(network.Conflicts()) != 1) {
				t.Fatal("rejected paste lost usable intent or conflict")
			}
			if accept {
				app.commands.UndoV(e.Dmm().Path.Absolute)
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				settlePastePreview(t, ws, app)
				if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
					t.Fatal("network paste undo did not restore original hash")
				}
			}
		})
	}
}

func TestPastePreviewConfirmationAndCancellation(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	g := tools.Selected().(*tools.ToolGrab)
	if !g.Placing() || g.Stale() {
		t.Fatal("paste did not start placement")
	}
	if _, err := e.SaveSnapshot(context.Background()); err != nil || e.CanChangeMapSize() {
		t.Fatal("isolated placement must allow committed Save while retaining resize ownership", err)
	}
	id := e.Dmm().Tiles[1].Instances()[2].StableID()
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	if e.Dmm().Tiles[1].Instances()[2].StableID() != id {
		t.Fatal("repeated Paste replaced template")
	}
	g.CancelPlacement()
	settlePastePreview(t, ws, app)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("cancel changed authority/history")
	}
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	g.UpdatePlacement(util.Point{X: 3, Y: 1, Z: 1})
	settlePastePreview(t, ws, app, util.Point{X: 3, Y: 1, Z: 1})
	if !g.ConfirmPlacement() {
		t.Fatal("valid placement did not confirm")
	}
	settlePastePreview(t, ws, app)
	id = e.Dmm().Tiles[2].Instances()[2].StableID()
	after := resizeSnapshot(t, e)
	if after.Revision != before.Revision+1 || id == string(before.Tiles[0].State.Prefabs[2].StableID) {
		t.Fatal("paste did not create one distinct operation")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("paste undo changed original hash")
	}
	app.commands.RedoV(e.Dmm().Path.Absolute)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) || e.Dmm().Tiles[2].Instances()[2].StableID() != id {
		t.Fatal("paste redo changed pasted identity/hash")
	}
}

func TestPastePreviewPreservesHiddenDestinationIdentity(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/area/foo")
	app.Clipboard().Copy(filter, e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	destination := util.Point{X: 2, Y: 1, Z: 1}
	hiddenID := e.Dmm().GetTile(destination).Instances()[0].StableID()
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	if got := e.Dmm().GetTile(destination).Instances()[0].StableID(); got != hiddenID {
		t.Fatal("paste replaced the hidden destination instance identity")
	}
}

func TestPastePreviewDoesNotClipAtMapEdge(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	before := e.Dmm().Copy()
	ws.Map().CanvasState().SetMousePosition(3*32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	if !reflect.DeepEqual(e.Dmm().Copy(), before) {
		t.Fatal("paste clipped the template and changed only its in-bounds portion")
	}
}
