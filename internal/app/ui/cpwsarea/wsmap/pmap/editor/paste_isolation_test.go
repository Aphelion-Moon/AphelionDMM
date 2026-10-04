package editor

import (
	"context"
	"reflect"
	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestNetworkPasteUsesIncrementalPublication(t *testing.T) {
	e := selectionEditor(t)
	app := e.app.(*editorTestApp)
	app.runLater = make(chan func(), 8)
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, e.authoritative, e.actorID, "paste")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(e.authoritative)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.beginPasteProposal([]dmmap.Tile{e.dmm.Tiles[0].Copy()}, func(string) bool { return true }, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
		t.Fatal(err)
	}
	settlePasteSource(t, e)
	if !e.ConfirmPastePlacement() {
		t.Fatal("paste confirmation was not admitted")
	}
	decoded, err := protocol.DecodeClient(mustEditorJSON(t, transport.next(t)))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := document.Apply(decoded.Payload.(*protocol.OperationSubmitPayload).Operation, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "paste-accepted", SessionID: "paste", Type: protocol.ServerOperationAccepted, Payload: mustEditorJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})}); err != nil {
		t.Fatal(err)
	}
	app.runScheduled(t)
	e.ProcessCollaborationUpdates()
	actual, err := e.authoritative.Hash()
	if err != nil || actual != hash || e.HasPastePlacement() || !app.commands.HasUndoV("test") {
		t.Fatalf("paste did not converge/settle: %v", err)
	}
	if stats := e.CollaborationPublicationStats(); stats.FullReplacements != 0 || stats.ReconstructedTiles != 1 {
		t.Fatalf("paste used full refresh or duplicate installation: %+v", stats)
	}
	if e.editWorkBudget().Used() != 0 {
		t.Fatal("accepted paste retained preparation resources")
	}
}

func settlePasteSource(t *testing.T, e *Editor) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.ProcessPasteWork()
		if e.paste != nil && e.paste.phase == pasteReady && !e.paste.workerBusy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("paste source did not settle")
}

func TestPasteTranslationAndCancelLeaveCommittedStateUntouched(t *testing.T) {
	e := selectionEditor(t)
	counted := &countedLocalEdits{Local: e.executor.(*executor.Local)}
	e.executor = counted
	before := e.dmm.Copy()
	generation, revision := e.SaveVersion()
	view, _ := e.MapViewVersion()
	source := []dmmap.Tile{e.dmm.Tiles[0].Copy()}
	if err := e.beginPasteProposal(source, func(string) bool { return true }, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
		t.Fatal(err)
	}
	settlePasteSource(t, e)
	payload, presentation := e.paste.payload, e.paste.presentation
	for i := 0; i < 100; i++ {
		// Both valid and out-of-bounds hover must leave committed state alone.
		_, _, _ = e.UpdatePastePlacement(util.Point{X: 1 + i%2, Y: 1, Z: 1})
		e.ProcessPasteWork()
	}
	if !reflect.DeepEqual(before, e.dmm.Copy()) || len(e.pendingChanges) != 0 || counted.snapshots != 0 || counted.wireEdits != 0 {
		t.Fatal("hover entered real map edit/capture/snapshot path")
	}
	if e.paste.payload != payload || e.paste.presentation != presentation || e.paste.request != 1 {
		t.Fatal("translation rebuilt source presentation")
	}
	if e.ChangedSinceSave(generation, revision) {
		t.Fatal("pure preview dirtied map")
	}
	if v, ready := e.MapViewVersion(); v != view || !ready {
		t.Fatal("pure preview invalidated committed query")
	}
	if _, err := e.SaveSnapshot(context.Background()); err != nil {
		t.Fatal("pure preview blocked committed save", err)
	}
	for i, instance := range source[0].Instances() {
		if instance.StableID() == payload.Tiles[0].Instances()[i].StableID() {
			t.Fatal("paste retained source identity")
		}
	}
	e.CancelPastePlacement()
	if e.HasPastePlacement() || !reflect.DeepEqual(before, e.dmm.Copy()) || e.editWorkBudget().Used() != 0 {
		t.Fatal("cancel restored/mutated map or retained resources")
	}
}

func TestPasteClickDuringPreparationRetainsExactTarget(t *testing.T) {
	e := selectionEditor(t)
	gate := make(chan struct{})
	source := []dmmap.Tile{e.dmm.Tiles[0].Copy()}
	if err := e.beginPasteProposalFromFactory(1, func(ctx context.Context) ([]dmmap.Tile, func(string) bool, *resources.Reservation, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, nil, nil, ctx.Err()
		}
		return source, func(string) bool { return true }, nil, nil
	}, nil, util.Point{X: 1, Y: 1, Z: 1}); err != nil {
		t.Fatal(err)
	}
	if !e.ConfirmPastePlacement() {
		t.Fatal("preparing click was discarded")
	}
	if _, _, err := e.UpdatePastePlacement(util.Point{X: 2, Y: 1, Z: 1}); err == nil {
		t.Fatal("preparing source unexpectedly reported ready")
	}
	if e.paste.target.X != 1 || e.paste.intent.X != 1 {
		t.Fatal("hover moved the pending click")
	}
	e.CancelPastePlacement()
	close(gate)
}
