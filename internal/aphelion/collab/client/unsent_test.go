package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestNetworkExecutorRetainsUnsentDraft(t *testing.T) {
	for _, reason := range []string{"message limit", "tile limit", "send failure", "stale precondition"} {
		t.Run(reason, func(t *testing.T) {
			snapshot := projectionSnapshot(t)
			operation := projectionOperation(t, snapshot, 1)
			switch reason {
			case "message limit":
				operation.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", protocol.MaxMessageBytes)
			case "tile limit":
				snapshot.MaxX, snapshot.MaxY = 65, 65
				operation = projectionOperation(t, snapshot, 1)
				operation.Changes = make([]model.TileChange, protocol.MaxOperationChanges+1)
				for i := range operation.Changes {
					operation.Changes[i].Coord = model.Coord{X: i%65 + 1, Y: i/65 + 1, Z: 1}
				}
			case "stale precondition":
				// A dependent gesture can still describe a preceding draft after
				// that draft was rejected, while the acknowledged base is current.
				operation.Changes[0].Before = model.CloneTileState(operation.Changes[0].After)
			}
			network, err := NewNetworkExecutor(NewWebSocketTransport(TransportConfig{}), snapshot, operation.ActorID, "session")
			if err != nil {
				t.Fatal(err)
			}
			want := model.CloneOperation(operation)
			_, err = network.Execute(context.Background(), operation)
			if err == nil {
				t.Fatal("invalid/unconnected submission succeeded")
			}
			if reason != "send failure" && errors.Is(err, ErrTransportNotConnected) {
				t.Fatalf("submission escaped local validation: %v", err)
			}
			if network.HasUnacknowledgedOperations() {
				t.Fatal("unsent operation remained pending")
			}
			conflicts := network.Conflicts()
			if len(conflicts) != 1 || !reflect.DeepEqual(conflicts[0].Draft, want) {
				t.Fatal("unsent intent was not retained exactly")
			}
			got, err := network.Snapshot(context.Background())
			if err != nil || !reflect.DeepEqual(got, model.CloneSnapshot(snapshot)) {
				t.Fatalf("failed submission changed authority: %v", err)
			}
			// Returned drafts and input operations must not alias retained intent.
			operation.Changes[0].Coord.X = 999
			conflicts[0].Draft.Changes[0].Coord.X = 998
			if !reflect.DeepEqual(network.Conflicts()[0].Draft, want) {
				t.Fatal("retained draft aliases caller data")
			}
			if _, err := network.RefreshConflict(context.Background(), want.OperationID); err != nil {
				t.Fatal(err)
			}
			if _, err := network.DiscardConflict(context.Background(), want.OperationID); err != nil || len(network.Conflicts()) != 0 {
				t.Fatalf("discard failed: %v", err)
			}
		})
	}
}

func TestWebSocketTransportSubmissionLimits(t *testing.T) {
	snapshot := projectionSnapshot(t)
	snapshot.MaxX, snapshot.MaxY = 65, 65
	operation := projectionOperation(t, snapshot, 1)
	operation.Changes = make([]model.TileChange, protocol.MaxOperationChanges)
	for i := range operation.Changes {
		operation.Changes[i].Coord = model.Coord{X: i%65 + 1, Y: i/65 + 1, Z: 1}
	}
	envelope := func() protocol.ClientEnvelope {
		return protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(operation.OperationID), SessionID: "session", Type: protocol.ClientOperationSubmit, Payload: mustJSON(t, protocol.OperationSubmitPayload{Operation: operation})}
	}
	transport := NewWebSocketTransport(TransportConfig{})
	if err := transport.Send(context.Background(), envelope()); !errors.Is(err, ErrTransportNotConnected) {
		t.Fatalf("4096 changes failed validation: %v", err)
	}
	operation.Changes = append(operation.Changes, model.TileChange{Coord: model.Coord{X: 65, Y: 65, Z: 1}})
	if err := transport.Send(context.Background(), envelope()); err == nil || errors.Is(err, ErrTransportNotConnected) {
		t.Fatalf("4097 changes escaped validation: %v", err)
	}
	operation = projectionOperation(t, snapshot, 1)
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = ""
	padding := protocol.MaxMessageBytes - len(mustJSON(t, envelope()))
	operation.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", padding)
	if got := len(mustJSON(t, envelope())); got != protocol.MaxMessageBytes {
		t.Fatalf("boundary fixture size = %d", got)
	}
	if err := transport.Send(context.Background(), envelope()); !errors.Is(err, ErrTransportNotConnected) {
		t.Fatalf("exact byte boundary failed validation: %v", err)
	}
	operation.Changes[0].After.Prefabs[0].Vars["payload"] += "x"
	if err := transport.Send(context.Background(), envelope()); err == nil || errors.Is(err, ErrTransportNotConnected) {
		t.Fatalf("oversized envelope escaped validation: %v", err)
	}
}

func TestUnsentDraftRebuildOverWebSocket(t *testing.T) {
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
	joined, err := protocol.DecodeServer(mustJSON(t, waitForServerType(t, received, protocol.ServerJoined)))
	if err != nil {
		t.Fatal(err)
	}
	actor := joined.Payload.(*protocol.JoinedPayload).ActorID
	network, err := NewNetworkExecutor(transport, snapshot, actor, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	waitForServerType(t, received, protocol.ServerReplayComplete)
	// Use an existing tile, matching imported editor maps; adding then clearing
	// a sparse absent tile retains an explicit empty tile in the model.
	executeWhileReceiving(t, network, received, projectionOperation(t, snapshot, 1))
	snapshot, err = network.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := projectionOperation(t, snapshot, 1)
	draft.Changes[0].Before = model.CloneTileState(draft.Changes[0].After)
	if _, err := network.Execute(ctx, draft); err == nil {
		t.Fatal("stale precondition submitted")
	}
	rebuilt, err := network.BuildConflictRebuild(ctx, draft.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	accepted := executeWhileReceiving(t, network, received, rebuilt)
	if accepted.Revision != 2 || !accepted.Changes[0].After.Equal(draft.Changes[0].After) || !network.DismissConflict(draft.OperationID) {
		t.Fatal("rebuild did not accept the retained intent exactly once")
	}
	inverse, err := network.BuildInverse(ctx, accepted.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if executeWhileReceiving(t, network, received, inverse).Revision != 3 {
		t.Fatal("rebuild inverse was not accepted")
	}
	got, err := network.Snapshot(ctx)
	wantHash, hashErr := snapshot.Hash()
	gotHash, gotErr := got.Hash()
	if err != nil || hashErr != nil || gotErr != nil || gotHash != wantHash {
		t.Fatal("inverse did not restore the original map hash")
	}
}

func TestUnsentForeignDocumentCannotBecomeConflict(t *testing.T) {
	snapshot := projectionSnapshot(t)
	operation := projectionOperation(t, projectionSnapshot(t), 1)
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, operation.ActorID, "session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := network.Execute(context.Background(), operation); err == nil || len(network.Conflicts()) != 0 {
		t.Fatal("foreign document became a rebuildable conflict")
	}
}
