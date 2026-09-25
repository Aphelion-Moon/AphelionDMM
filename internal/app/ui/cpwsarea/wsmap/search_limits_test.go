package wsmap

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
)

func TestSearchOversizedSubmissionRetainsDraftAndAllowsRetry(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	initial := resizeSnapshot(t, e)
	initialHash := resizeHash(t, initial)
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	network, err := client.NewNetworkExecutor(client.NewWebSocketTransport(client.TransportConfig{}), initial, actor, "selection-verification")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { network.Terminate(nil) })
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	first := e.Dmm().Tiles[0].Instances()[2]
	replacement := dmmprefab.New(dmmprefab.IdNone, first.Prefab().Path(), dmvars.Set(first.Prefab().Vars(), "payload", strings.Repeat("x", protocol.MaxMessageBytes)))
	e.CommitInstanceBatch([]*dmminstance.Instance{first}, replacement, "Oversized search replacement")
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if len(app.errors) != 1 || !strings.Contains(app.errors[0].Error(), "maximum") || app.commands.HasUndoV(e.Dmm().Path.Absolute) || !e.CanStartMapEdit() {
		t.Fatal("oversized submission did not restore usable editing without history")
	}
	if resizeHash(t, resizeSnapshot(t, e)) != initialHash {
		t.Fatal("oversized submission changed save authority")
	}
	display, err := mapadapter.Import(e.Dmm(), initial.DocumentID, initial.EnvironmentHash)
	if err != nil || resizeHash(t, display) != initialHash {
		t.Fatalf("oversized submission left speculative display data: %v", err)
	}
	conflicts := network.Conflicts()
	if len(conflicts) != 1 || len(conflicts[0].Draft.Changes[0].After.Prefabs[2].Vars["payload"]) != protocol.MaxMessageBytes {
		t.Fatal("oversized search intent was lost")
	}
	if _, err := network.DiscardConflict(context.Background(), conflicts[0].OperationID); err != nil {
		t.Fatal(err)
	}
	// Reuse the same executor after reconnection; the smaller action must keep
	// the ordinary one-operation history and actor-scoped inverse path.
	transport := &selectionTransport{sent: make(chan protocol.ClientEnvelope, 8), ws: ws, app: app}
	network.Suspend(nil)
	if err := network.Resume(transport); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	first = e.Dmm().Tiles[0].Instances()[2]
	replacement = dmmprefab.New(dmmprefab.IdNone, first.Prefab().Path(), dmvars.Set(first.Prefab().Vars(), "dir", "4"))
	e.CommitInstanceBatch([]*dmminstance.Instance{first}, replacement, "Corrected search replacement")
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if resizeSnapshot(t, e).Revision != 1 || !app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("corrected replacement was not accepted once")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	acceptSelection(t, network, document, transport.next(t))
	runSelectionJob(t, app)
	e.ProcessCollaborationUpdates()
	if resizeHash(t, resizeSnapshot(t, e)) != initialHash {
		t.Fatal("corrected replacement inverse did not restore original hash")
	}
}
