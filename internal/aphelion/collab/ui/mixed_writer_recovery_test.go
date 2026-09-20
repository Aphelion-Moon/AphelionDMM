package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
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

func TestMultipleWritersRecoverMixedPendingEditsDuringLoad(t *testing.T) {
	verifyMixedWriterRecoveryCases(t, false)
}

func TestMultipleSlowWritersRecoverMixedPendingEditsDuringLoad(t *testing.T) {
	verifyMixedWriterRecoveryCases(t, true)
}

func verifyMixedWriterRecoveryCases(t *testing.T, slowConsumer bool) {
	t.Helper()
	for _, backend := range []string{"memory", "sqlite"} {
		for _, snapshot := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/snapshot=%t", backend, snapshot), func(t *testing.T) {
				var store server.SessionStore = server.NewMemoryStore()
				if backend == "sqlite" {
					value, err := sqlite.Open(filepath.Join(t.TempDir(), "mixed-writers.db"))
					if err != nil {
						t.Fatal(err)
					}
					store = value
				}
				t.Cleanup(func() { _ = store.Close() })
				verifyMixedWriterRecovery(t, store, snapshot, slowConsumer)
			})
		}
	}
}

type mixedWriter struct {
	client             *SessionClient
	network            *collabclient.NetworkExecutor
	first              *mixedWriterTransport
	resume             *heldWriterReconnect
	transports, offers atomic.Int32
	credential         string
	drafts             []model.Operation
	resolved           map[model.OperationID]bool
	completed          chan mixedWriterCompletion
}

type mixedWriterCompletion struct {
	id       model.OperationID
	accepted model.AcceptedOperation
	err      error
}

func verifyMixedWriterRecovery(t *testing.T, store server.SessionStore, snapshotFallback, slowConsumer bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	scenario, err := loadscenario.GenerateConcurrent(loadscenario.ConcurrentConfig{Config: loadscenario.Config{
		Seed: 20260920, Clients: 1, Operations: 64, MaxX: 96, MaxY: 1, TargetOperationsPerSecond: 40,
	}})
	if err != nil {
		t.Fatal(err)
	}
	expected := model.CloneSnapshot(scenario.Initial)
	used := make(map[model.Coord]bool)
	healthyValues := make(map[model.Coord]model.TileState)
	known := make(map[model.OperationID]bool)
	for _, intent := range scenario.Intents {
		change := intent.Operation.Changes[0]
		used[change.Coord], known[intent.Operation.OperationID] = true, true
		healthyValues[change.Coord] = change.After
		expected.Tiles = append(expected.Tiles, model.Tile{Coord: change.Coord, State: model.CloneTileState(change.After)})
	}
	var spare []model.Coord
	for x := 1; x <= scenario.Initial.MaxX; x++ {
		coord := model.Coord{X: x, Y: 1, Z: 1}
		if !used[coord] {
			spare = append(spare, coord)
		}
	}
	config := server.ServiceConfig{Store: store, AllowedOrigins: []string{"http://127.0.0.1"}, PresenceInterval: 16 * time.Millisecond}
	var gates []*slowWriteGate
	if slowConsumer {
		config.Limits.DurableQueueDepth = 8
		for range 2 {
			gate := &slowWriteGate{ctx: ctx, blocked: make(chan struct{}), release: make(chan struct{})}
			defer gate.unblock()
			gates = append(gates, gate)
		}
	}
	service := server.NewService(config)
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	var connections atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slowConsumer && r.URL.Path == "/v1/collaboration" {
			if index := connections.Add(1) - 2; index >= 0 && index < int32(len(gates)) {
				w = &slowResponseWriter{ResponseWriter: w, gate: gates[index]}
			}
		}
		service.Handler().ServeHTTP(w, r)
	}))
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
	var writers []*mixedWriter
	for index := range 2 {
		writer := &mixedWriter{resolved: make(map[model.OperationID]bool), completed: make(chan mixedWriterCompletion, 16)}
		writer.first = &mixedWriterTransport{SessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}), outcomes: make(map[model.OperationID]string), observed: make(chan mixedWriterObservation, 32), ended: make(chan error, 1)}
		writer.resume = &heldWriterReconnect{SessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}), entered: make(chan struct{}), release: make(chan struct{})}
		defer writer.resume.unblock()
		writer.client = NewSessionClient(SessionClientConfig{NewTransport: func() SessionTransport {
			var transport SessionTransport
			switch writer.transports.Add(1) {
			case 1:
				transport = writer.first
			case 2:
				transport = writer.resume
			default:
				transport = collabclient.NewWebSocketTransport(collabclient.TransportConfig{})
			}
			return &countedWriterTransport{SessionTransport: transport, offers: &writer.offers}
		}})
		t.Cleanup(func() { _ = writer.client.Leave(context.Background()) })
		invite, err := owner.CreateInvitation(ctx, InvitationRoleEditor, fmt.Sprintf("Interrupted writer %d", index))
		if err != nil {
			t.Fatal(err)
		}
		// Configure immutable outcome identities before the transport starts.
		for draftIndex := range 16 {
			draft := sessionConflictOperation(t, scenario.Initial, scenario.Actors[0])
			outcome := []string{"committed", "queued", "rejected"}[draftIndex%3]
			if outcome == "rejected" {
				draft.Changes[0].Coord = scenario.Intents[index*5+draftIndex/3].Operation.Changes[0].Coord
			} else {
				draft.Changes[0].Coord, spare = spare[0], spare[1:]
			}
			writer.first.outcomes[draft.OperationID] = outcome
			writer.drafts = append(writer.drafts, draft)
			if outcome == "committed" {
				known[draft.OperationID] = true
				change := draft.Changes[0]
				expected.Tiles = append(expected.Tiles, model.Tile{Coord: change.Coord, State: model.CloneTileState(change.After)})
			}
		}
		if err := writer.client.Join(ctx, invite); err != nil {
			t.Fatal(err)
		}
		writer.client.mutex.Lock()
		writer.credential = writer.client.resumptionToken
		actor := writer.client.actorID
		writer.client.mutex.Unlock()
		for i := range writer.drafts {
			writer.drafts[i].ActorID = actor
		}
		writer.network = writer.client.NetworkExecutor()
		writers = append(writers, writer)
	}
	var sent atomic.Int32
	loadDone := make(chan error, 1)
	go func() { loadDone <- sendScheduledSessionEdits(ctx, owner, scenario, &sent) }()
	// Both writers remain at revision zero while the server acquires the ten
	// conflicting before-values. All other writer coordinates are disjoint.
	waitWriterCondition(t, ctx, func() bool { return owner.Status().Revision >= 10 })
	// Observe each injected outcome before enqueueing the next offer. The
	// transport still withholds every callback, leaving sixteen pending drafts
	// per writer without overflowing the small queue before the write stall.
	for _, writer := range writers {
		seen := make(map[model.OperationID]bool)
		for _, draft := range writer.drafts {
			if err := writer.network.ExecuteAsync(ctx, draft, func(accepted model.AcceptedOperation, err error) {
				writer.completed <- mixedWriterCompletion{id: draft.OperationID, accepted: accepted, err: err}
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case observation := <-writer.first.observed:
				if seen[observation.id] || observation.outcome != writer.first.outcomes[observation.id] {
					t.Fatal("unexpected or repeated injected outcome")
				}
				seen[observation.id] = true
				if rejection := observation.rejection; rejection != nil {
					hash, found, err := store.RevisionHash(ctx, scenario.Initial.DocumentID, rejection.Revision)
					if err != nil || !found || hash != rejection.MapHash || rejection.Code != "precondition_failed" || len(rejection.AuthoritativeValues) != 1 {
						t.Fatalf("rejection did not identify durable precondition authority: %v", err)
					}
					for _, draft := range writer.drafts {
						if draft.OperationID == rejection.OperationID {
							value := rejection.AuthoritativeValues[0]
							if value.Coord != draft.Changes[0].Coord || !value.State.Equal(healthyValues[value.Coord]) {
								t.Fatal("rejection did not preserve the healthy edit's authoritative value")
							}
							break
						}
					}
				}
			case result := <-writer.completed:
				t.Fatalf("writer completed before interruption: %s: %v", result.id, result.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if writer.offers.Load() != 16 || !writer.network.HasUnacknowledgedOperations() {
			t.Fatal("all sixteen writer offers were not pending together")
		}
	}
	waitWriterCondition(t, ctx, func() bool { return sent.Load() >= 24 && owner.Status().Revision >= 36 })
	if slowConsumer {
		for _, gate := range gates {
			gate.armed.Store(true)
			waitWriterSignal(t, ctx, gate.blocked)
		}
		// Count from both blocked writes. Twelve further accepted events exceed
		// each eight-entry durable queue even with an event held by its writer.
		blockedRevision := owner.Status().Revision
		waitWriterCondition(t, ctx, func() bool { return owner.Status().Revision >= blockedRevision+12 })
		for _, gate := range gates {
			gate.unblock()
		}
		for _, writer := range writers {
			select {
			case err := <-writer.first.ended:
				if websocket.CloseStatus(err) != server.CloseSlowConsumer {
					t.Fatalf("pending writer did not close from durable queue overflow: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("queue overflow did not disconnect both pending writers")
			}
		}
		t.Logf("both pending writers closed with slow-consumer code %d after revision %d", server.CloseSlowConsumer, blockedRevision+12)
	} else {
		for _, writer := range writers {
			if err := writer.first.Close(websocket.StatusInternalError, "injected concurrent writer interruption"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, writer := range writers {
		seen := make(map[model.OperationID]bool)
		for range writer.drafts {
			select {
			case result := <-writer.completed:
				if result.err == nil || seen[result.id] || writer.first.outcomes[result.id] == "" {
					t.Fatal("interruption did not release each distinct writer callback with an error")
				}
				seen[result.id] = true
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		waitWriterSignal(t, ctx, writer.resume.entered)
		verifyMixedWriterDrafts(t, writer, true)
		for _, draft := range writer.drafts {
			_, found, err := store.LookupOperation(ctx, scenario.Initial.DocumentID, draft.OperationID)
			if err != nil || found != (writer.first.outcomes[draft.OperationID] == "committed") {
				t.Fatalf("durable outcome differs for %s: %v", draft.OperationID, err)
			}
		}
	}
	if snapshotFallback {
		checkpoint, err := owner.NetworkExecutor().Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSnapshot(ctx, checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	for _, writer := range writers {
		writer.resume.unblock()
	}
	for index, writer := range writers {
		waitWriterCondition(t, ctx, func() bool {
			writer.client.mutex.Lock()
			rotated := writer.client.resumptionToken != "" && writer.client.resumptionToken != writer.credential
			writer.client.mutex.Unlock()
			return rotated && writer.client.Status().State == collabclient.StateCaughtUp
		})
		if writer.client.Status().Revision >= 76 || sent.Load() >= 64 {
			t.Fatal("writers did not recover during continued healthy offers")
		}
		t.Logf("writer %d recovered at revision %d with %d/64 healthy offers sent", index, writer.client.Status().Revision, sent.Load())
	}
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	clients := []*SessionClient{owner, writers[0].client, writers[1].client}
	for _, client := range clients {
		waitForClientRevision(t, ctx, client, 76)
		current, err := client.NetworkExecutor().Snapshot(ctx)
		if err != nil || mustSnapshotHash(t, current) != mustSnapshotHash(t, expected) {
			t.Fatal("initial recovered authority differs between clients")
		}
	}
	for index, writer := range writers {
		wantTransports := int32(2)
		if snapshotFallback {
			wantTransports++
		}
		if writer.offers.Load() != 16 || writer.transports.Load() != wantTransports || writer.client.NetworkExecutor() != writer.network {
			t.Fatalf("recovery offers=%d (want 16), transports=%d (want %d), same executor=%t", writer.offers.Load(), writer.transports.Load(), wantTransports, writer.client.NetworkExecutor() == writer.network)
		}
		// Each prior writer submitted twenty explicit recovery operations. Their
		// callbacks do not imply this writer has received those broadcasts yet.
		waitForClientRevision(t, ctx, writer.client, model.Revision(76+index*20))
		verifyMixedWriterDrafts(t, writer, snapshotFallback)
		for _, draft := range writer.drafts {
			outcome := writer.first.outcomes[draft.OperationID]
			if outcome == "committed" {
				if snapshotFallback {
					if _, err := writer.network.BuildConflictRebuild(ctx, draft.OperationID); err == nil || !strings.Contains(err.Error(), "already present") {
						t.Fatalf("committed snapshot intent became rebuildable: %v", err)
					}
					if _, err := writer.client.DiscardConflict(ctx, draft.OperationID); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				before, err := writer.network.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := writer.client.RebuildConflict(ctx, draft.OperationID, func(accepted model.AcceptedOperation, err error) {
					writer.completed <- mixedWriterCompletion{accepted: accepted, err: err}
				}); err != nil {
					t.Fatal(err)
				}
				var result mixedWriterCompletion
				select {
				case result = <-writer.completed:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				accepted := result.accepted
				wantBefore := model.TileState{}
				for _, tile := range before.Tiles {
					if tile.Coord == draft.Changes[0].Coord {
						wantBefore = tile.State
					}
				}
				if result.err != nil || accepted.OperationID == draft.OperationID || accepted.BaseRevision != before.Revision || accepted.BaseMapHash != mustSnapshotHash(t, before) || len(accepted.Changes) != 1 || accepted.Changes[0].Coord != draft.Changes[0].Coord || !accepted.Changes[0].Before.Equal(wantBefore) || !accepted.Changes[0].After.Equal(draft.Changes[0].After) {
					t.Fatalf("explicit rebuild changed draft or authority: %v", result.err)
				}
				inverse, err := writer.network.BuildInverse(ctx, accepted.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				undone, err := writer.network.Execute(ctx, inverse)
				if err != nil {
					t.Fatal(err)
				}
				known[accepted.OperationID], known[undone.OperationID] = true, true
				if outcome == "queued" {
					// A v1 inverse restores empty values, not coordinate omission.
					expected.Tiles = append(expected.Tiles, model.Tile{Coord: draft.Changes[0].Coord})
				}
			}
			writer.resolved[draft.OperationID] = true
			verifyMixedWriterDrafts(t, writer, snapshotFallback)
		}
		if writer.offers.Load() != 36 {
			t.Fatal("writer did not send exactly sixteen original and twenty explicit recovery operations")
		}
	}
	state, err := store.LoadRecovery(ctx, scenario.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := state.Restore()
	if err != nil || state.HeadRevision != 116 || len(state.Operations) != len(known) || mustSnapshotHash(t, document.Snapshot()) != mustSnapshotHash(t, expected) {
		t.Fatalf("full durable reconstruction differs after mixed recovery: %v", err)
	}
	for _, operation := range state.Operations {
		if !known[operation.OperationID] {
			t.Fatal("unknown or repeated durable operation")
		}
		delete(known, operation.OperationID)
	}
	for _, client := range clients {
		waitForClientRevision(t, ctx, client, 116)
		current, err := client.NetworkExecutor().Snapshot(ctx)
		if err != nil || mustSnapshotHash(t, current) != mustSnapshotHash(t, expected) {
			t.Fatal("final clients disagree after all explicit rebuilds and inverses")
		}
	}
	t.Log("accounted for 64 healthy edits, 12 original commits, 10 lost rejections, 10 queued-unsent drafts and 40 explicit recovery operations; final revision 116")
}

func verifyMixedWriterDrafts(t *testing.T, writer *mixedWriter, retainCommitted bool) {
	t.Helper()
	want := make(map[model.OperationID]model.Operation)
	for _, draft := range writer.drafts {
		if !writer.resolved[draft.OperationID] && (retainCommitted || writer.first.outcomes[draft.OperationID] != "committed") {
			want[draft.OperationID] = draft
		}
	}
	conflicts := writer.network.Conflicts()
	if writer.network.HasUnacknowledgedOperations() || len(conflicts) != len(want) {
		t.Fatalf("retained draft count = %d, want %d, or pending callbacks remain", len(conflicts), len(want))
	}
	for _, conflict := range conflicts {
		draft, exists := want[conflict.OperationID]
		if !exists || conflict.Code != "delivery_unconfirmed" || !reflect.DeepEqual(conflict.Draft, model.CloneOperation(draft)) {
			t.Fatal("recovery lost, changed or incorrectly resolved a pending draft")
		}
		delete(want, conflict.OperationID)
	}
}

type mixedWriterObservation struct {
	id        model.OperationID
	outcome   string
	rejection *protocol.OperationRejectedPayload
}

type mixedWriterTransport struct {
	SessionTransport
	outcomes map[model.OperationID]string
	observed chan mixedWriterObservation
	ended    chan error
}

func (transport *mixedWriterTransport) Wait(ctx context.Context) error {
	err := transport.SessionTransport.Wait(ctx)
	select {
	case transport.ended <- err:
	default:
	}
	return err
}

func (transport *mixedWriterTransport) Connect(ctx context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	return transport.SessionTransport.Connect(ctx, request, func(envelope protocol.ServerEnvelope) {
		switch envelope.Type {
		case protocol.ServerOperationAccepted:
			decoded, err := decodeServerEnvelope(envelope)
			if err == nil {
				id := decoded.Payload.(*protocol.OperationAcceptedPayload).Operation.OperationID
				if transport.outcomes[id] != "" {
					transport.observed <- mixedWriterObservation{id: id, outcome: "committed"}
				}
			}
			return // Preserve the acknowledged prefix while healthy edits continue.
		case protocol.ServerOperationRejected:
			decoded, err := decodeServerEnvelope(envelope)
			if err == nil {
				rejection := decoded.Payload.(*protocol.OperationRejectedPayload)
				transport.observed <- mixedWriterObservation{id: rejection.OperationID, outcome: "rejected", rejection: rejection}
				return
			}
		}
		receive(envelope)
	})
}

func (transport *mixedWriterTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	if envelope.Type == protocol.ClientOperationSubmit {
		var payload protocol.OperationSubmitPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		if transport.outcomes[payload.Operation.OperationID] == "queued" {
			transport.observed <- mixedWriterObservation{id: payload.Operation.OperationID, outcome: "queued"}
			return nil // Successful local enqueue, injected loss before socket write.
		}
	}
	return transport.SessionTransport.Send(ctx, envelope)
}
