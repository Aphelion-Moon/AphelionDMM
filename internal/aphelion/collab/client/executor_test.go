package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestNetworkExecutorAcceptRejectAndInverse(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	operation := projectionOperation(t, snapshot, 1)
	result := make(chan model.AcceptedOperation, 1)
	errorsFound := make(chan error, 1)
	go func() {
		accepted, executeErr := network.Execute(context.Background(), operation)
		result <- accepted
		errorsFound <- executeErr
	}()
	submitted := transport.next(t)
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	submission := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted := model.AcceptedOperation{Operation: submission, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	acceptedSnapshot := snapshotWithOperation(t, snapshot, accepted)
	acceptedHash, err := acceptedSnapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: acceptedHash})); err != nil {
		t.Fatalf("receive accepted operation: %v", err)
	}
	if executeErr := <-errorsFound; executeErr != nil {
		t.Fatal(executeErr)
	}
	if actual := <-result; actual.Revision != 1 {
		t.Fatalf("Execute() revision = %d, want 1", actual.Revision)
	}
	synchronized, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if synchronized.Revision != 1 {
		t.Fatalf("Snapshot() revision = %d, want 1", synchronized.Revision)
	}
	inverse, err := network.BuildInverse(context.Background(), accepted.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if inverse.InverseOf == nil || *inverse.InverseOf != accepted.OperationID || inverse.ActorID != actorID {
		t.Fatalf("BuildInverse() = %#v", inverse)
	}

	rejectedOperation := projectionOperation(t, synchronized, 2)
	rejectedErrors := make(chan error, 1)
	go func() {
		_, executeErr := network.Execute(context.Background(), rejectedOperation)
		rejectedErrors <- executeErr
	}()
	rejectedSubmission := transport.next(t)
	rejectedDecoded, err := protocol.DecodeClient(mustJSON(t, rejectedSubmission))
	if err != nil {
		t.Fatal(err)
	}
	rejectedID := rejectedDecoded.Payload.(*protocol.OperationSubmitPayload).Operation.OperationID
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{OperationID: rejectedID, Code: "precondition_failed", Message: "conflict", Revision: 1, MapHash: acceptedHash})); err != nil {
		t.Fatalf("receive rejected operation: %v", err)
	}
	if executeErr := <-rejectedErrors; !errors.Is(executeErr, ErrOperationRejected) {
		t.Fatalf("Execute(rejected) error = %v, want %v", executeErr, ErrOperationRejected)
	}
}

func TestNetworkExecutorIgnoresExactAcceptedDuplicate(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, executeErr := network.Execute(context.Background(), projectionOperation(t, snapshot, 1))
		result <- executeErr
	}()
	submitted := transport.next(t)
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	mapHash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	envelope := serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: mapHash})
	if err := network.Receive(envelope); err != nil {
		t.Fatalf("receive accepted operation: %v", err)
	}
	if executeErr := <-result; executeErr != nil {
		t.Fatal(executeErr)
	}
	if err := network.Receive(envelope); err != nil {
		t.Fatalf("receive accepted duplicate: %v", err)
	}

	network.mutex.Lock()
	terminal := network.terminal
	network.mutex.Unlock()
	if terminal != nil {
		t.Fatalf("exact accepted duplicate terminated executor: %v", terminal)
	}
	current, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != accepted.Revision {
		t.Fatalf("snapshot revision = %d, want %d", current.Revision, accepted.Revision)
	}
}

func TestNetworkExecutorSuspendsOnAlteredAcceptedDuplicate(t *testing.T) {
	network, accepted, mapHash := acceptedNetworkFixture(t)
	altered := model.CloneAcceptedOperation(accepted)
	altered.Operation.Changes[0].After.Prefabs[0].Path = "/turf/open/floor/iron"
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: altered, MapHash: mapHash})); err == nil {
		t.Fatal("altered accepted duplicate was ignored")
	}
	network.mutex.Lock()
	suspended, terminal := network.suspended, network.terminal
	network.mutex.Unlock()
	if suspended == nil || terminal != nil {
		t.Fatalf("altered duplicate state = suspended %v terminal %v", suspended, terminal)
	}
}

func TestNetworkExecutorSuspendsOnAcceptedDuplicateWithWrongHash(t *testing.T) {
	network, accepted, _ := acceptedNetworkFixture(t)
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: strings.Repeat("f", 64)})); err == nil {
		t.Fatal("accepted duplicate with wrong hash was ignored")
	}
	network.mutex.Lock()
	suspended, terminal := network.suspended, network.terminal
	network.mutex.Unlock()
	if suspended == nil || terminal != nil {
		t.Fatalf("wrong-hash duplicate state = suspended %v terminal %v", suspended, terminal)
	}
}

func acceptedNetworkFixture(t *testing.T) (*NetworkExecutor, model.AcceptedOperation, string) {
	t.Helper()
	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, executeErr := network.Execute(context.Background(), projectionOperation(t, snapshot, 1))
		result <- executeErr
	}()
	decoded, err := protocol.DecodeClient(mustJSON(t, transport.next(t)))
	if err != nil {
		t.Fatal(err)
	}
	accepted := model.AcceptedOperation{Operation: decoded.Payload.(*protocol.OperationSubmitPayload).Operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	mapHash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: mapHash})); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	return network, accepted, mapHash
}

func TestNetworkExecutorSuspendsOnAcceptedRevisionGap(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	operation := projectionOperation(t, snapshot, 1)
	accepted := model.AcceptedOperation{Operation: operation, Revision: snapshot.Revision + 2, AcceptedAt: time.Unix(1, 0)}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: strings.Repeat("a", 64)})); err == nil || !strings.Contains(err.Error(), "accepted revision") {
		t.Fatalf("receive revision gap error = %v, want accepted revision error", err)
	}
	network.mutex.Lock()
	terminal := network.terminal
	suspended := network.suspended
	network.mutex.Unlock()
	if terminal != nil || suspended == nil {
		t.Fatalf("revision gap state = terminal %v, suspended %v; want recoverable suspension", terminal, suspended)
	}
}

func TestNetworkExecutorConflictRefreshDiscardAndRebuild(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	draft := projectionOperation(t, snapshot, 1)
	rejected := make(chan error, 1)
	go func() {
		_, executeErr := network.Execute(context.Background(), draft)
		rejected <- executeErr
	}()
	submitted := transport.next(t)
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	submission := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	mapHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationRejected, protocol.OperationRejectedPayload{
		OperationID: submission.OperationID,
		Code:        "precondition_failed",
		Message:     "conflict",
		Revision:    snapshot.Revision,
		MapHash:     mapHash,
	})); err != nil {
		t.Fatalf("receive rejected operation: %v", err)
	}
	if executeErr := <-rejected; !errors.Is(executeErr, ErrOperationRejected) {
		t.Fatalf("Execute() error = %v, want %v", executeErr, ErrOperationRejected)
	}

	refreshed, err := network.RefreshConflict(context.Background(), submission.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Revision != snapshot.Revision || len(network.Conflicts()) != 1 {
		t.Fatalf("refresh = revision %d, conflicts %d; want revision %d, conflicts 1", refreshed.Revision, len(network.Conflicts()), snapshot.Revision)
	}
	rebuilt, err := network.BuildConflictRebuild(context.Background(), submission.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.OperationID == submission.OperationID || rebuilt.ActorID != actorID || rebuilt.BaseRevision != snapshot.Revision || rebuilt.BaseMapHash != mapHash {
		t.Fatalf("rebuilt operation identity/base = %#v", rebuilt)
	}
	if len(rebuilt.Changes) != 1 || !rebuilt.Changes[0].Before.Equal(tileAt(snapshot, 1)) || !rebuilt.Changes[0].After.Equal(submission.Changes[0].After) {
		t.Fatalf("rebuilt changes = %#v, want current authoritative before and rejected intended after", rebuilt.Changes)
	}
	if len(network.Conflicts()) != 1 {
		t.Fatal("building a replacement dismissed the unresolved conflict")
	}
	if _, err := network.DiscardConflict(context.Background(), submission.OperationID); err != nil {
		t.Fatal(err)
	}
	if len(network.Conflicts()) != 0 {
		t.Fatal("discard retained the conflict")
	}
	select {
	case message := <-transport.sent:
		t.Fatalf("conflict lifecycle sent an unexpected network message: %#v", message)
	default:
	}
}

func TestNetworkExecutorConformanceOverWebSocket(t *testing.T) {
	t.Parallel()

	baseURL, sessionID, token, snapshot, shutdown := startClientTestService(t)
	defer shutdown()
	transport := NewWebSocketTransport(TransportConfig{})
	received := make(chan protocol.ServerEnvelope, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := transport.Connect(ctx, protocol.JoinRequest{BaseURL: baseURL, Origin: "http://127.0.0.1", Token: token, SessionID: sessionID}, func(message protocol.ServerEnvelope) {
		received <- message
	}); err != nil {
		t.Fatal(err)
	}
	joined := waitForServerType(t, received, protocol.ServerJoined)
	decodedJoined, err := protocol.DecodeServer(mustJSON(t, joined))
	if err != nil {
		t.Fatal(err)
	}
	actorID := decodedJoined.Payload.(*protocol.JoinedPayload).ActorID
	network, err := NewNetworkExecutor(transport, snapshot, actorID, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	waitForServerType(t, received, protocol.ServerReplayComplete)

	operation := projectionOperation(t, snapshot, 1)
	accepted := executeWhileReceiving(t, network, received, operation)
	if accepted.Revision != 1 {
		t.Fatalf("Execute() revision = %d, want 1", accepted.Revision)
	}
	inverse, err := network.BuildInverse(context.Background(), accepted.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	acceptedInverse := executeWhileReceiving(t, network, received, inverse)
	if acceptedInverse.Revision != 2 {
		t.Fatalf("Execute(inverse) revision = %d, want 2", acceptedInverse.Revision)
	}
	result, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 2 || !tileAt(result, 1).Equal(model.TileState{}) {
		t.Fatalf("Snapshot() = revision %d tile %#v, want revision 2 empty tile", result.Revision, tileAt(result, 1))
	}
}

func TestNetworkExecutorExecuteAsyncDoesNotWaitForAcknowledgement(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan operationResult, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, snapshot, 1), func(accepted model.AcceptedOperation, executeErr error) {
		results <- operationResult{accepted: accepted, err: executeErr}
	}); err != nil {
		t.Fatal(err)
	}
	submitted := transport.next(t)
	select {
	case result := <-results:
		t.Fatalf("callback completed before acknowledgement: %#v", result)
	default:
	}
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	mapHash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: mapHash})); err != nil {
		t.Fatalf("receive accepted operation: %v", err)
	}
	select {
	case result := <-results:
		if result.err != nil || result.accepted.Revision != 1 {
			t.Fatalf("callback result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not receive acknowledgement")
	}
}

func TestNetworkExecutorExecuteAsyncCallbacksRemainInAdmissionOrder(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actorID := mustActorID(t)
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	callbacks := make(chan string, 2)
	first := projectionOperation(t, snapshot, 1)
	second := projectionOperation(t, snapshot, 2)
	if err := network.ExecuteAsync(context.Background(), first, func(accepted model.AcceptedOperation, executeErr error) {
		if executeErr != nil {
			callbacks <- "first-error"
			return
		}
		firstStarted <- struct{}{}
		<-releaseFirst
		callbacks <- string(accepted.OperationID)
	}); err != nil {
		t.Fatal(err)
	}
	firstEnvelope := transport.next(t)
	if err := network.ExecuteAsync(context.Background(), second, func(accepted model.AcceptedOperation, executeErr error) {
		if executeErr != nil {
			callbacks <- "second-error"
			return
		}
		callbacks <- string(accepted.OperationID)
	}); err != nil {
		t.Fatal(err)
	}
	secondEnvelope := transport.next(t)
	decodedFirst, err := protocol.DecodeClient(mustJSON(t, firstEnvelope))
	if err != nil {
		t.Fatal(err)
	}
	decodedSecond, err := protocol.DecodeClient(mustJSON(t, secondEnvelope))
	if err != nil {
		t.Fatal(err)
	}
	firstOperation := decodedFirst.Payload.(*protocol.OperationSubmitPayload).Operation
	secondOperation := decodedSecond.Payload.(*protocol.OperationSubmitPayload).Operation
	acceptedFirst := model.AcceptedOperation{Operation: firstOperation, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(1, 0)}
	afterFirst := snapshotWithOperation(t, snapshot, acceptedFirst)
	firstHash, err := afterFirst.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: acceptedFirst, MapHash: firstHash})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first callback did not start")
	}
	acceptedSecond := model.AcceptedOperation{Operation: secondOperation, Revision: snapshot.Revision + 2, AcceptedAt: time.Unix(2, 0)}
	afterSecond := snapshotWithOperation(t, afterFirst, acceptedSecond)
	secondHash, err := afterSecond.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: acceptedSecond, MapHash: secondHash})); err != nil {
		t.Fatal(err)
	}
	select {
	case callback := <-callbacks:
		t.Fatalf("second callback ran while first callback was blocked: %q", callback)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case callback := <-callbacks:
		if callback != string(firstOperation.OperationID) {
			t.Fatalf("first callback = %q, want %q", callback, firstOperation.OperationID)
		}
	case <-time.After(time.Second):
		t.Fatal("first callback did not finish")
	}
	select {
	case callback := <-callbacks:
		if callback != string(secondOperation.OperationID) {
			t.Fatalf("second callback = %q, want %q", callback, secondOperation.OperationID)
		}
	case <-time.After(time.Second):
		t.Fatal("second callback did not finish")
	}
}

func TestNetworkExecutorAcceptsLocallyVerifiedStaleDisjointBase(t *testing.T) {
	snapshot := projectionSnapshot(t)
	actor := mustActorID(t)
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	remote := projectionOperation(t, snapshot, 1)
	remote.ActorID = mustActorID(t)
	accepted := model.AcceptedOperation{Operation: remote, Revision: snapshot.Revision + 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	hash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
		t.Fatal(err)
	}

	local := projectionOperation(t, snapshot, 2)
	completed := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), local, func(_ model.AcceptedOperation, executeErr error) { completed <- executeErr }); err != nil {
		t.Fatal(err)
	}
	submitted := transport.next(t)
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	if got.BaseRevision != local.BaseRevision || got.BaseMapHash != local.BaseMapHash {
		t.Fatalf("stale operation header was rewritten: %#v", got)
	}
	network.Terminate(errors.New("test cleanup"))
	if err := <-completed; err == nil {
		t.Fatal("terminated stale operation unexpectedly succeeded")
	}
}

func TestNetworkExecutorTerminationReleasesPendingOperation(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, snapshot, 1), func(_ model.AcceptedOperation, executeErr error) {
		results <- executeErr
	}); err != nil {
		t.Fatal(err)
	}
	transport.next(t)

	cause := errors.New("connection lost")
	network.Terminate(cause)
	select {
	case executeErr := <-results:
		if !errors.Is(executeErr, cause) {
			t.Fatalf("pending operation error = %v, want %v", executeErr, cause)
		}
	case <-time.After(time.Second):
		t.Fatal("pending operation remained blocked after executor termination")
	}
	after, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("read acknowledged snapshot after termination: %v", err)
	}
	if !tileAt(after, 1).Equal(tileAt(snapshot, 1)) {
		t.Fatal("terminated executor did not retain the acknowledged snapshot for rollback")
	}
	if network.HasUnacknowledgedOperations() {
		t.Fatal("terminated executor retained speculative operations")
	}
}

func TestNetworkExecutorSuspendsAndResumesOnFreshTransport(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	first := newFakeTransport()
	network, err := NewNetworkExecutor(first, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	pendingResult := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, snapshot, 1), func(_ model.AcceptedOperation, executeErr error) { pendingResult <- executeErr }); err != nil {
		t.Fatal(err)
	}
	first.next(t)
	lost := errors.New("connection lost")
	network.Suspend(lost)
	if executeErr := <-pendingResult; !errors.Is(executeErr, lost) {
		t.Fatalf("suspended pending operation error = %v, want %v", executeErr, lost)
	}
	second := newFakeTransport()
	if err := network.Resume(second); err != nil {
		t.Fatal(err)
	}
	acceptedResult := make(chan operationResult, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, snapshot, 2), func(accepted model.AcceptedOperation, executeErr error) {
		acceptedResult <- operationResult{accepted: accepted, err: executeErr}
	}); err != nil {
		t.Fatal(err)
	}
	submitted := second.next(t)
	decoded, err := protocol.DecodeClient(mustJSON(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	operation := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	accepted := model.AcceptedOperation{Operation: operation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	after := snapshotWithOperation(t, snapshot, accepted)
	mapHash, err := after.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: mapHash})); err != nil {
		t.Fatalf("receive resumed operation: %v", err)
	}
	result := <-acceptedResult
	if result.err != nil || result.accepted.OperationID != operation.OperationID {
		t.Fatalf("resumed execution = %#v", result)
	}
}

func TestNetworkExecutorReplacesAcknowledgedSnapshotWithoutRollback(t *testing.T) {
	t.Parallel()

	snapshot := projectionSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := newFakeTransport()
	network, err := NewNetworkExecutor(transport, snapshot, actorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	replacement := model.CloneSnapshot(snapshot)
	replacement.Revision = snapshot.Revision + 3
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	replacement.Tiles = []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: stableID, Path: "/turf/open/floor", Vars: map[string]string{"dir": "8"}}}}}}
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	current, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != replacement.Revision || !current.Tiles[0].State.Equal(replacement.Tiles[0].State) {
		t.Fatalf("replacement snapshot = %#v, want %#v", current, replacement)
	}
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), snapshot); err == nil {
		t.Fatal("snapshot replacement allowed revision rollback")
	}
	incompatible := model.CloneSnapshot(replacement)
	incompatible.Revision++
	incompatible.EnvironmentHash = strings.Repeat("b", 64)
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), incompatible); err == nil {
		t.Fatal("snapshot replacement allowed incompatible environment")
	}
	pendingResult := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), projectionOperation(t, replacement, 2), func(_ model.AcceptedOperation, executeErr error) { pendingResult <- executeErr }); err != nil {
		t.Fatal(err)
	}
	transport.next(t)
	newer := model.CloneSnapshot(replacement)
	newer.Revision++
	if err := network.ReplaceAcknowledgedSnapshot(context.Background(), newer); err == nil {
		t.Fatal("snapshot replacement allowed a pending operation")
	}
	lost := errors.New("test cleanup")
	network.Suspend(lost)
	if executeErr := <-pendingResult; !errors.Is(executeErr, lost) {
		t.Fatalf("pending cleanup error = %v, want %v", executeErr, lost)
	}
}

func executeWhileReceiving(t *testing.T, network *NetworkExecutor, received <-chan protocol.ServerEnvelope, operation model.Operation) model.AcceptedOperation {
	t.Helper()
	results := make(chan operationResult, 1)
	go func() {
		accepted, err := network.Execute(context.Background(), operation)
		results <- operationResult{accepted: accepted, err: err}
	}()
	deadline := time.After(time.Second)
	for {
		select {
		case message := <-received:
			if err := network.Receive(message); err != nil {
				t.Fatalf("receive network message: %v", err)
			}
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			return result.accepted
		case <-deadline:
			t.Fatal("timed out waiting for network execution")
			return model.AcceptedOperation{}
		}
	}
}

type fakeTransport struct {
	sent chan protocol.ClientEnvelope
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{sent: make(chan protocol.ClientEnvelope, 8)}
}

func (transport *fakeTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}

func (transport *fakeTransport) Send(_ context.Context, envelope protocol.ClientEnvelope) error {
	transport.sent <- envelope
	return nil
}

func (transport *fakeTransport) Close(websocket.StatusCode, string) error { return nil }

func (transport *fakeTransport) next(t *testing.T) protocol.ClientEnvelope {
	t.Helper()
	select {
	case envelope := <-transport.sent:
		return envelope
	case <-time.After(time.Second):
		t.Fatal("transport received no message")
		return protocol.ClientEnvelope{}
	}
}

func serverEnvelope(t *testing.T, messageType protocol.ServerType, payload any) protocol.ServerEnvelope {
	t.Helper()
	return protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "server", SessionID: "session", Type: messageType, Payload: mustJSON(t, payload)}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
