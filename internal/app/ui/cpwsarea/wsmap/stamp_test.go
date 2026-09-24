package wsmap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestStampNetworkAcceptanceAndRejection(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject", true: "accept"}[accepted], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			g := activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			before := document.Snapshot()
			stamp, err := e.CaptureStamp("User stamp", []util.Point{{X: 1, Y: 1, Z: 1}})
			if err != nil {
				t.Fatal(err)
			}
			ws.Map().CanvasState().SetMousePosition(32, 32, 1)
			if err := e.StartStamp(stamp, false); err != nil {
				t.Fatal(err)
			}
			settlePastePreview(t, ws, app)
			select {
			case <-transport.sent:
				t.Fatal("stamp preview submitted before confirmation")
			default:
			}
			if !g.ConfirmPlacement() {
				t.Fatal("stamp did not confirm")
			}
			op := transport.next(t)
			if _, err := e.CaptureStamp("Pending", []util.Point{{X: 2, Y: 2, Z: 1}}); err == nil {
				t.Fatal("captured unacknowledged stamp contents")
			}
			if accepted {
				acceptSelection(t, network, document, op)
			} else {
				receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: op.OperationID, Code: "precondition_failed", Message: "forced stamp rejection", Revision: before.Revision, MapHash: resizeHash(t, before)})
			}
			runSelectionJob(t, app)
			settlePastePreview(t, ws, app)
			e.ProcessCollaborationUpdates()
			if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, document.Snapshot()) || app.commands.HasUndoV(e.Dmm().Path.Absolute) != accepted {
				t.Fatal("stamp result diverged from authority or history")
			}
			if !accepted {
				if len(network.Conflicts()) != 1 || resizeHash(t, document.Snapshot()) != resizeHash(t, before) {
					t.Fatal("rejected stamp lost retained draft or changed authority")
				}
				return
			}
			after := document.Snapshot()
			app.commands.UndoV(e.Dmm().Path.Absolute)
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			settlePastePreview(t, ws, app)
			if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
				t.Fatal("network stamp undo changed original contents")
			}
			app.commands.RedoV(e.Dmm().Path.Absolute)
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			settlePastePreview(t, ws, app)
			if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, after) {
				t.Fatal("network stamp redo changed copied identities")
			}
		})
	}
}

func TestStampRoundTripPreviewAndHistory(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	g := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	coords := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}}
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), coords[:1])
	clipboard := app.Clipboard().Buffer().Buffer[0].Copy()
	// Unknown mechanical values must survive the file and placement paths.
	i := e.Dmm().Tiles[0].Instances()[2]
	e.InstanceReplace(i, dmmprefab.New(0, "/obj/unknown", dmvars.Set(i.Prefab().Vars(), "custom", `"kept value"`)))
	e.CommitOperation("fixture unknown value")
	app.PathsFilter().TogglePath("/area/foo")
	stamp, err := e.CaptureStamp("User selection", coords)
	if err != nil {
		t.Fatal(err)
	}
	before := resizeSnapshot(t, e)
	beforeDisplay := e.Dmm().Copy()
	sourceID := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2].StableID()
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err := stamp.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), i.StableID()) || strings.Contains(string(data), e.Dmm().Path.Absolute) {
		t.Fatal("stamp persisted instance identity or source path", err)
	}
	loaded, err := stamps.Load(path)
	if err != nil || loaded.Name() != "User selection" {
		t.Fatal("saved stamp did not reload", err)
	}
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	point := util.Point{X: 2, Y: 2, Z: 1}
	hidden := e.Dmm().GetTile(point).Instances()[0].StableID()
	destinationID := e.Dmm().GetTile(point).Instances()[2].StableID()
	if err := e.StartStamp(loaded, false); err != nil {
		t.Fatal(err)
	}
	settlePastePreview(t, ws, app)
	if !g.Placing() {
		t.Fatal("stamp did not start a floating preview")
	}
	if _, err := e.SaveSnapshot(context.Background()); err != nil {
		t.Fatal("read-only stamp preview blocked committed Save snapshot", err)
	}
	if !reflect.DeepEqual(beforeDisplay, e.Dmm().Copy()) || resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("stamp preview mutated committed map state")
	}
	pressSelectionShortcut(glfw.KeyRightBracket)
	settlePastePreview(t, ws, app)
	if !reflect.DeepEqual(beforeDisplay, e.Dmm().Copy()) || resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("stamp transform mutated committed map state")
	}
	pressSelectionShortcut(glfw.KeyEscape)
	settlePastePreview(t, ws, app)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("stamp cancellation failed to restore map")
	}
	if err := e.StartStamp(loaded, false); err != nil {
		t.Fatal(err)
	}
	settlePastePreview(t, ws, app)
	if !reflect.DeepEqual(beforeDisplay, e.Dmm().Copy()) {
		t.Fatal("second stamp preview mutated committed display data")
	}
	if !g.ConfirmPlacement() {
		t.Fatal("stamp preview did not confirm")
	}
	settlePastePreview(t, ws, app)
	after := resizeSnapshot(t, e)
	if after.Revision != before.Revision+1 || e.Dmm().GetTile(point).Instances()[0].StableID() != hidden {
		t.Fatal("stamp changed hidden layer or committed more than once")
	}
	pasted := e.Dmm().GetTile(point).Instances()[2]
	committedID := pasted.StableID()
	if committedID == "" || committedID == sourceID || committedID == destinationID {
		t.Fatal("stamp commit did not assign a fresh placement identity")
	}
	if pasted.Prefab().Path() != "/obj/unknown" || pasted.Prefab().Vars().ValueV("custom", "") != `"kept value"` {
		t.Fatal("stamp lost unknown type or variable")
	}
	mapPath := e.Dmm().Path.Absolute
	afterHash := resizeHash(t, after)
	app.commands.UndoV(mapPath)
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) || !app.commands.HasRedoV(mapPath) {
		t.Fatal("stamp undo did not restore the exact original map")
	}
	app.commands.RedoV(mapPath)
	redone := e.Dmm().GetTile(point).Instances()[2]
	if resizeHash(t, resizeSnapshot(t, e)) != afterHash || redone.StableID() != committedID {
		t.Fatal("stamp redo lost copied identities")
	}
	if !reflect.DeepEqual(clipboard, app.Clipboard().Buffer().Buffer[0].Copy()) {
		t.Fatal("stamp workflow altered clipboard")
	}
	if !saveForTest(t, ws, app.jobs) {
		t.Fatal("confirmed stamp could not be saved")
	}
}

func TestStampRejectsBusyOrDifferentEnvironmentAndPreservesHiddenDestination(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	stamp, err := stamps.Capture("Selection", strings.Repeat("f", 64), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}}, app.PathsFilter())
	if err != nil {
		t.Fatal(err)
	}
	before := e.Dmm().Copy()
	if err := e.StartStamp(stamp, false); err == nil || !reflect.DeepEqual(e.Dmm(), &before) {
		t.Fatal("environment mismatch changed display without acknowledgement")
	}
	app.PathsFilter().TogglePath("/obj/foo")
	ws.Map().CanvasState().SetMousePosition(32, 32, 1)
	point := util.Point{X: 2, Y: 2, Z: 1}
	if err := e.StartStamp(stamp, true); err != nil {
		t.Fatal(err)
	}
	settlePastePreview(t, ws, app)
	if !samePasteDisplay(t, before, e.Dmm()) {
		t.Fatal("active stamp preview changed the hidden destination or committed display")
	}
	if err := e.StartStamp(stamp, true); err == nil {
		t.Fatal("stamp replaced an active preview")
	}
	if _, err := e.CaptureStamp("Preview", []util.Point{point}); err == nil {
		t.Fatal("capture accepted an uncommitted preview")
	}
	tools.Selected().(*tools.ToolGrab).CancelPlacement()
	settlePastePreview(t, ws, app)
	if !samePasteDisplay(t, before, e.Dmm()) {
		t.Fatal("cancelled stamp changed original display")
	}
	e.BeginTileChange(point)
	if err := e.StartStamp(stamp, true); err == nil {
		t.Fatal("stamp replaced a pending edit")
	}
}
