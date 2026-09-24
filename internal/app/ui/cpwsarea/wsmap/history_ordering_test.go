package wsmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/util"
)

func TestHistorySavedSelectionBranchKeepsCloseGuard(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	// Give the moved cell distinct serialized content; stable IDs alone are
	// intentionally absent from DMM output in this otherwise uniform map.
	if err := grab.Rotate(true, ws.Map().Editor().RotateSelection); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []util.Point{{X: 1}, {Y: 1}} {
		if err := grab.Nudge(delta); err != nil {
			t.Fatal(err)
		}
		settleSelectionMove(t, ws, app)
	}
	if !saveForTest(t, ws, app.jobs) || ws.HasUnsavedChanges() {
		t.Fatal("fixture did not establish a saved map")
	}
	saved, err := os.ReadFile(ws.CommandStackId())
	if err != nil {
		t.Fatal(err)
	}
	app.commands.UndoV(ws.CommandStackId())
	if err := grab.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	settleSelectionMove(t, ws, app)
	if !app.commands.IsModified(ws.CommandStackId()) {
		t.Error("new selection edit at saved depth is marked clean")
	}
	// Exercise the independent close guard even with a falsely clean command marker.
	app.commands.ForceBalance(ws.CommandStackId())
	if !ws.HasUnsavedChanges() {
		t.Fatal("authority hash failed to protect an unsaved branch")
	}
	unchanged, err := os.ReadFile(ws.CommandStackId())
	if err != nil || !bytes.Equal(saved, unchanged) {
		t.Fatal("editing/close inspection modified the saved file")
	}
	if !saveForTest(t, ws, app.jobs) || ws.HasUnsavedChanges() {
		t.Fatal("saving the new branch did not clear the close guard")
	}
	updated, err := os.ReadFile(ws.CommandStackId())
	if err != nil || bytes.Equal(saved, updated) {
		t.Fatal("saving the new branch did not update the map file")
	}
}

func TestHistoryNewSelectionEditDuringAcknowledgement(t *testing.T) {
	for _, scenario := range []struct {
		operation string
		rejected  bool
	}{{"undo", false}, {"redo", false}, {"undo", true}, {"redo", true}} {
		t.Run(fmt.Sprintf("%s/rejected=%v", scenario.operation, scenario.rejected), func(t *testing.T) {
			operation := scenario.operation
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			initialHash := resizeHash(t, document.Snapshot())
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			app.commands.UndoV(ws.CommandStackId())
			pending := transport.next(t)
			if operation == "redo" {
				acceptSelection(t, network, document, pending)
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
				app.commands.RedoV(ws.CommandStackId())
				pending = transport.next(t)
			}
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 3, Z: 1}})
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			// Accept the independent new edit while the inverse/redo is in flight.
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			if _, err := e.SaveSnapshot(context.Background()); err == nil || !ws.HasUnsavedChanges() {
				t.Fatal("pending history allowed Save or appeared clean")
			}
			if scenario.rejected {
				snapshot := document.Snapshot()
				receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{
					OperationID: pending.OperationID, Code: "precondition_failed", Message: "controlled history rejection",
					Revision: snapshot.Revision, MapHash: resizeHash(t, snapshot),
				})
			} else {
				acceptSelection(t, network, document, pending)
			}
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			if !app.commands.UndoAsyncV(ws.CommandStackId(), nil) {
				t.Fatal("new edit has no undo")
			}
			acceptSelection(t, network, document, transport.next(t))
			runSelectionJob(t, app)
			e.ProcessCollaborationUpdates()
			originalApplied := (operation == "redo" && !scenario.rejected) || (operation == "undo" && scenario.rejected)
			if originalApplied {
				if !app.commands.UndoAsyncV(ws.CommandStackId(), nil) {
					t.Fatal("an applied edit was lost when another edit arrived")
				}
				acceptSelection(t, network, document, transport.next(t))
				runSelectionJob(t, app)
				e.ProcessCollaborationUpdates()
			}
			if app.commands.HasUndoV(ws.CommandStackId()) {
				t.Fatal("history retains an already-undone edit")
			}
			if scenario.rejected {
				if len(app.errors) != 1 || !errors.Is(app.errors[0], client.ErrOperationRejected) {
					t.Fatalf("history rejection errors: %v", app.errors)
				}
			} else if len(app.errors) != 0 {
				t.Fatalf("unexpected history errors: %v", app.errors)
			}
			if resizeHash(t, resizeSnapshot(t, e)) != initialHash {
				t.Fatal("undo history did not restore the original authoritative map")
			}
		})
	}
}
