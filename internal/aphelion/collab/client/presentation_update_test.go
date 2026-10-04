package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestTakePresentationUpdateCoalescesLatestSparseTiles(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}

	first := projectionOperation(t, snapshot, 1)
	first.ActorID = actor
	firstAccepted := model.AcceptedOperation{Operation: first, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(1, 0)}
	firstSnapshot := snapshotWithOperation(t, snapshot, firstAccepted)
	firstHash, err := firstSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: firstAccepted, MapHash: firstHash})); err != nil {
		t.Fatal(err)
	}

	second := projectionOperation(t, firstSnapshot, 1)
	second.ActorID = actor
	second.Changes[0].Before = model.CloneTileState(first.Changes[0].After)
	second.Changes[0].After = model.TileState{}
	secondAccepted := model.AcceptedOperation{Operation: second, Revision: firstAccepted.Revision + 1, AcceptedAt: time.Unix(2, 0)}
	secondSnapshot := snapshotWithOperation(t, firstSnapshot, secondAccepted)
	secondHash, err := secondSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: secondAccepted, MapHash: secondHash})); err != nil {
		t.Fatal(err)
	}

	update := network.TakePresentationUpdate()
	if update == nil {
		t.Fatal("TakePresentationUpdate returned nil after accepted changes")
	}
	if update.Sequence == 0 || update.DocumentID != snapshot.DocumentID || update.EnvironmentHash != snapshot.EnvironmentHash || update.Revision != secondAccepted.Revision || update.MapHash != secondHash {
		t.Fatalf("presentation metadata = %#v", update)
	}
	if len(update.Authoritative) != 1 || update.Authoritative[0].Coord != first.Changes[0].Coord || !update.Authoritative[0].State.Equal(model.TileState{}) {
		t.Fatalf("coalesced authoritative tiles = %#v", update.Authoritative)
	}
	if len(update.Display) != 1 || update.Display[0].Coord != first.Changes[0].Coord || !update.Display[0].State.Equal(model.TileState{}) {
		t.Fatalf("coalesced display tiles = %#v", update.Display)
	}
	if update.Replacement != nil {
		t.Fatal("ordinary accepted changes unexpectedly requested a replacement")
	}
	if next := network.TakePresentationUpdate(); next != nil {
		t.Fatalf("presentation update was not drained: %#v", next)
	}
}

func TestTakePresentationUpdateReportsEmptyDisplayPatch(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	operation := projectionOperation(t, snapshot, 1)
	results := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), operation, func(_ model.AcceptedOperation, executeErr error) { results <- executeErr }); err != nil {
		t.Fatal(err)
	}
	submitted := transport.next(t)
	deadline := time.After(time.Second)
	for {
		if update := network.TakePresentationUpdate(); update != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("local speculative publication did not become available")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// The local speculative state is already represented by the editor's gesture.
	// A remote acceptance that leaves that effective state unchanged still advances
	// authority, but must not force a display tile reconstruction.
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	acceptedOperation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted := model.AcceptedOperation{Operation: acceptedOperation, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	hash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	update := network.TakePresentationUpdate()
	if update == nil {
		t.Fatal("TakePresentationUpdate returned nil after acknowledgement")
	}
	if len(update.Authoritative) != 1 {
		t.Fatalf("authoritative tile count = %d, want 1", len(update.Authoritative))
	}
	if len(update.Display) != 0 {
		t.Fatalf("unchanged effective display = %#v, want empty patch", update.Display)
	}
}

func TestCaptureProjectionAndPendingMetadataAvoidExecutorMutex(t *testing.T) {
	network, err := NewNetworkExecutor(newFakeTransport(), projectionSnapshot(t), mustActorID(t), "session")
	if err != nil {
		t.Fatal(err)
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	finished := make(chan error, 1)
	go func() {
		capture, captureErr := network.CaptureProjection(context.Background())
		if captureErr == nil && capture.DocumentID() == "" {
			captureErr = errors.New("capture lost immutable metadata")
		}
		if captureErr == nil && network.HasUnacknowledgedOperations() {
			captureErr = errors.New("initial publication unexpectedly has pending operations")
		}
		_ = network.Conflicts()
		finished <- captureErr
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("immutable capture read waited on reconciliation mutex")
	}
}

func TestExecuteAsyncAdmissionGatesPresentationUntilPendingInstalled(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actor := mustActorID(t)
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, snapshot, 1), func(_ model.AcceptedOperation, executeErr error) { completed <- executeErr }); err != nil {
		t.Fatal(err)
	}
	if update := network.TakePresentationUpdate(); update != nil {
		t.Fatalf("presentation escaped before admitted pending view was installed: %#v", update)
	}
	transport.next(t)
	deadline := time.After(time.Second)
	for {
		if update := network.TakePresentationUpdate(); update != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("admission did not publish its pending view")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	network.Terminate(errors.New("test cleanup"))
	if err := <-completed; err == nil {
		t.Fatal("terminated pending execution unexpectedly succeeded")
	}
}

func TestExecuteAsyncValidationFailurePublishesRestoreDelta(t *testing.T) {
	snapshot := projectionSnapshot(t)
	operation := projectionOperation(t, snapshot, 1)
	operation.Changes[0].Before = model.TileState{Prefabs: []model.PrefabState{{StableID: mustStableID(t), Path: "/wrong", Vars: map[string]string{}}}}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, operation.ActorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), operation, func(_ model.AcceptedOperation, executeErr error) { completed <- executeErr }); err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err == nil {
		t.Fatal("invalid local operation unexpectedly succeeded")
	}
	update := network.TakePresentationUpdate()
	if update == nil || len(update.Display) != 1 || update.Display[0].Coord != operation.Changes[0].Coord || !update.Display[0].State.Equal(tileAt(snapshot, operation.Changes[0].Coord.X)) {
		t.Fatalf("validation restore update = %#v", update)
	}
}

func mustActorID(t *testing.T) model.ActorID {
	t.Helper()
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	return actor
}

func mustStableID(t *testing.T) model.StableID {
	t.Helper()
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	return stableID
}
