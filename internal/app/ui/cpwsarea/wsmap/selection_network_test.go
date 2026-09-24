package wsmap

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/util"
)

type selectionTransport struct {
	sent chan protocol.ClientEnvelope
	ws   *WsMap
	app  *selectionTestApp
}

func (*selectionTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}
func (transport *selectionTransport) Send(_ context.Context, envelope protocol.ClientEnvelope) error {
	transport.sent <- envelope
	return nil
}
func (*selectionTransport) Close(websocket.StatusCode, string) error { return nil }

func (transport *selectionTransport) next(t *testing.T) model.Operation {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case envelope := <-transport.sent:
			var payload protocol.OperationSubmitPayload
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			return payload.Operation
		case <-deadline.C:
			t.Fatal("selection operation was not sent")
		case job := <-transport.app.jobs:
			job()
		case <-time.After(time.Millisecond):
			transport.ws.Map().Editor().ProcessCollaborationUpdates()
			transport.ws.Map().Editor().ProcessPasteWork()
		}
	}
}

func selectionNetwork(t *testing.T, ws *WsMap) (*client.NetworkExecutor, *selectionTransport, *engine.Document) {
	t.Helper()
	snapshot, err := ws.Map().Editor().CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	app := ws.app.(*selectionTestApp)
	app.network = true
	transport := &selectionTransport{sent: make(chan protocol.ClientEnvelope, 8), ws: ws, app: app}
	network, err := client.NewNetworkExecutor(transport, snapshot, actor, "selection-verification")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { network.Terminate(nil) })
	if err := ws.Map().Editor().AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return network, transport, document
}

func receiveSelection(t *testing.T, network *client.NetworkExecutor, kind protocol.ServerType, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "selection-verification", SessionID: "selection-verification", Type: kind, Payload: data}); err != nil {
		t.Fatal(err)
	}
}

func acceptSelection(t *testing.T, network *client.NetworkExecutor, document *engine.Document, operation model.Operation) {
	t.Helper()
	accepted, err := document.Apply(operation, time.Unix(0, int64(document.Snapshot().Revision+1)))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	receiveSelection(t, network, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})
}

func runSelectionJob(t *testing.T, app *selectionTestApp) {
	t.Helper()
	select {
	case job := <-app.jobs:
		job()
	case <-time.After(3 * time.Second):
		t.Fatal("selection completion was not scheduled")
	}
}

func TestSelectionNetworkRejectionDuringNewDrag(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	initial := document.Snapshot()
	initialHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	rotation := transport.next(t)
	move, err := e.BeginSelectionMove(grab.Bounds(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	receiveSelection(t, network, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: rotation.OperationID, Code: "precondition_failed", Message: "forced verification conflict", Revision: initial.Revision, MapHash: initialHash})
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if len(app.errors) != 1 || !errors.Is(app.errors[0], client.ErrOperationRejected) {
		t.Fatal("rejection was not reported")
	}
	if got := e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()[2].Prefab().Vars().ValueV("dir", ""); got != "8" {
		t.Fatal("older rejection replaced the newer open preview")
	}
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("open preview became saveable after rejection")
	}
	if conflicts := network.Conflicts(); len(conflicts) != 1 || conflicts[0].OperationID != rotation.OperationID {
		t.Fatal("rejected rotation intent was lost")
	}
	e.FinishSelectionMove(move, true)
	e.ProcessCollaborationUpdates()
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("rejected rotation or cancelled drag entered undo")
	}
	if err := grab.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	nudge := transport.next(t)
	for _, change := range nudge.Changes {
		if change.Coord.X == 1 && change.Before.Prefabs[2].Vars["dir"] != "2" {
			t.Fatal("nudge reused rejected rotated contents")
		}
	}
	acceptSelection(t, network, document, nudge)
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	app.commands.UndoV(e.Dmm().Path.Absolute)
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	got, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err := got.Hash()
	if err != nil || gotHash != initialHash {
		t.Fatal("acknowledged nudge undo did not restore initial hash")
	}
}

func TestSelectionNetworkRemotePassedTileSurvivesCommit(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	move, err := e.BeginSelectionMove(grab.Bounds(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []int{1, 2} {
		if _, err := e.PreviewSelectionMove(move, util.Point{X: x}); err != nil {
			t.Fatal(err)
		}
	}
	initial := document.Snapshot()
	baseHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	passed := initial.Tiles[1]
	after := model.CloneTileState(passed.State)
	after.Prefabs[2].Vars["dir"] = "4"
	opID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	remote := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: initial.DocumentID, ActorID: actor, OperationID: opID, BaseRevision: initial.Revision, BaseMapHash: baseHash, EnvironmentHash: initial.EnvironmentHash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{{Coord: passed.Coord, Before: passed.State, After: after}}}
	acceptSelection(t, network, document, remote)
	e.ProcessCollaborationUpdates()
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("remote update released the open gesture guard")
	}
	e.FinishSelectionMove(move, false)
	operation := transport.next(t)
	if len(operation.Changes) != 2 {
		t.Fatalf("move submitted %d tiles, want source/destination", len(operation.Changes))
	}
	for _, change := range operation.Changes {
		if change.Coord == passed.Coord {
			t.Fatal("move included restored passed-over tile")
		}
	}
	acceptSelection(t, network, document, operation)
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if got := e.Dmm().Tiles[1].Instances()[2].Prefab().Vars().ValueV("dir", ""); got != "4" {
		t.Fatal("move overwrote remote passed-tile edit")
	}
	if e.Dmm().Tiles[2].Instances()[2].StableID() != string(initial.Tiles[0].State.Prefabs[2].StableID) {
		t.Fatal("move lost source identity")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if got := e.Dmm().Tiles[1].Instances()[2].Prefab().Vars().ValueV("dir", ""); got != "4" {
		t.Fatal("undo erased another actor's edit")
	}
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}
}

func TestSelectionNetworkQueuedCompletionCannotReviveOldDrag(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	network, transport, document := selectionNetwork(t, ws)
	e := ws.Map().Editor()
	initial := document.Snapshot()
	if err := grab.Rotate(true, e.RotateSelection); err != nil {
		t.Fatal(err)
	}
	operation := transport.next(t)
	move, err := e.BeginSelectionMove(grab.Bounds(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviewSelectionMove(move, util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	acceptSelection(t, network, document, operation)
	// The successful callback is queued while a newer preview belongs to the
	// old attachment. Install a different snapshot before dispatching that job.
	e.Close()
	initial.Tiles[0].State.Prefabs[2].Vars["dir"] = "4"
	replacementDoc, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := executor.NewLocal(replacementDoc, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(replacement); err != nil {
		t.Fatal(err)
	}
	runSelectionJob(t, app)
	if _, err := e.PreviewSelectionMove(move, util.Point{X: 2}); err == nil {
		t.Fatal("old attachment preview was accepted")
	}
	e.FinishSelectionMove(move, true)
	if got := e.Dmm().Tiles[0].Instances()[2].Prefab().Vars().ValueV("dir", ""); got != "4" {
		t.Fatal("old callback or cancel restored over new attachment")
	}
	if app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("old callback added undo to replacement attachment")
	}
}
