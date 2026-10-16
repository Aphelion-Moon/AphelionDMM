package server

import (
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// A fresh socket for the same actor restarts its presence sequence at 1, as a
// relaunched editor does. The server must apply it and keep both sockets open:
// stale presence is lossy data, never grounds to close a connection that may
// carry durable edits.
func TestPresenceSequenceRestartOnNewSocketIsAppliedWithoutClosing(t *testing.T) {
	t.Parallel()

	service, created, testServer := startHTTPTestSession(t)
	first, resumption := connectTestClientWithCredential(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = first.CloseNow() }()
	sendPresenceBurst(t, first, created.SessionID, "first", 1, 5)
	requireAlive(t, first, created.SessionID, "first-alive")

	second, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, resumption, 0)
	defer func() { _ = second.CloseNow() }()
	cursor := model.Coord{X: 2, Y: 1, Z: 1}
	writeClientEnvelope(t, second, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "second-1", SessionID: created.SessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: 1, Cursor: &cursor, Status: "active"})
	requireAlive(t, second, created.SessionID, "second-alive")
	presence := currentPresence(t, service, created.SessionID)
	if len(presence) != 1 || presence[0].Sequence != 1 || presence[0].Cursor == nil || *presence[0].Cursor != cursor {
		t.Fatalf("presence after replacement socket = %#v, want the replacement's sequence 1 at %v", presence, cursor)
	}

	// A late update from the lingering older socket is dropped, not applied,
	// and the older socket stays open.
	sendPresenceBurst(t, first, created.SessionID, "first-late", 6, 1)
	requireAlive(t, first, created.SessionID, "first-still-alive")
	presence = currentPresence(t, service, created.SessionID)
	if len(presence) != 1 || presence[0].Sequence != 1 || presence[0].Cursor == nil || *presence[0].Cursor != cursor {
		t.Fatalf("presence after stale update = %#v, want the replacement's state retained", presence)
	}

	// The older socket closing late does not erase the replacement's presence.
	if err := first.Close(websocket.StatusNormalClosure, "stale socket"); err != nil {
		t.Fatal(err)
	}
	waitActiveConnections(t, service, 1)
	presence = currentPresence(t, service, created.SessionID)
	if len(presence) != 1 || presence[0].Sequence != 1 {
		t.Fatalf("presence after older socket closed = %#v, want the replacement's presence retained", presence)
	}
	requireAlive(t, second, created.SessionID, "second-still-alive")
}

func currentPresence(t *testing.T, service *Service, sessionID string) []Presence {
	t.Helper()
	snapshot, _, cancel, err := service.hub.SubscribePresence(sessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	return snapshot
}

func waitActiveConnections(t *testing.T, service *Service, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.mutex.RLock()
		count := service.activeConnections
		service.mutex.RUnlock()
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("active connection count did not reach %d", want)
}
