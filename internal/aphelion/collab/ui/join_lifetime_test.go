package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestLeaveCancelsInitialSnapshotFetch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client := NewSessionClient(SessionClientConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- client.Join(ctx, Invitation{BaseURL: server.URL, Origin: server.URL, SessionID: "session-1", Token: "token", TokenExpiresAt: time.Now().Add(time.Hour)})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("join did not fetch its snapshot")
	}
	if err := client.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("abandoned join = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Leave did not cancel the initial snapshot request")
	}
	if status := client.Status(); status.State != collabclient.StateClosed || status.ReconnectReady {
		t.Fatalf("abandoned join changed closed state: %+v", status)
	}
}

type delayedJoinTransport struct {
	*integritySessionTransport
	started, release chan struct{}
}

func (transport *delayedJoinTransport) Connect(ctx context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	if err := transport.integritySessionTransport.Connect(ctx, request, receive); err != nil {
		return err
	}
	close(transport.started)
	<-transport.release // Deliberately complete even after cancellation.
	return nil
}

func TestAbandonedJoinCannotReplaceNewSession(t *testing.T) {
	snapshot := controllerSnapshot(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(snapshot)
	}))
	defer server.Close()
	hash := mustSnapshotHash(t, snapshot)
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ad")
	messages := func(sessionID string) []protocol.ServerEnvelope {
		return []protocol.ServerEnvelope{
			{ProtocolVersion: model.ProtocolVersion, MessageID: "joined", SessionID: sessionID, Type: protocol.ServerJoined, Payload: mustRawJSON(t, protocol.JoinedPayload{DocumentID: snapshot.DocumentID, ActorID: actor, Role: "owner", Revision: snapshot.Revision, MapHash: hash, PresenceIntervalMS: 100, ResumptionToken: "resume", ResumptionTokenExpiresAt: time.Now().Add(time.Hour)})},
			{ProtocolVersion: model.ProtocolVersion, MessageID: "ready", SessionID: sessionID, Type: protocol.ServerReplayComplete, Payload: mustRawJSON(t, protocol.ReplayCompletePayload{Revision: snapshot.Revision, MapHash: hash})},
		}
	}
	old := &delayedJoinTransport{integritySessionTransport: newIntegritySessionTransport(messages("old-session")...), started: make(chan struct{}), release: make(chan struct{})}
	releaseOld := sync.OnceFunc(func() { close(old.release) })
	defer releaseOld()
	current := newIntegritySessionTransport(messages("new-session")...)
	transports := []SessionTransport{old, current}
	client := NewSessionClient(SessionClientConfig{NewTransport: func() SessionTransport {
		transport := transports[0]
		transports = transports[1:]
		return transport
	}})
	defer func() { _ = client.Leave(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	invitation := Invitation{BaseURL: server.URL, Origin: server.URL, SessionID: "old-session", Token: "token", TokenExpiresAt: time.Now().Add(time.Hour)}
	result := make(chan error, 1)
	go func() { result <- client.Join(ctx, invitation) }()
	select {
	case <-old.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := client.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	invitation.SessionID = "new-session"
	if err := client.Join(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	network := client.NetworkExecutor()
	releaseOld()
	if err := <-result; err == nil {
		t.Error("abandoned join reported success")
	}
	if client.NetworkExecutor() != network || !old.wasClosed() || current.wasClosed() {
		t.Error("abandoned join replaced or damaged the current connection")
	}
	// Old sockets must not repopulate or replace the current participant view.
	presence := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence", SessionID: "old-session", Type: protocol.ServerPresenceUpdate, Payload: mustRawJSON(t, protocol.ServerPresenceUpdatePayload{ActorID: actor, DisplayName: "Old peer", Sequence: 1, Cursor: coord(1, 1, 1), Status: "active"})}
	if _, err := protocol.DecodeServerEnvelope(presence); err != nil {
		t.Fatal(err)
	}
	old.deliver(presence)
	presence.Type = protocol.ServerPresenceSnapshot
	presence.Payload = mustRawJSON(t, protocol.PresenceSnapshotPayload{Participants: []protocol.ParticipantPresence{{ActorID: actor, DisplayName: "Old peer", Sequence: 1, Status: "active"}}})
	if _, err := protocol.DecodeServerEnvelope(presence); err != nil {
		t.Fatal(err)
	}
	old.deliver(presence)
	if len(client.ObservedPresence()) != 0 {
		t.Error("old join callback polluted current presence")
	}
	presence.SessionID = "new-session"
	current.deliver(presence)
	if len(client.ObservedPresence()) != 1 {
		t.Fatal("returning from Join disabled current presence callbacks")
	}
	if err := client.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	current.deliver(messages("new-session")[0])
	current.deliver(presence)
	if len(client.ObservedPresence()) != 0 || client.Status().ReconnectReady || client.Status().State != collabclient.StateClosed {
		t.Fatal("late connection messages revived a left session")
	}
}
