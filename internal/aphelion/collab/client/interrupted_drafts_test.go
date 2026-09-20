package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

func TestInterruptedDraftSurvivesReplayOrSnapshotRecovery(t *testing.T) {
	for _, recovery := range []string{"accepted replay", "snapshot without draft", "snapshot with draft"} {
		t.Run(recovery, func(t *testing.T) {
			snapshot := projectionSnapshot(t)
			draft := projectionOperation(t, snapshot, 1)
			transport := newFakeTransport()
			network, err := NewNetworkExecutor(transport, snapshot, draft.ActorID, "session")
			if err != nil {
				t.Fatal(err)
			}
			completed := make(chan error, 1)
			if err := network.ExecuteAsync(context.Background(), draft, func(_ model.AcceptedOperation, err error) { completed <- err }); err != nil {
				t.Fatal(err)
			}
			transport.next(t)
			lost := errors.New("connection interrupted after queueing")
			network.Suspend(lost)
			if err := <-completed; !errors.Is(err, lost) {
				t.Fatalf("interruption callback = %v", err)
			}
			conflicts := network.Conflicts()
			if network.HasUnacknowledgedOperations() || len(conflicts) != 1 || conflicts[0].Code != "delivery_unconfirmed" || !reflect.DeepEqual(conflicts[0].Draft, model.CloneOperation(draft)) {
				t.Fatal("interruption lost intent or misrepresented its delivery status")
			}
			resumed := newFakeTransport()
			if err := network.Resume(resumed); err != nil {
				t.Fatal(err)
			}
			acceptedOperation := draft
			if recovery == "snapshot without draft" {
				acceptedOperation = projectionOperation(t, snapshot, 2)
			}
			accepted := model.AcceptedOperation{Operation: acceptedOperation, Revision: 1, AcceptedAt: time.Unix(1, 0)}
			authority := snapshotWithOperation(t, snapshot, accepted)
			hash, err := authority.Hash()
			if err != nil {
				t.Fatal(err)
			}
			if recovery == "accepted replay" {
				if err := network.Receive(serverEnvelope(t, protocol.ServerOperationAccepted, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})); err != nil {
					t.Fatal(err)
				}
				if len(network.Conflicts()) != 0 {
					t.Fatal("verified acceptance did not resolve the uncertain draft")
				}
			} else {
				if err := network.ReplaceAcknowledgedSnapshot(context.Background(), authority); err != nil {
					t.Fatal(err)
				}
				if conflicts := network.Conflicts(); len(conflicts) != 1 || !reflect.DeepEqual(conflicts[0].Draft, model.CloneOperation(draft)) {
					t.Fatal("snapshot fallback discarded unresolved intent")
				}
				rebuilt, err := network.BuildConflictRebuild(context.Background(), draft.OperationID)
				if recovery == "snapshot with draft" {
					if err == nil || !strings.Contains(err.Error(), "already present") {
						t.Fatalf("rebuild duplicated intent already present in authority: %v", err)
					}
				} else if err != nil || rebuilt.BaseRevision != 1 || rebuilt.BaseMapHash != hash || rebuilt.OperationID == draft.OperationID || !rebuilt.Changes[0].After.Equal(draft.Changes[0].After) || !rebuilt.Changes[0].Before.Equal(tileAt(authority, 1)) {
					t.Fatalf("rebuild did not preserve intent against fresh authority: %v", err)
				}
			}
			if len(resumed.sent) != 0 {
				t.Fatal("recovery automatically resubmitted uncertain work")
			}
			current, err := network.Snapshot(context.Background())
			if err != nil || !reflect.DeepEqual(current, model.CloneSnapshot(authority)) {
				t.Fatalf("draft recovery changed acknowledged state: %v", err)
			}
		})
	}
}

func TestLowerServerByteLimitRetainsQueuedDraftOnDisconnect(t *testing.T) {
	baseURL, sessionID, token, snapshot, shutdown := startClientTestServiceWithConfig(t, server.ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"}, Limits: server.Limits{MaxWebSocketMessageBytes: 1024},
	})
	defer shutdown()
	transport := NewWebSocketTransport(TransportConfig{})
	received := make(chan protocol.ServerEnvelope, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.Connect(ctx, protocol.JoinRequest{BaseURL: baseURL, Origin: "http://127.0.0.1", Token: token, SessionID: sessionID}, func(message protocol.ServerEnvelope) { received <- message }); err != nil {
		t.Fatal(err)
	}
	joined, err := protocol.DecodeServer(mustJSON(t, waitForServerType(t, received, protocol.ServerJoined)))
	if err != nil {
		t.Fatal(err)
	}
	waitForServerType(t, received, protocol.ServerReplayComplete)
	network, err := NewNetworkExecutor(transport, snapshot, joined.Payload.(*protocol.JoinedPayload).ActorID, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	draft := projectionOperation(t, snapshot, 1)
	draft.Changes[0].After.Prefabs[0].Vars["payload"] = strings.Repeat("x", 2048)
	completed := make(chan error, 1)
	if err := network.ExecuteAsync(ctx, draft, func(_ model.AcceptedOperation, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	lost := transport.Wait(ctx)
	if websocket.CloseStatus(lost) != websocket.StatusMessageTooBig {
		t.Fatalf("server did not close the oversized queued request: %v", lost)
	}
	// SessionClient's connection watcher uses this same suspension boundary.
	network.Suspend(lost)
	if err := <-completed; !errors.Is(err, lost) {
		t.Fatalf("pending callback = %v, want disconnect", err)
	}
	conflicts := network.Conflicts()
	if len(conflicts) != 1 || conflicts[0].Code != "delivery_unconfirmed" || !conflicts[0].Draft.Changes[0].After.Equal(draft.Changes[0].After) || network.HasUnacknowledgedOperations() {
		t.Fatal("lower server limit lost queued intent")
	}
}
