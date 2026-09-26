package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestHostedDuplicateSocketsKeepNewestPresenceAndCountUntilExpiry(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeHostedBackend(map[string]auth.Session{
		"owner-token": {Token: "owner-token", ActorID: actorID, Issuer: "https://issuer.example", Subject: "owner", DisplayName: "Owner", Role: auth.RoleOwner, ExpiresAt: expiresAt},
	})
	service := NewService(ServiceConfig{
		HostedAuth: backend, HostedRegistry: backend, AllowedOrigins: []string{"http://127.0.0.1"},
		HostedReauthorizationInterval: 2 * time.Hour, Now: func() time.Time { return now }, OnWebSocketError: func(error) {},
	})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)
	created := createHostedBrowserTestSession(t, testServer.URL, "owner-token", testSnapshot(t, 1), collabstore.HostedSessionMetadata{Visibility: collabstore.HostedVisibilityCommunity, Title: "Active map"})
	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"

	older := connectHostedBrowserSocket(t, websocketURL, created.SessionID, "owner-token")
	defer func() { _ = older.CloseNow() }()
	waitHostedSocketCount(t, service, created.SessionID, 1)
	newer := connectHostedBrowserSocket(t, websocketURL, created.SessionID, "owner-token")
	defer func() { _ = newer.CloseNow() }()
	waitHostedSocketCount(t, service, created.SessionID, 2)
	if got := service.hostedParticipantCount(created.SessionID); got != 1 {
		t.Fatalf("duplicate actor participant count = %d, want 1", got)
	}

	writeClientEnvelope(t, newer, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence-newer", SessionID: created.SessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: 1, Cursor: &model.Coord{X: 1, Y: 1, Z: 1}, Status: "active"})
	_ = readServerEnvelopeType(t, older, protocol.ServerPresenceUpdate)
	_ = readServerEnvelopeType(t, newer, protocol.ServerPresenceUpdate)
	if err := older.Close(websocket.StatusNormalClosure, "older socket closed"); err != nil {
		t.Fatal(err)
	}
	waitHostedSocketCount(t, service, created.SessionID, 1)
	if got := service.hostedParticipantCount(created.SessionID); got != 1 {
		t.Fatalf("participant count after older socket closed = %d, want 1", got)
	}
	presence, _, cancelPresence, err := service.hub.SubscribePresence(created.SessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	cancelPresence()
	if len(presence) != 1 || presence[0].ActorID != actorID || presence[0].Sequence != 1 {
		t.Fatalf("presence after older socket closed = %#v; newer connection's actor presence must remain", presence)
	}

	now = expiresAt.Add(time.Second)
	if got := service.hostedParticipantCount(created.SessionID); got != 0 {
		t.Fatalf("expired participant count = %d, want 0", got)
	}
	if err := newer.Close(websocket.StatusNormalClosure, "newer socket closed"); err != nil {
		t.Fatal(err)
	}
	waitHostedSocketCount(t, service, created.SessionID, 0)
	if got := service.hostedParticipantCount(created.SessionID); got != 0 {
		t.Fatalf("idle participant count = %d, want 0", got)
	}
}

func connectHostedBrowserSocket(t *testing.T, websocketURL, sessionID, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "hosted-join", SessionID: sessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: token})
	if joined := readServerEnvelope(t, connection); joined.Envelope.Type != protocol.ServerJoined {
		_ = connection.CloseNow()
		t.Fatalf("first hosted socket message = %q, want joined", joined.Envelope.Type)
	}
	_ = readServerEnvelopeType(t, connection, protocol.ServerReplayComplete)
	_ = readServerEnvelopeType(t, connection, protocol.ServerPresenceSnapshot)
	return connection
}

func waitHostedSocketCount(t *testing.T, service *Service, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.mutex.RLock()
		count := 0
		for _, connection := range service.hostedConnections {
			if connection.sessionID == sessionID {
				count++
			}
		}
		service.mutex.RUnlock()
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hosted socket count did not reach %d", want)
}
