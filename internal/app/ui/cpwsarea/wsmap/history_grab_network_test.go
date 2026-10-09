package wsmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

// settleGrabHistory drains queued completion jobs and collaboration updates.
func settleGrabHistory(t *testing.T, ws *WsMap, app *selectionTestApp) {
	t.Helper()
	e := ws.Map().Editor()
	for i := 0; i < 8; i++ {
		e.ProcessCollaborationUpdates()
		select {
		case job := <-app.jobs:
			job()
		case <-time.After(20 * time.Millisecond):
		}
	}
	e.ProcessCollaborationUpdates()
}

func rejectGrabOperation(t *testing.T, network *client.NetworkExecutor, document *engine.Document, operation model.Operation) {
	t.Helper()
	snapshot := document.Snapshot()
	receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{
		OperationID: operation.OperationID, Code: "precondition_failed", Message: "controlled rejection",
		Revision: snapshot.Revision, MapHash: resizeHash(t, snapshot),
	})
}

// moveGrabSelection drags the current Grab selection one tile and releases it.
func moveGrabSelection(t *testing.T, ws *WsMap, grab *tools.ToolGrab, shift util.Point) {
	t.Helper()
	e := ws.Map().Editor()
	move, err := e.BeginSelectionMove(grab.Bounds(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, shift); err != nil {
		t.Fatal(err)
	}
	e.FinishSelectionMove(move, false)
}

func TestHistoryUndoThenGrabMoveOverUndoneTiles(t *testing.T) {
	type outcome int
	const (
		accept outcome = iota
		reject
	)
	for _, scenario := range []struct {
		name     string
		holdUndo bool
		undo     outcome
		move     outcome
	}{
		{"undo-held/accept/accept", true, accept, accept},
		{"undo-held/accept/reject-move", true, accept, reject},
		{"undo-held/reject/reject-move", true, reject, reject},
		{"undo-acked/accept", false, accept, accept},
		{"undo-acked/reject-move", false, accept, reject},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			stack := ws.CommandStackId()
			t.Cleanup(func() { t.Logf("reported errors: %v", app.errors) })
			initialHash := resizeHash(t, document.Snapshot())

			// Edit A: nudge (1,1) to (2,1), acknowledged.
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			acceptSelection(t, network, document, transport.next(t))
			settleGrabHistory(t, ws, app)

			// Undo A.
			app.commands.UndoV(stack)
			undoOp := transport.next(t)
			e.ProcessCollaborationUpdates() // frame loop installs the pending inverse view
			if !scenario.holdUndo {
				acceptSelection(t, network, document, undoOp)
				settleGrabHistory(t, ws, app)
			}

			// Grab-select a region including A's tiles, then move it.
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
			moveGrabSelection(t, ws, grab, util.Point{Y: 1})
			moveOp := transport.next(t)

			if scenario.holdUndo {
				if scenario.undo == accept {
					acceptSelection(t, network, document, undoOp)
				} else {
					rejectGrabOperation(t, network, document, undoOp)
				}
			}
			if scenario.move == accept {
				acceptSelection(t, network, document, moveOp)
			} else {
				rejectGrabOperation(t, network, document, moveOp)
			}
			settleGrabHistory(t, ws, app)

			// Undo the move, then redo it, acknowledging whatever is submitted.
			for _, step := range []string{"undo", "redo", "undo"} {
				var ok bool
				if step == "undo" {
					ok = app.commands.UndoAsyncV(stack, nil)
				} else {
					ok = app.commands.RedoAsyncV(stack, nil)
				}
				if !ok {
					break
				}
				operation := transport.next(t)
				acceptSelection(t, network, document, operation)
				settleGrabHistory(t, ws, app)
			}
			for app.commands.HasUndoV(stack) {
				if !app.commands.UndoAsyncV(stack, nil) {
					break
				}
				acceptSelection(t, network, document, transport.next(t))
				settleGrabHistory(t, ws, app)
			}
			for _, err := range app.errors {
				if strings.Contains(err.Error(), "precondition failed") && scenario.move == accept && scenario.undo == accept {
					t.Fatalf("ordinary sequence reported %v", err)
				}
			}
			if scenario.move == accept && scenario.undo == accept && len(app.errors) != 0 {
				t.Fatalf("unexpected errors: %v", app.errors)
			}
			authority := resizeHash(t, document.Snapshot())
			display := resizeHash(t, resizeSnapshot(t, e))
			if authority != display {
				t.Fatalf("display %s != authority %s", display, authority)
			}
			if scenario.move == accept && scenario.undo == accept && authority != initialHash {
				t.Fatal("full undo did not restore the initial map")
			}
		})
	}
}

// tryNextGrabOperation returns a submitted operation, or false when none arrives.
func tryNextGrabOperation(t *testing.T, ws *WsMap, app *selectionTestApp, transport *selectionTransport) (model.Operation, bool) {
	t.Helper()
	e := ws.Map().Editor()
	deadline := time.After(400 * time.Millisecond)
	for {
		select {
		case envelope := <-transport.sent:
			var payload protocol.OperationSubmitPayload
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			return payload.Operation, true
		case job := <-app.jobs:
			job()
		case <-deadline:
			return model.Operation{}, false
		case <-time.After(time.Millisecond):
			e.ProcessCollaborationUpdates()
		}
	}
}

// An undo issued while an overlapping Grab move is still unacknowledged used to
// be built from authority and then fail against the speculative projection.
func TestHistoryUndoWaitsForOverlappingPendingGrabMove(t *testing.T) {
	for _, moveAccepted := range []bool{true, false} {
		t.Run(fmt.Sprintf("move-accepted=%v", moveAccepted), func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			grab := activateSelectionWorkspace(t, ws)
			network, transport, document := selectionNetwork(t, ws)
			e := ws.Map().Editor()
			stack := ws.CommandStackId()

			// A moves (1,1) to (2,1); B moves (1,3) to (2,3). Both acknowledged.
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			acceptSelection(t, network, document, transport.next(t))
			settleGrabHistory(t, ws, app)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 3, Z: 1}})
			if err := grab.Nudge(util.Point{X: 1}); err != nil {
				t.Fatal(err)
			}
			acceptSelection(t, network, document, transport.next(t))
			settleGrabHistory(t, ws, app)

			// Undo B (acknowledged); then grab-move over A's tiles and hold the move.
			app.commands.UndoV(stack)
			acceptSelection(t, network, document, transport.next(t))
			settleGrabHistory(t, ws, app)
			grab.Reset()
			grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
			moveGrabSelection(t, ws, grab, util.Point{Y: 1})
			move := transport.next(t)

			// Undo A while both are still pending. It must wait, not fail.
			app.commands.UndoV(stack)
			if extra, sent := tryNextGrabOperation(t, ws, app, transport); sent {
				t.Fatalf("undo was submitted over an unacknowledged overlapping move: %s", extra.OperationID)
			}
			if moveAccepted {
				acceptSelection(t, network, document, move)
			} else {
				rejectGrabOperation(t, network, document, move)
			}
			if inverse, sent := tryNextGrabOperation(t, ws, app, transport); sent {
				acceptSelection(t, network, document, inverse)
			}
			settleGrabHistory(t, ws, app)
			for _, err := range app.errors {
				if strings.Contains(err.Error(), "precondition failed") {
					t.Fatalf("raw precondition error reached the user: %v", err)
				}
			}
			if !moveAccepted {
				// Only the rejected move's report is expected; undo A must have run.
				if len(app.errors) != 1 || !errors.Is(app.errors[0], client.ErrOperationRejected) {
					t.Fatalf("errors: %v", app.errors)
				}
			}
			if resizeHash(t, document.Snapshot()) != resizeHash(t, resizeSnapshot(t, e)) {
				t.Fatal("display diverged from authority")
			}
		})
	}
}

// Undo/redo cannot run underneath an open Grab drag; it is refused up front.
func TestHistoryUndoRefusedWhileGrabDragOpen(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	stack := ws.CommandStackId()
	if err := grab.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	acceptSelection(t, network, document, transport.next(t))
	settleGrabHistory(t, ws, app)
	move, err := e.BeginSelectionMove(grab.Bounds(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, util.Point{Y: 1}); err != nil {
		t.Fatal(err)
	}
	app.commands.UndoV(stack)
	settleGrabHistory(t, ws, app)
	if extra, sent := tryNextGrabOperation(t, ws, app, transport); sent {
		t.Fatalf("undo was submitted under an open drag: %s", extra.OperationID)
	}
	if len(app.errors) != 1 || !strings.Contains(app.errors[0].Error(), "move") {
		t.Fatalf("undo during drag was not refused clearly: %v", app.errors)
	}
	e.FinishSelectionMove(move, true)
	e.ProcessCollaborationUpdates()
	if !app.commands.HasUndoV(stack) {
		t.Fatal("refused undo lost the history entry")
	}
	app.commands.UndoV(stack)
	acceptSelection(t, network, document, transport.next(t))
	settleGrabHistory(t, ws, app)
	if resizeHash(t, document.Snapshot()) != resizeHash(t, resizeSnapshot(t, e)) {
		t.Fatal("display diverged from authority")
	}
}
