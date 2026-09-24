package wsmap

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSelectionCaptureFailureCancelsAndRecoversWorkspace(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprintf("explicit_cancel=%v", cancel), func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			e := ws.Map().Editor()
			before := resizeSnapshot(t, e)
			document, err := engine.NewDocument(before)
			if err != nil {
				t.Fatal(err)
			}
			actor, err := model.NewActorID()
			if err != nil {
				t.Fatal(err)
			}
			authority, err := executor.NewLocal(document, actor)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.AttachCollaborationExecutor(authority); err != nil {
				t.Fatal(err)
			}
			move, err := e.BeginSelectionMove(util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 1}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.PreviewSelectionMove(move, util.Point{Y: 1}); err != nil {
				t.Fatal(err)
			}
			invalid := e.Dmm().GetTile(util.Point{X: 4, Y: 3, Z: 1}).Instances()[2]
			originalID := invalid.StableID()
			invalid.SetStableID("invalid-later-destination")
			previous := e.Dmm().Copy()
			if _, err := e.PreviewSelectionMove(move, util.Point{X: 2, Y: 2}); err == nil {
				t.Fatal("invalid destination was accepted")
			}
			if !reflect.DeepEqual(e.Dmm(), &previous) {
				t.Fatal("failed destination changed a valid earlier preview")
			}
			invalid.SetStableID(originalID) // Repair injected fault; the guard must remain.
			e.FinishSelectionMove(move, cancel)
			if _, err := e.SaveSnapshot(context.Background()); err == nil {
				t.Fatal("capture fault allowed Save before validated replacement")
			}
			display, err := mapadapter.Import(e.Dmm(), before.DocumentID, before.EnvironmentHash)
			if err != nil || !reflect.DeepEqual(display.Tiles, before.Tiles) {
				t.Error("faulted selection release did not restore the original display")
			}
			if err := e.AttachCollaborationExecutor(authority); err != nil {
				t.Fatalf("cancelled failed drag left captures blocking recovery: %v", err)
			}
			if got := resizeSnapshot(t, e); !reflect.DeepEqual(got, before) || !saveForTest(t, ws, app.jobs) {
				t.Fatal("validated recovery changed authority or failed actual Save")
			}
		})
	}
}

func TestSelectionCancelKeepsUnrelatedEditPending(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	move, err := e.BeginSelectionMove(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	instance := e.Dmm().GetTile(util.Point{X: 4, Y: 4, Z: 1}).Instances()[2]
	prefab := instance.Prefab()
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, prefab.Path(), dmvars.Set(prefab.Vars(), "dir", "4")))
	e.FinishSelectionMove(move, true)
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("cancelling the drag committed or discarded an unrelated pending edit")
	}
	if app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("cancellation created an undo command for unrelated work")
	}
	e.CommitOperation("Unrelated direction change")
	if got := resizeSnapshot(t, e); got.Revision != 1 {
		t.Fatal("unrelated edit could not be committed independently")
	}
	app.commands.UndoV(ws.CommandStackId())
	if got := resizeSnapshot(t, e); resizeHash(t, got) != resizeHash(t, before) {
		t.Fatal("independent edit undo failed to restore the exact original map")
	}
}

func TestSelectionCaptureFailureKeepsPendingNetworkAcceptance(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 3, Z: 1}})
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	pending := transport.next(t)
	e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2].SetStableID("invalid-later-capture")
	if _, err := e.MirrorSelection(util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 2}, 1, editing.MirrorHorizontal); err == nil {
		t.Fatal("invalid mirror capture was accepted")
	}
	if !network.HasUnacknowledgedOperations() || app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("failed capture lost or prematurely accepted the earlier operation")
	}
	acceptSelection(t, network, document, pending)
	runSelectionJob(t, app)
	if !app.commands.HasUndoV(ws.CommandStackId()) || network.HasUnacknowledgedOperations() {
		t.Fatal("failed capture invalidated the real acknowledgement/history callback")
	}
	// Replacing the attachment is deliberate and occurs only after acknowledgement.
	// Capture cleanup itself must neither reset that lifetime nor drop the edit.
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatalf("orphan captures blocked validated network recovery: %v", err)
	}
	got := resizeSnapshot(t, e)
	if got.Revision != 1 || resizeHash(t, got) != resizeHash(t, document.Snapshot()) || !saveForTest(t, ws, app.jobs) {
		t.Fatal("recovery lost the accepted edit or failed actual Save")
	}
	if got := e.Dmm().GetTile(util.Point{X: 1, Y: 3, Z: 1}).Instances()[2].Prefab().Vars().ValueV("dir", ""); got != "8" {
		t.Fatal("network recovery lost the independently accepted rotation")
	}
}

func TestSelectionPreviewCannotAcquireUnrelatedPendingTile(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	move, err := e.BeginSelectionMove(util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, util.Point{Y: 1}); err != nil {
		t.Fatal(err)
	}
	instance := e.Dmm().GetTile(util.Point{X: 4, Y: 3, Z: 1}).Instances()[2]
	prefab := instance.Prefab()
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, prefab.Path(), dmvars.Set(prefab.Vars(), "dir", "4")))
	previous := e.Dmm().Copy()
	if _, err := e.PreviewSelectionMove(move, util.Point{X: 2, Y: 2}); err == nil {
		t.Fatal("preview acquired a tile owned by an unrelated pending edit")
	}
	if !reflect.DeepEqual(e.Dmm().Copy(), previous) {
		t.Fatal("refused preview changed existing display work")
	}
	e.FinishSelectionMove(move, true)
	if _, err := e.SaveSnapshot(context.Background()); err == nil || app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("cancel committed or released the unrelated tile")
	}
	e.CommitOperation("Keep destination edit")
	got := resizeSnapshot(t, e)
	if got.Revision != 1 {
		t.Fatal("destination edit did not commit independently")
	}
	for _, tile := range got.Tiles {
		if tile.Coord == (model.Coord{X: 4, Y: 3, Z: 1}) && tile.State.Prefabs[2].Vars["dir"] != "4" {
			t.Fatal("refusal lost destination contents")
		}
	}
	app.commands.UndoV(ws.CommandStackId())
	if resizeHash(t, resizeSnapshot(t, e)) != resizeHash(t, before) {
		t.Fatal("destination edit lost its original before-state")
	}
}
