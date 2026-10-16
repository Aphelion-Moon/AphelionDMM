package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// A hosted member's chosen display name must survive the periodic and
// per-message reauthorization, which rejoins the hub with the login's name.
func TestHostedDisplayNameSurvivesReauthorization(t *testing.T) {
	t.Parallel()

	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &fakeHostedAuthorizer{session: auth.Session{
		ActorID: actorID, Issuer: "https://issuer.example", Subject: "subject", DisplayName: "Login Name", Role: auth.RoleEditor, ExpiresAt: time.Now().Add(time.Hour),
	}}
	_, created, testServer := startHTTPTestSessionWithConfig(t, ServiceConfig{
		AllowedOrigins: []string{"http://127.0.0.1"}, HostedAuth: authorizer, HostedReauthorizationInterval: time.Hour,
	})
	websocketURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer hosted-token"}, "Origin": []string{"http://127.0.0.1"}}, Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.CloseNow() }()
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "hosted-join", SessionID: created.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: "hosted-token"})
	if joined := readServerEnvelope(t, connection); joined.Envelope.Type != protocol.ServerJoined {
		t.Fatalf("first message = %q, want joined", joined.Envelope.Type)
	}
	_ = readServerEnvelopeType(t, connection, protocol.ServerReplayComplete)
	_ = readServerEnvelopeType(t, connection, protocol.ServerPresenceSnapshot)

	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "rename", SessionID: created.SessionID, Type: protocol.ClientProfileUpdate}, protocol.ProfileUpdatePayload{DisplayName: "Chosen Name"})
	// Every profile update reauthorizes the hosted credential before it is
	// handled, so this second one rejoins the hub with the login's name first.
	color := 3
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "recolor", SessionID: created.SessionID, Type: protocol.ClientProfileUpdate}, protocol.ProfileUpdatePayload{CursorColor: &color})
	writeClientEnvelope(t, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence-1", SessionID: created.SessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: 1, Status: "active"})
	update := readServerEnvelopeType(t, connection, protocol.ServerPresenceUpdate).Payload.(*protocol.ServerPresenceUpdatePayload)
	if update.ActorID != actorID || update.DisplayName != "Chosen Name" {
		t.Fatalf("presence after reauthorization = %#v, want display name %q", update, "Chosen Name")
	}
	if update.CursorColor == nil || *update.CursorColor != color {
		t.Fatalf("presence cursor color = %v, want %d", update.CursorColor, color)
	}
}
