package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	collabclient "sdmm/internal/aphelion/collab/client"
	loadscenario "sdmm/internal/aphelion/collab/load"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

func TestSessionWriterRecoversUncertainEditDuringLoad(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, committed := range []bool{false, true} {
			for _, snapshot := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/committed=%t/snapshot=%t", backend, committed, snapshot), func(t *testing.T) {
					var store server.SessionStore = server.NewMemoryStore()
					if backend == "sqlite" {
						value, err := sqlite.Open(filepath.Join(t.TempDir(), "writer.sqlite"))
						if err != nil {
							t.Fatal(err)
						}
						store = value
					}
					t.Cleanup(func() { _ = store.Close() })
					verifyWriterRecoveryDuringLoad(t, store, committed, snapshot)
				})
			}
		}
	}
}

func verifyWriterRecoveryDuringLoad(t *testing.T, store server.SessionStore, committed, snapshotFallback bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scenario, err := loadscenario.GenerateConcurrent(loadscenario.ConcurrentConfig{Config: loadscenario.Config{
		Seed: 20260920, Clients: 1, Operations: 32, MaxX: 33, MaxY: 1, TargetOperationsPerSecond: 40,
	}})
	if err != nil {
		t.Fatal(err)
	}
	expected := model.CloneSnapshot(scenario.Initial)
	used := make(map[model.Coord]bool)
	for _, intent := range scenario.Intents {
		change := intent.Operation.Changes[0]
		used[change.Coord] = true
		expected.Tiles = append(expected.Tiles, model.Tile{Coord: change.Coord, State: model.CloneTileState(change.After)})
	}
	draft := sessionConflictOperation(t, scenario.Initial, scenario.Actors[0])
	for x := 1; x <= scenario.Initial.MaxX; x++ {
		coord := model.Coord{X: x, Y: 1, Z: 1}
		if !used[coord] {
			draft.Changes[0].Coord = coord
			break
		}
	}
	service := server.NewService(server.ServiceConfig{Store: store, AllowedOrigins: []string{"http://127.0.0.1"}, PresenceInterval: 16 * time.Millisecond})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	backend := httptest.NewServer(service.Handler())
	t.Cleanup(backend.Close)
	launch, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	owner := NewSessionClient(SessionClientConfig{})
	t.Cleanup(func() { _ = owner.Leave(context.Background()) })
	invitation, err := owner.Create(ctx, backend.URL, launch, scenario.Initial)
	if err != nil {
		t.Fatal(err)
	}
	invitation.Origin = "http://127.0.0.1"
	if err := owner.Join(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	invite, err := owner.CreateInvitation(ctx, InvitationRoleEditor, "Interrupted writer")
	if err != nil {
		t.Fatal(err)
	}
	first := &uncertainWriterTransport{SessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}),
		target: draft.OperationID, committed: committed, intercepted: make(chan struct{})}
	resume := &heldWriterReconnect{SessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}), entered: make(chan struct{}), release: make(chan struct{})}
	defer resume.unblock()
	var transports atomic.Int32
	var writerOffers atomic.Int32
	writer := NewSessionClient(SessionClientConfig{NewTransport: func() SessionTransport {
		var transport SessionTransport
		switch transports.Add(1) {
		case 1:
			transport = first
		case 2:
			transport = resume
		default:
			transport = collabclient.NewWebSocketTransport(collabclient.TransportConfig{})
		}
		return &countedWriterTransport{SessionTransport: transport, offers: &writerOffers}
	}})
	t.Cleanup(func() { _ = writer.Leave(context.Background()) })
	if err := writer.Join(ctx, invite); err != nil {
		t.Fatal(err)
	}
	writer.mutex.Lock()
	credential, actor := writer.resumptionToken, writer.actorID
	writer.mutex.Unlock()
	draft.ActorID = actor
	network := writer.NetworkExecutor()
	pending := make(chan error, 1)
	if err := network.ExecuteAsync(ctx, draft, func(_ model.AcceptedOperation, err error) { pending <- err }); err != nil {
		t.Fatal(err)
	}
	waitWriterSignal(t, ctx, first.intercepted)
	var sent atomic.Int32
	loadDone := make(chan error, 1)
	go func() { loadDone <- sendScheduledSessionEdits(ctx, owner, scenario, &sent) }()
	waitWriterCondition(t, ctx, func() bool { return sent.Load() >= 8 && owner.Status().Revision >= 8 })
	if err := first.Close(websocket.StatusInternalError, "injected writer interruption"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-pending:
		if err == nil {
			t.Fatal("writer reported success without receiving its acknowledgement")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitWriterSignal(t, ctx, resume.entered)
	conflicts := network.Conflicts()
	if network.HasUnacknowledgedOperations() || len(conflicts) != 1 || conflicts[0].Code != "delivery_unconfirmed" || !reflect.DeepEqual(conflicts[0].Draft, model.CloneOperation(draft)) {
		t.Fatal("disconnect lost or incorrectly resolved the uncertain draft")
	}
	_, found, err := store.LookupOperation(ctx, scenario.Initial.DocumentID, draft.OperationID)
	if err != nil || found != committed {
		t.Fatalf("pre-reconnect durable outcome = %t, want %t: %v", found, committed, err)
	}
	if snapshotFallback {
		acknowledged, err := network.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		waitWriterCondition(t, ctx, func() bool { return owner.Status().Revision >= acknowledged.Revision+4 })
		checkpoint, err := owner.NetworkExecutor().Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSnapshot(ctx, checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	resume.unblock()
	finalRevision := model.Revision(len(scenario.Intents))
	if committed {
		finalRevision++
		change := draft.Changes[0]
		expected.Tiles = append(expected.Tiles, model.Tile{Coord: change.Coord, State: model.CloneTileState(change.After)})
	}
	waitWriterCondition(t, ctx, func() bool {
		writer.mutex.Lock()
		rotated := writer.resumptionToken != "" && writer.resumptionToken != credential
		writer.mutex.Unlock()
		return rotated && writer.Status().State == collabclient.StateCaughtUp
	})
	if recovered := writer.Status().Revision; recovered >= finalRevision || sent.Load() >= int32(len(scenario.Intents)) {
		t.Fatal("writer did not reconnect while offers were still in progress")
	} else {
		t.Logf("writer reconnected at revision %d with %d/%d healthy offers sent", recovered, sent.Load(), len(scenario.Intents))
	}
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, client := range []*SessionClient{owner, writer} {
		waitForClientRevision(t, ctx, client, finalRevision)
		current, err := client.NetworkExecutor().Snapshot(ctx)
		if err != nil || mustSnapshotHash(t, current) != mustSnapshotHash(t, expected) {
			t.Fatal("writer/healthy editor did not converge to all offered changes")
		}
	}
	wantTransports := int32(2)
	if snapshotFallback {
		wantTransports++
	}
	if transports.Load() != wantTransports || writer.NetworkExecutor() != network {
		t.Fatal("unexpected reconnect attempts or replaced executor")
	}
	if writerOffers.Load() != 1 {
		t.Fatal("recovery automatically resubmitted uncertain intent")
	}
	conflicts = network.Conflicts()
	if committed && !snapshotFallback {
		if len(conflicts) != 0 {
			t.Fatal("verified replay did not resolve the committed uncertain draft")
		}
	} else if len(conflicts) != 1 || conflicts[0].OperationID != draft.OperationID || conflicts[0].Code != "delivery_unconfirmed" || !reflect.DeepEqual(conflicts[0].Draft, model.CloneOperation(draft)) {
		t.Fatal("recovery discarded or changed unresolved intent")
	}
	var next model.Operation
	if !committed {
		next, err = network.BuildConflictRebuild(ctx, draft.OperationID)
		if err != nil || next.OperationID == draft.OperationID || !next.Changes[0].After.Equal(draft.Changes[0].After) {
			t.Fatalf("explicit rebuild did not preserve the absent intent: %v", err)
		}
	} else {
		if snapshotFallback {
			if _, err := network.BuildConflictRebuild(ctx, draft.OperationID); err == nil || !strings.Contains(err.Error(), "already present") {
				t.Fatalf("snapshot recovery allowed duplicate intent: %v", err)
			}
		}
		current, err := network.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		next = sessionReplacementOperation(t, current, actor, "/obj/after_writer_recovery")
	}
	accepted, err := network.Execute(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		if _, err := network.DiscardConflict(ctx, draft.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	inverse, err := network.BuildInverse(ctx, accepted.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	undone, err := network.Execute(ctx, inverse)
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[model.OperationID]bool)
	for _, intent := range scenario.Intents {
		known[intent.Operation.OperationID] = true
	}
	known[accepted.OperationID], known[undone.OperationID] = true, true
	if committed {
		known[draft.OperationID] = true
	}
	state, err := store.LoadRecovery(ctx, scenario.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := state.Restore()
	if err != nil || len(state.Operations) != len(known) || state.HeadRevision != finalRevision+2 {
		t.Fatalf("durable operation accounting differs: %v", err)
	}
	for _, operation := range state.Operations {
		if !known[operation.OperationID] {
			t.Fatal("unknown or repeated edit in durable history")
		}
		delete(known, operation.OperationID)
	}
	expectedAfterInverse := model.CloneSnapshot(expected)
	if !committed {
		// Protocol v1 swaps tile values; it does not remove a coordinate first
		// introduced by a forward edit. Hashes distinguish omission from an
		// explicit empty tile, so retain that precise representation here.
		expectedAfterInverse.Tiles = append(expectedAfterInverse.Tiles, model.Tile{Coord: draft.Changes[0].Coord, State: model.TileState{}})
	}
	if mustSnapshotHash(t, document.Snapshot()) != mustSnapshotHash(t, expectedAfterInverse) {
		t.Fatal("post-recovery inverse or durable reconstruction changed unrelated work")
	}
	for _, client := range []*SessionClient{owner, writer} {
		waitForClientRevision(t, ctx, client, finalRevision+2)
		current, err := client.NetworkExecutor().Snapshot(ctx)
		if err != nil || mustSnapshotHash(t, current) != mustSnapshotHash(t, expectedAfterInverse) {
			t.Fatal("post-recovery inverse did not converge")
		}
	}
	if writerOffers.Load() != 3 {
		t.Fatal("writer sent more than its initial offer and two explicit recovery edits")
	}
	t.Logf("accounted for 32 healthy offers, committed draft=%t, two explicit recovery edits; final revision %d", committed, finalRevision+2)
}

// Send on fixed absolute deadlines without waiting for an operation outcome.
// Authentic revision-zero bases remain valid because these cells are distinct.
func sendScheduledSessionEdits(ctx context.Context, owner *SessionClient, scenario loadscenario.ConcurrentScenario, sent *atomic.Int32) error {
	owner.mutex.Lock()
	transport, actor, sessionID := owner.transport, owner.actorID, owner.sessionID
	owner.mutex.Unlock()
	epoch := time.Now().Add(10 * time.Millisecond)
	for index, intent := range scenario.Intents {
		timer := time.NewTimer(time.Until(epoch.Add(time.Duration(index) * time.Second / time.Duration(scenario.Config.TargetOperationsPerSecond))))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		operation := model.CloneOperation(intent.Operation)
		operation.ActorID = actor
		payload, err := json.Marshal(protocol.OperationSubmitPayload{Operation: operation})
		if err != nil {
			return err
		}
		if err := transport.Send(ctx, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(operation.OperationID), SessionID: sessionID, Type: protocol.ClientOperationSubmit, Payload: payload}); err != nil {
			return err
		}
		sent.Add(1)
		if index%2 == 0 {
			cursor := operation.Changes[0].Coord
			if err := owner.PublishPresence(ctx, &cursor, nil, "active"); err != nil {
				return err
			}
		}
	}
	return nil
}

type uncertainWriterTransport struct {
	SessionTransport
	target      model.OperationID
	committed   bool
	intercepted chan struct{}
	once        sync.Once
	withholding atomic.Bool
}

type countedWriterTransport struct {
	SessionTransport
	offers *atomic.Int32
}

func (transport *countedWriterTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	if envelope.Type == protocol.ClientOperationSubmit {
		transport.offers.Add(1)
	}
	return transport.SessionTransport.Send(ctx, envelope)
}

func (transport *uncertainWriterTransport) Connect(ctx context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	return transport.SessionTransport.Connect(ctx, request, func(envelope protocol.ServerEnvelope) {
		if envelope.Type == protocol.ServerOperationAccepted {
			decoded, err := decodeServerEnvelope(envelope)
			if err == nil && decoded.Payload.(*protocol.OperationAcceptedPayload).Operation.OperationID == transport.target && transport.committed {
				transport.withholding.Store(true)
				transport.once.Do(func() { close(transport.intercepted) })
			}
			if transport.withholding.Load() {
				return // Retain the contiguous prefix; never deliver a suffix with a gap.
			}
		}
		receive(envelope)
	})
}

func (transport *uncertainWriterTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	if transport.committed || envelope.Type != protocol.ClientOperationSubmit {
		return transport.SessionTransport.Send(ctx, envelope)
	}
	transport.once.Do(func() { close(transport.intercepted) })
	// Model successful local enqueue followed by connection loss before the
	// writer reaches the socket. A Send error would instead prove a local
	// submission failure and would not exercise uncertain queued delivery.
	return nil
}

type heldWriterReconnect struct {
	SessionTransport
	entered, release chan struct{}
	releaseOnce      sync.Once
}

func (transport *heldWriterReconnect) unblock() {
	transport.releaseOnce.Do(func() { close(transport.release) })
}

func (transport *heldWriterReconnect) Connect(ctx context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	close(transport.entered)
	select {
	case <-transport.release:
		return transport.SessionTransport.Connect(ctx, request, receive)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitWriterSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func waitWriterCondition(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
