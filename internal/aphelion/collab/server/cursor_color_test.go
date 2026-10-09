package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func colorHub(t *testing.T) (*Hub, Principal) {
	t.Helper()
	ownerLoop, err := StartDocument(context.Background(), testSnapshot(t, 1), NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ownerLoop.Close(context.Background()) })
	owner := testPrincipal(t, "Owner", RoleOwner)
	hub := NewHub(time.Minute)
	if err := hub.Create("session-1", ownerLoop, owner); err != nil {
		t.Fatal(err)
	}
	return hub, owner
}

func TestHubCursorColorSurvivesPresenceUpdatesAndPublishes(t *testing.T) {
	t.Parallel()
	hub, owner := colorHub(t)
	// Colour chosen before the first presence update is retained.
	if err := hub.UpdateCursorColor("session-1", owner, 4); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpdatePresence("session-1", owner, PresenceUpdate{Sequence: 1, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	snapshot, updates, cancel, err := hub.SubscribePresence("session-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(snapshot) != 1 || snapshot[0].CursorColor == nil || *snapshot[0].CursorColor != 4 {
		t.Fatalf("snapshot = %#v, want colour 4", snapshot)
	}
	if err := hub.UpdatePresence("session-1", owner, PresenceUpdate{Sequence: 2, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if got := <-updates; got.CursorColor == nil || *got.CursorColor != 4 {
		t.Fatalf("later presence lost colour: %#v", got)
	}
	if err := hub.UpdateCursorColor("session-1", owner, 7); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-updates:
		if got.CursorColor == nil || *got.CursorColor != 7 || got.Sequence != 2 {
			t.Fatalf("recolour = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("colour change was not published")
	}
}

func TestHubCursorColorRejectsInvalidAndForged(t *testing.T) {
	t.Parallel()
	hub, owner := colorHub(t)
	for _, index := range []int{-1, protocol.CursorColorPaletteSize, 1 << 20} {
		if err := hub.UpdateCursorColor("session-1", owner, index); !errors.Is(err, ErrInvalidCursorColor) {
			t.Fatalf("UpdateCursorColor(%d) error = %v", index, err)
		}
	}
	forged, err := NewPrincipal("forged", owner.ActorID(), "Attacker", RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.UpdateCursorColor("session-1", forged, 1); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("forged error = %v", err)
	}
	if err := hub.UpdateCursorColor("missing", owner, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
}

func TestPresenceManagerForgetsColorOnRemove(t *testing.T) {
	t.Parallel()
	manager := NewPresenceManager(time.Minute)
	principal := testPrincipal(t, "Owner", RoleOwner)
	manager.SetCursorColor(principal, 2)
	manager.Remove(principal.ActorID())
	if err := manager.Update(principal, PresenceUpdate{Sequence: 1, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	snapshot, _, cancel := manager.Subscribe(1)
	defer cancel()
	if len(snapshot) != 1 || snapshot[0].CursorColor != nil {
		t.Fatalf("colour persisted past Remove: %#v", snapshot)
	}
}

// Older servers are unsupported, so cursor_color is delivered to every
// connection: in presence updates and in the join presence snapshot.
func TestWebSocketCursorColorIsDeliveredToAllConnections(t *testing.T) {
	t.Parallel()
	_, created, testServer := startHTTPTestSession(t)
	editorToken := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleEditor, "Editor")
	observer, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = observer.CloseNow() }()
	editor, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, editorToken, 0)
	defer func() { _ = editor.CloseNow() }()
	envelope := func(id string, kind protocol.ClientType) protocol.ClientEnvelope {
		return protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: id, SessionID: created.SessionID, Type: kind}
	}
	color := 6
	writeClientEnvelope(t, editor, envelope("editor-presence", protocol.ClientPresenceUpdate), protocol.PresenceUpdatePayload{Sequence: 1, Status: "active"})
	writeClientEnvelope(t, editor, envelope("editor-color", protocol.ClientProfileUpdate), protocol.ProfileUpdatePayload{CursorColor: &color})
	// The observer never sent cursor_color but must still receive the editor's.
	for {
		update := readServerEnvelopeType(t, observer, protocol.ServerPresenceUpdate).Payload.(*protocol.ServerPresenceUpdatePayload)
		if update.DisplayName == "Editor" && update.CursorColor != nil {
			if *update.CursorColor != color {
				t.Fatalf("observer colour = %d, want %d", *update.CursorColor, color)
			}
			break
		}
	}
	// A late joiner's snapshot carries the colour without any opt-in.
	lateToken := createTestJoinToken(t, testServer.URL, created.SessionID, created.OwnerToken, RoleViewer, "Late")
	late, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(testServer.URL, "http")+"/v1/collaboration", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + lateToken}, "Origin": []string{"http://127.0.0.1"}},
		Subprotocols: []string{WebSocketSubprotocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = late.CloseNow() }()
	writeClientEnvelope(t, late, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "join", SessionID: created.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: lateToken})
	snapshot := readServerEnvelopeType(t, late, protocol.ServerPresenceSnapshot).Payload.(*protocol.PresenceSnapshotPayload)
	var sawEditor bool
	for _, participant := range snapshot.Participants {
		if participant.DisplayName == "Editor" {
			sawEditor = true
			if participant.CursorColor == nil || *participant.CursorColor != color {
				t.Fatalf("join snapshot colour = %v, want %d", participant.CursorColor, color)
			}
		}
	}
	if !sawEditor {
		t.Fatalf("join snapshot lacks the editor: %#v", snapshot.Participants)
	}
	requireAlive(t, observer, created.SessionID, "observer-alive")
}

func TestWebSocketProfileUpdateBroadcastsAndBoundsCursorColor(t *testing.T) {
	t.Parallel()
	_, created, testServer := startHTTPTestSession(t)
	connection, _ := connectTestClientWithCredential(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	envelope := func(id string, kind protocol.ClientType) protocol.ClientEnvelope {
		return protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: id, SessionID: created.SessionID, Type: kind}
	}
	writeClientEnvelope(t, connection, envelope("presence-1", protocol.ClientPresenceUpdate), protocol.PresenceUpdatePayload{Sequence: 1, Status: "active"})

	// Out of range: bounded notice, connection stays open, nothing published.
	bad := protocol.CursorColorPaletteSize
	writeClientEnvelope(t, connection, envelope("profile-bad", protocol.ClientProfileUpdate), protocol.ProfileUpdatePayload{CursorColor: &bad})
	notice := readServerEnvelopeType(t, connection, protocol.ServerSessionNotice).Payload.(*protocol.SessionNoticePayload)
	if notice.Code != protocol.NoticeInvalidCursorColor {
		t.Fatalf("notice code = %q", notice.Code)
	}
	requireAlive(t, connection, created.SessionID, "after-bad-colour")

	// Valid colour-only update is broadcast with the presence record.
	good := 5
	writeClientEnvelope(t, connection, envelope("profile-good", protocol.ClientProfileUpdate), protocol.ProfileUpdatePayload{CursorColor: &good})
	for {
		update := readServerEnvelopeType(t, connection, protocol.ServerPresenceUpdate).Payload.(*protocol.ServerPresenceUpdatePayload)
		if update.CursorColor == nil {
			continue
		}
		if *update.CursorColor != good {
			t.Fatalf("broadcast colour = %d, want %d", *update.CursorColor, good)
		}
		break
	}
}
