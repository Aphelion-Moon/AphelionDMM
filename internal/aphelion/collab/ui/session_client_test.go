package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

func TestSessionClientReportsOversizedSnapshotRequestSize(t *testing.T) {
	t.Parallel()

	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	t.Cleanup(testServer.Close)
	client := NewSessionClient(SessionClientConfig{})
	_, err := client.Create(context.Background(), testServer.URL, "launch-token", controllerSnapshot(t))
	if err == nil || !strings.Contains(err.Error(), "snapshot request") || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("Create() error = %v, want snapshot request size diagnostic", err)
	}
}

func TestSessionClientCreateNamedSendsOwnerDisplayName(t *testing.T) {
	t.Parallel()
	snapshot := controllerSnapshot(t)
	var displayName string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		displayName = body.DisplayName
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"session_id": "session-1", "document_id": snapshot.DocumentID, "revision": snapshot.Revision,
			"map_hash": mustSnapshotHash(t, snapshot), "owner_token": "owner-token", "owner_token_expires_at": time.Now().Add(time.Hour),
		})
	}))
	t.Cleanup(testServer.Close)
	client := NewSessionClient(SessionClientConfig{})
	if _, err := client.CreateNamed(context.Background(), testServer.URL, "launch-token", snapshot, "  Test Owner  "); err != nil {
		t.Fatal(err)
	}
	if displayName != "Test Owner" {
		t.Fatalf("display name = %q, want Test Owner", displayName)
	}
}

func TestSessionClientPublishPresenceUsesNegotiatedInterval(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	scheduler := &manualPresenceScheduler{}
	transport := &capturingSessionTransport{}
	client := NewSessionClient(SessionClientConfig{
		Now:      func() time.Time { return now },
		Schedule: scheduler.schedule,
	})
	client.transport = transport
	client.sessionID = "session-1"
	client.presenceInterval = 100 * time.Millisecond

	first := model.Coord{X: 1, Y: 2, Z: 1}
	selection := &protocol.PresenceSelection{Min: model.Coord{X: 1, Y: 1, Z: 1}, Max: model.Coord{X: 2, Y: 2, Z: 1}}
	if err := client.PublishPresence(context.Background(), &first, selection, "active"); err != nil {
		t.Fatal(err)
	}
	suppressed := model.Coord{X: 2, Y: 2, Z: 1}
	if err := client.PublishPresence(context.Background(), &suppressed, selection, "active"); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("presence messages before trailing flush = %d, want 1", len(transport.sent))
	}
	if scheduler.delay != 100*time.Millisecond || scheduler.job == nil {
		t.Fatalf("scheduled trailing flush = %v, job present %t", scheduler.delay, scheduler.job != nil)
	}
	now = now.Add(100 * time.Millisecond)
	scheduler.run()

	if len(transport.sent) != 2 {
		t.Fatalf("presence messages = %d, want 2", len(transport.sent))
	}
	for index, want := range []model.Coord{first, suppressed} {
		decoded, err := protocol.DecodeClient(mustMarshalEnvelope(t, transport.sent[index]))
		if err != nil {
			t.Fatal(err)
		}
		payload := decoded.Payload.(*protocol.PresenceUpdatePayload)
		if payload.Sequence != uint64(index+1) || payload.Cursor == nil || *payload.Cursor != want {
			t.Fatalf("presence %d = %#v, want sequence %d cursor %#v", index, payload, index+1, want)
		}
		if payload.Selection == nil || payload.Selection.Min != selection.Min || payload.Selection.Max != selection.Max {
			t.Fatalf("presence %d selection = %#v, want %#v", index, payload.Selection, selection)
		}
	}
}

func TestSessionClientUpdatesAuthenticatedDisplayName(t *testing.T) {
	t.Parallel()
	transport := &capturingSessionTransport{}
	client := NewSessionClient(SessionClientConfig{})
	client.transport = transport
	client.sessionID = "session-1"
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized} {
		if err := client.machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.UpdateDisplayName(context.Background(), "  Test Owner  "); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("profile messages = %d, want 1", len(transport.sent))
	}
	decoded, err := protocol.DecodeClient(mustMarshalEnvelope(t, transport.sent[0]))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Envelope.Type != protocol.ClientProfileUpdate || decoded.Payload.(*protocol.ProfileUpdatePayload).DisplayName != "Test Owner" {
		t.Fatalf("profile update = %#v", decoded)
	}
	if err := client.UpdateDisplayName(context.Background(), "   "); err == nil {
		t.Fatal("blank display name was accepted")
	}
}

type manualPresenceScheduler struct {
	delay    time.Duration
	job      func()
	canceled bool
}

func (scheduler *manualPresenceScheduler) schedule(delay time.Duration, job func()) func() {
	scheduler.delay = delay
	scheduler.job = job
	return func() { scheduler.canceled = true }
}

func (scheduler *manualPresenceScheduler) run() {
	if scheduler.job != nil && !scheduler.canceled {
		scheduler.job()
	}
}

func mustMarshalEnvelope(t *testing.T, envelope protocol.ClientEnvelope) []byte {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type capturingSessionTransport struct {
	sent []protocol.ClientEnvelope
}

func (*capturingSessionTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}

func (transport *capturingSessionTransport) Send(_ context.Context, envelope protocol.ClientEnvelope) error {
	transport.sent = append(transport.sent, envelope)
	return nil
}

func (*capturingSessionTransport) Close(websocket.StatusCode, string) error { return nil }

func (*capturingSessionTransport) Wait(context.Context) error { return nil }

func TestSessionClientDropsPresenceOnConnectionLoss(t *testing.T) {
	t.Parallel()

	client := NewSessionClient(SessionClientConfig{})
	machine := client.machine
	if err := machine.Apply(collabclient.EventConnect); err != nil {
		t.Fatal(err)
	}
	client.participants[model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ac")] = ObservedPresence{
		Presence:   protocol.ParticipantPresence{ActorID: model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ac")},
		ObservedAt: time.Now(),
	}

	client.recordConnectionFailure(machine, errors.New("connection lost"))

	if presence := client.ObservedPresence(); len(presence) != 0 {
		t.Fatalf("presence after connection loss = %d entries, want 0", len(presence))
	}
}

func TestSessionClientObservedPresenceDetachesSelection(t *testing.T) {
	t.Parallel()

	actorID := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ad")
	client := NewSessionClient(SessionClientConfig{})
	client.participants[actorID] = ObservedPresence{
		Presence: protocol.ParticipantPresence{
			ActorID: actorID,
			Selection: &protocol.PresenceSelection{
				Min: model.Coord{X: 1, Y: 1, Z: 1},
				Max: model.Coord{X: 2, Y: 2, Z: 1},
			},
		},
		ObservedAt: time.Now(),
	}

	observed := client.ObservedPresence()
	observed[0].Presence.Selection.Min.X = 99

	detached := client.ObservedPresence()
	if detached[0].Presence.Selection.Min.X != 1 {
		t.Fatalf("internal selection was mutated through observed copy: %#v", detached[0].Presence.Selection)
	}
}

func TestSessionClientReportsConnectionLoss(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	client := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	if err := embedded.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if client.Status().State == collabclient.StateReconnecting {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state after connection loss = %q, want reconnecting", client.Status().State)
}

func TestSessionClientCreatesScopedInvitationOnlyForActiveOwner(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	client := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	if !client.Status().InviteReady {
		t.Fatal("owner session did not expose invitation capability")
	}
	created, err := client.CreateInvitation(context.Background(), InvitationRoleEditor, "Second Mapper")
	if err != nil {
		t.Fatal(err)
	}
	if created.BaseURL != embedded.Endpoint() || created.Origin != embedded.Endpoint() || created.SessionID != invitation.SessionID || created.Token == "" {
		t.Fatalf("created invitation = %#v", created)
	}
	second := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	if err := second.Join(context.Background(), created); err != nil {
		t.Fatalf("second client join: %v", err)
	}
	t.Cleanup(func() { _ = second.Leave(context.Background()) })
	if status := second.Status(); status.Role != "editor" || status.SessionID != invitation.SessionID || status.State != collabclient.StateCaughtUp {
		t.Fatalf("second client status = %#v", status)
	}
	reused := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	if err := reused.Join(context.Background(), created); err == nil {
		t.Fatal("single-use invitation joined a second time")
	}
	if err := client.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.Status().InviteReady {
		t.Fatal("leave retained invitation capability")
	}
	if _, err := client.CreateInvitation(context.Background(), InvitationRoleEditor, "Late Mapper"); err == nil {
		t.Fatal("inactive client minted an invitation")
	}
}

func TestSessionClientReconnectsWithRotatedCredential(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	client := NewSessionClient(SessionClientConfig{})
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	client.mutex.Lock()
	firstTransport := client.transport
	firstNetwork := client.network
	firstCredential := client.resumptionToken
	client.mutex.Unlock()
	if err := firstTransport.Close(websocket.StatusInternalError, "forced reconnect"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client.mutex.Lock()
		reconnected := client.transport != nil && client.transport != firstTransport && client.network == firstNetwork && client.resumptionToken != "" && client.resumptionToken != firstCredential
		client.mutex.Unlock()
		if reconnected && client.Status().State == collabclient.StateCaughtUp {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session did not reconnect: status %#v", client.Status())
}

func TestSessionClientReconnectsAfterInFlightOperationAndAcceptsInverse(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	initialStableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tiles = []model.Tile{{
		Coord: model.Coord{X: 1, Y: 1, Z: 1},
		State: model.TileState{Prefabs: []model.PrefabState{{StableID: initialStableID, Path: "/turf/open/space", Vars: map[string]string{}}}},
	}}
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })

	blocked := &blockingOperationSessionTransport{
		sessionTransport: collabclient.NewWebSocketTransport(collabclient.TransportConfig{}),
		intercepted:      make(chan struct{}),
		release:          make(chan struct{}),
	}
	client := NewSessionClient(SessionClientConfig{})
	transportCount := 0
	client.newTransport = func() sessionTransport {
		transportCount++
		if transportCount == 1 {
			return blocked
		}
		return collabclient.NewWebSocketTransport(collabclient.TransportConfig{})
	}
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	network := client.NetworkExecutor()
	if network == nil {
		t.Fatal("joined session has no network executor")
	}

	pendingResult := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), sessionReplacementOperation(t, snapshot, client.actorID, "/turf/open/floor"), func(_ model.AcceptedOperation, executeErr error) {
		pendingResult <- executeErr
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.intercepted:
	case <-time.After(time.Second):
		t.Fatal("operation did not reach the gated transport")
	}
	if err := blocked.Close(websocket.StatusInternalError, "forced disconnect with operation in flight"); err != nil {
		t.Fatal(err)
	}
	close(blocked.release)
	select {
	case pendingErr := <-pendingResult:
		if pendingErr == nil {
			t.Fatal("in-flight operation unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight operation callback was not released")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client.mutex.Lock()
		reconnected := client.transport != nil && client.transport != blocked && client.network == network
		client.mutex.Unlock()
		if reconnected && client.Status().State == collabclient.StateCaughtUp {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	client.mutex.Lock()
	reconnected := client.transport != nil && client.transport != blocked && client.network == network
	client.mutex.Unlock()
	if status := client.Status(); !reconnected || status.State != collabclient.StateCaughtUp {
		t.Fatalf("status after reconnect = %#v", status)
	}
	acknowledged, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.Revision != snapshot.Revision || len(acknowledged.Tiles) != 1 || !acknowledged.Tiles[0].State.Equal(snapshot.Tiles[0].State) {
		t.Fatalf("failed operation changed authoritative state: %#v", acknowledged)
	}

	forward, err := network.Execute(context.Background(), sessionReplacementOperation(t, acknowledged, client.actorID, "/turf/open/floor"))
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := network.BuildInverse(context.Background(), forward.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := network.Execute(context.Background(), inverse); err != nil {
		t.Fatal(err)
	}
	finalSnapshot, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if finalSnapshot.Revision != snapshot.Revision+2 || len(finalSnapshot.Tiles) != 1 || !finalSnapshot.Tiles[0].State.Equal(snapshot.Tiles[0].State) {
		t.Fatalf("inverse result = %#v, want revision %d with original tile state", finalSnapshot, snapshot.Revision+2)
	}
}

func TestSessionClientRetryReconnectRejectsDuplicateAndExpiredCredential(t *testing.T) {
	t.Parallel()

	client := NewSessionClient(SessionClientConfig{})
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized, collabclient.EventConnectionLost} {
		if err := client.machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	client.resumptionToken = "resumption-token"
	client.resumptionExpiresAt = time.Now().Add(time.Hour)
	client.reconnecting = true
	if err := client.RetryReconnect(); !errors.Is(err, ErrReconnectInProgress) {
		t.Fatalf("duplicate RetryReconnect() error = %v, want %v", err, ErrReconnectInProgress)
	}
	if client.Status().ReconnectReady {
		t.Fatal("status offered retry while a reconnect attempt was active")
	}
	client.reconnecting = false
	client.resumptionExpiresAt = time.Now().Add(-time.Second)
	if err := client.RetryReconnect(); !errors.Is(err, collabclient.ErrAuthenticationDenied) {
		t.Fatalf("expired RetryReconnect() error = %v, want %v", err, collabclient.ErrAuthenticationDenied)
	}
}

func TestSessionClientReconnectAttemptStopsWhenTransportEndsBeforeReplay(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	initialTransport := &capturingSessionTransport{}
	network, err := collabclient.NewNetworkExecutor(initialTransport, snapshot, actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	transportErr := errors.New("transport ended before replay")
	network.Suspend(transportErr)
	machine := collabclient.NewStateMachine()
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized, collabclient.EventConnectionLost} {
		if err := machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	joinedPayload := protocol.JoinedPayload{
		DocumentID:               snapshot.DocumentID,
		ActorID:                  actorID,
		Role:                     "owner",
		Revision:                 snapshot.Revision,
		MapHash:                  mustSnapshotHash(t, snapshot),
		PresenceIntervalMS:       100,
		ResumptionToken:          "rotated-resumption-token",
		ResumptionTokenExpiresAt: time.Now().Add(time.Hour),
	}
	failedTransport := &preReplayFailureTransport{
		err:    transportErr,
		joined: mustRawJSON(t, joinedPayload),
	}
	client := NewSessionClient(SessionClientConfig{})
	client.machine = machine
	client.network = network
	client.sessionID = "session-1"
	client.baseURL = "http://127.0.0.1:1"
	client.origin = client.baseURL
	client.documentID = snapshot.DocumentID
	client.actorID = actorID
	client.resumptionToken = "resumption-token"
	client.resumptionExpiresAt = time.Now().Add(time.Hour)
	client.newTransport = func() sessionTransport { return failedTransport }

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.reconnectAttempt(ctx, machine, network, snapshot.Revision); !errors.Is(err, transportErr) {
		t.Fatalf("reconnectAttempt() error = %v, want %v", err, transportErr)
	}
	if state := machine.State(); state != collabclient.StateReconnecting {
		t.Fatalf("state after pre-replay transport failure = %q, want %q", state, collabclient.StateReconnecting)
	}
}

func TestSessionClientClosesTransportOnIntegrityFault(t *testing.T) {
	snapshot := controllerSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	mapHash := mustSnapshotHash(t, snapshot)
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/sessions/session-1/snapshot" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(snapshot)
	}))
	t.Cleanup(testServer.Close)
	joined := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "joined", SessionID: "session-1", Type: protocol.ServerJoined, Payload: mustRawJSON(t, protocol.JoinedPayload{
		DocumentID: snapshot.DocumentID, ActorID: actorID, Role: "owner", Revision: snapshot.Revision, MapHash: mapHash,
		PresenceIntervalMS: 100, ResumptionToken: "resumption-token", ResumptionTokenExpiresAt: time.Now().Add(time.Hour),
	})}
	replayComplete := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-complete", SessionID: "session-1", Type: protocol.ServerReplayComplete, Payload: mustRawJSON(t, protocol.ReplayCompletePayload{Revision: snapshot.Revision, MapHash: mapHash})}
	first := newIntegritySessionTransport(joined, replayComplete)
	failedReconnect := &preReplayFailureTransport{err: errors.New("reconnect remains pending"), joined: joined.Payload}
	transports := []sessionTransport{first, failedReconnect}
	client := NewSessionClient(SessionClientConfig{Reconnect: collabclient.ReconnectPolicy{MaxAttempts: 1, Wait: func(context.Context, time.Duration) error { return nil }}})
	client.newTransport = func() sessionTransport {
		transport := transports[0]
		transports = transports[1:]
		return transport
	}
	invitation := Invitation{BaseURL: testServer.URL, Origin: testServer.URL, SessionID: "session-1", Token: "owner-token", TokenExpiresAt: time.Now().Add(time.Hour)}
	if err := client.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	operation := sessionConflictOperation(t, snapshot, actorID)
	accepted := model.AcceptedOperation{Operation: operation, Revision: snapshot.Revision + 2, AcceptedAt: time.Unix(1, 0)}
	first.deliver(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "revision-gap", SessionID: "session-1", Type: protocol.ServerOperationAccepted, Payload: mustRawJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: strings.Repeat("a", 64)})})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if first.wasClosed() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("integrity fault did not close the active transport")
}

type integritySessionTransport struct {
	mutex    sync.Mutex
	messages []protocol.ServerEnvelope
	receive  func(protocol.ServerEnvelope)
	done     chan struct{}
	closed   bool
	once     sync.Once
}

func newIntegritySessionTransport(messages ...protocol.ServerEnvelope) *integritySessionTransport {
	return &integritySessionTransport{messages: messages, done: make(chan struct{})}
}

func (transport *integritySessionTransport) Connect(_ context.Context, _ protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	transport.mutex.Lock()
	transport.receive = receive
	transport.mutex.Unlock()
	for _, message := range transport.messages {
		receive(message)
	}
	return nil
}

func (*integritySessionTransport) Send(context.Context, protocol.ClientEnvelope) error { return nil }

func (transport *integritySessionTransport) Close(websocket.StatusCode, string) error {
	transport.once.Do(func() {
		transport.mutex.Lock()
		transport.closed = true
		transport.mutex.Unlock()
		close(transport.done)
	})
	return nil
}

func (transport *integritySessionTransport) Wait(ctx context.Context) error {
	select {
	case <-transport.done:
		return errors.New("integrity transport closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transport *integritySessionTransport) deliver(message protocol.ServerEnvelope) {
	transport.mutex.Lock()
	receive := transport.receive
	transport.mutex.Unlock()
	receive(message)
}

func (transport *integritySessionTransport) wasClosed() bool {
	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	return transport.closed
}

func TestSessionClientReconnectFetchesSnapshotWhenReplayWasCompacted(t *testing.T) {
	t.Parallel()

	initial := controllerSnapshot(t)
	replacement := model.CloneSnapshot(initial)
	replacement.Revision = initial.Revision + 4
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	replacement.Tiles = []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: stableID, Path: "/turf/open/floor", Vars: map[string]string{"dir": "8"}}}}}}
	replacementHash := mustSnapshotHash(t, replacement)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	firstToken := "rotated-resumption-one"
	secondToken := "rotated-resumption-two"
	expiresAt := time.Now().Add(time.Hour)
	var snapshotAuthorization string
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		snapshotAuthorization = request.Header.Get("Authorization")
		if request.URL.Path != "/v1/sessions/session-1/snapshot" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(replacement)
	}))
	t.Cleanup(testServer.Close)
	joined := func(token string) protocol.ServerEnvelope {
		return protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "joined", SessionID: "session-1", Type: protocol.ServerJoined, Payload: mustRawJSON(t, protocol.JoinedPayload{DocumentID: initial.DocumentID, ActorID: actorID, Role: "owner", Revision: replacement.Revision, MapHash: replacementHash, PresenceIntervalMS: 100, ResumptionToken: token, ResumptionTokenExpiresAt: expiresAt})}
	}
	first := newScriptedReconnectTransport(
		joined(firstToken),
		protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "snapshot-required", SessionID: "session-1", Type: protocol.ServerSessionNotice, Payload: mustRawJSON(t, protocol.SessionNoticePayload{Code: protocol.NoticeSnapshotRequired, Message: "snapshot required"})},
	)
	second := newScriptedReconnectTransport(
		joined(secondToken),
		protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-complete", SessionID: "session-1", Type: protocol.ServerReplayComplete, Payload: mustRawJSON(t, protocol.ReplayCompletePayload{Revision: replacement.Revision, MapHash: replacementHash})},
	)
	transports := []sessionTransport{first, second}
	client := NewSessionClient(SessionClientConfig{Reconnect: collabclient.ReconnectPolicy{MaxAttempts: 2, Wait: func(context.Context, time.Duration) error { return nil }}})
	client.newTransport = func() sessionTransport {
		transport := transports[0]
		transports = transports[1:]
		return transport
	}
	machine := collabclient.NewStateMachine()
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized, collabclient.EventConnectionLost} {
		if err := machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	initialTransport := &capturingSessionTransport{}
	network, err := collabclient.NewNetworkExecutor(initialTransport, initial, actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	network.Suspend(errors.New("initial disconnect"))
	client.machine = machine
	client.network = network
	client.transport = initialTransport
	client.sessionID = "session-1"
	client.baseURL = testServer.URL
	client.origin = testServer.URL
	client.documentID = initial.DocumentID
	client.actorID = actorID
	client.resumptionToken = "initial-resumption-token"
	client.resumptionExpiresAt = expiresAt
	client.reconnecting = true

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.runReconnect(ctx, machine, network)
	if status := client.Status(); status.State != collabclient.StateCaughtUp || status.Revision != replacement.Revision || status.Err != nil {
		t.Fatalf("status after snapshot fallback = %#v", status)
	}
	current, err := network.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != replacement.Revision || mustSnapshotHash(t, current) != replacementHash {
		t.Fatalf("fallback snapshot revision/hash = %d/%s, want %d/%s", current.Revision, mustSnapshotHash(t, current), replacement.Revision, replacementHash)
	}
	if snapshotAuthorization != "Bearer "+firstToken {
		t.Fatalf("snapshot authorization = %q, want rotated resumption credential", snapshotAuthorization)
	}
	if first.requests()[0].AcknowledgedRevision != initial.Revision || second.requests()[0].AcknowledgedRevision != replacement.Revision {
		t.Fatalf("reconnect revisions = %d then %d, want %d then %d", first.requests()[0].AcknowledgedRevision, second.requests()[0].AcknowledgedRevision, initial.Revision, replacement.Revision)
	}
	if err := client.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type scriptedReconnectTransport struct {
	mutex    sync.Mutex
	messages []protocol.ServerEnvelope
	joins    []protocol.JoinRequest
	done     chan struct{}
	close    sync.Once
}

func newScriptedReconnectTransport(messages ...protocol.ServerEnvelope) *scriptedReconnectTransport {
	return &scriptedReconnectTransport{messages: messages, done: make(chan struct{})}
}

func (transport *scriptedReconnectTransport) Connect(_ context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	transport.mutex.Lock()
	transport.joins = append(transport.joins, request)
	transport.mutex.Unlock()
	for _, message := range transport.messages {
		receive(message)
	}
	return nil
}

func (*scriptedReconnectTransport) Send(context.Context, protocol.ClientEnvelope) error { return nil }

func (transport *scriptedReconnectTransport) Close(websocket.StatusCode, string) error {
	transport.close.Do(func() { close(transport.done) })
	return nil
}

func (transport *scriptedReconnectTransport) Wait(ctx context.Context) error {
	select {
	case <-transport.done:
		return errors.New("scripted transport closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transport *scriptedReconnectTransport) requests() []protocol.JoinRequest {
	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	return append([]protocol.JoinRequest(nil), transport.joins...)
}

type preReplayFailureTransport struct {
	err    error
	joined []byte
}

func (transport *preReplayFailureTransport) Connect(_ context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	receive(protocol.ServerEnvelope{
		ProtocolVersion: model.ProtocolVersion,
		MessageID:       "joined",
		SessionID:       request.SessionID,
		Type:            protocol.ServerJoined,
		Payload:         transport.joined,
	})
	return nil
}

func (*preReplayFailureTransport) Send(context.Context, protocol.ClientEnvelope) error { return nil }

func (*preReplayFailureTransport) Close(websocket.StatusCode, string) error { return nil }

func (transport *preReplayFailureTransport) Wait(context.Context) error { return transport.err }

func mustSnapshotHash(t *testing.T, snapshot model.Snapshot) string {
	t.Helper()
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestSessionClientJoinContextDoesNotOwnEstablishedConnection(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	embedded, err := server.StartEmbedded(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = embedded.Shutdown(context.Background()) })
	client := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	invitation, err := client.Create(context.Background(), embedded.Endpoint(), embedded.TakeLaunchToken(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	joinContext, cancelJoin := context.WithCancel(context.Background())
	if err := client.Join(joinContext, invitation); err != nil {
		t.Fatal(err)
	}
	client.mutex.Lock()
	resumptionToken := client.resumptionToken
	resumptionExpiresAt := client.resumptionExpiresAt
	administrationToken := client.administrationToken
	client.mutex.Unlock()
	if resumptionToken == "" || resumptionToken == invitation.Token || administrationToken != invitation.Token || !resumptionExpiresAt.After(time.Now()) {
		t.Fatalf("resumption credential = token present %t, rotated %t, expiry %v", resumptionToken != "", resumptionToken != invitation.Token, resumptionExpiresAt)
	}
	status := client.Status()
	if !status.ReconnectReady || len(status.SensitiveValues) != 2 || status.SensitiveValues[0] != administrationToken || status.SensitiveValues[1] != resumptionToken {
		t.Fatalf("credential status = reconnect ready %t, sensitive values %d", status.ReconnectReady, len(status.SensitiveValues))
	}
	cancelJoin()

	waitContext, cancelWait := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = client.transport.Wait(waitContext)
	cancelWait()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("established transport ended with %v after join context cancellation", err)
	}
	if err := client.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.resumptionToken != "" || client.administrationToken != "" || !client.resumptionExpiresAt.IsZero() {
		t.Fatal("leave retained a collaboration credential")
	}
}

func TestSessionClientLeaveIgnoresAlreadyClosedTransport(t *testing.T) {
	t.Parallel()
	client := NewSessionClient(SessionClientConfig{})
	client.transport = &closeErrorSessionTransport{err: net.ErrClosed}
	if err := client.Leave(context.Background()); err != nil {
		t.Fatalf("Leave() error = %v, want nil for already-closed transport", err)
	}
	unexpected := errors.New("unexpected close failure")
	client.transport = &closeErrorSessionTransport{err: unexpected}
	if err := client.Leave(context.Background()); !errors.Is(err, unexpected) {
		t.Fatalf("Leave() error = %v, want %v", err, unexpected)
	}
}

type closeErrorSessionTransport struct {
	err error
}

func (*closeErrorSessionTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}
func (*closeErrorSessionTransport) Send(context.Context, protocol.ClientEnvelope) error { return nil }
func (transport *closeErrorSessionTransport) Close(websocket.StatusCode, string) error {
	return transport.err
}
func (*closeErrorSessionTransport) Wait(context.Context) error { return nil }

func TestSessionClientDiscardConflictResolvesConflictState(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := &channelSessionTransport{sent: make(chan protocol.ClientEnvelope, 1)}
	network, err := collabclient.NewNetworkExecutor(transport, snapshot, actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	client := NewSessionClient(SessionClientConfig{})
	client.network = network
	client.transport = transport
	client.sessionID = "session-1"
	for _, event := range []collabclient.Event{collabclient.EventConnect, collabclient.EventConnected, collabclient.EventSynchronized, collabclient.EventConflict} {
		if err := client.machine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	operation := sessionConflictOperation(t, snapshot, actorID)
	result := make(chan error, 1)
	if err := network.ExecuteAsync(context.Background(), operation, func(_ model.AcceptedOperation, executeErr error) { result <- executeErr }); err != nil {
		t.Fatal(err)
	}
	submitted := <-transport.sent
	decoded, err := protocol.DecodeClient(mustMarshalEnvelope(t, submitted))
	if err != nil {
		t.Fatal(err)
	}
	submission := decoded.Payload.(*protocol.OperationSubmitPayload).Operation
	mapHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "reject", SessionID: "session-1", Type: protocol.ServerOperationRejected, Payload: mustRawJSON(t, protocol.OperationRejectedPayload{OperationID: submission.OperationID, Code: "precondition_failed", Message: "conflict", Revision: snapshot.Revision, MapHash: mapHash})}); err != nil {
		t.Fatalf("receive rejected operation: %v", err)
	}
	if executeErr := <-result; !errors.Is(executeErr, collabclient.ErrOperationRejected) {
		t.Fatalf("rejected operation error = %v", executeErr)
	}
	if _, err := client.DiscardConflict(context.Background(), submission.OperationID); err != nil {
		t.Fatal(err)
	}
	if state := client.Status().State; state != collabclient.StateCaughtUp {
		t.Fatalf("state after discard = %q, want %q", state, collabclient.StateCaughtUp)
	}
}

type channelSessionTransport struct {
	sent chan protocol.ClientEnvelope
}

type blockingOperationSessionTransport struct {
	sessionTransport
	intercepted chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (transport *blockingOperationSessionTransport) Send(ctx context.Context, envelope protocol.ClientEnvelope) error {
	if envelope.Type != protocol.ClientOperationSubmit {
		return transport.sessionTransport.Send(ctx, envelope)
	}
	blocked := false
	transport.once.Do(func() {
		blocked = true
		close(transport.intercepted)
	})
	if !blocked {
		return transport.sessionTransport.Send(ctx, envelope)
	}
	select {
	case <-transport.release:
		return errors.New("forced disconnect before operation send")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*channelSessionTransport) Connect(context.Context, protocol.JoinRequest, func(protocol.ServerEnvelope)) error {
	return nil
}

func (transport *channelSessionTransport) Send(_ context.Context, envelope protocol.ClientEnvelope) error {
	transport.sent <- envelope
	return nil
}

func (*channelSessionTransport) Close(websocket.StatusCode, string) error { return nil }

func (*channelSessionTransport) Wait(context.Context) error { return nil }

func sessionConflictOperation(t *testing.T, snapshot model.Snapshot, actorID model.ActorID) model.Operation {
	t.Helper()
	operationID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	stableID, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID, ActorID: actorID, OperationID: operationID, BaseRevision: snapshot.Revision, EnvironmentHash: snapshot.EnvironmentHash, BaseMapHash: mapHash, Kind: model.OperationKindTileChange, Changes: []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, After: model.TileState{Prefabs: []model.PrefabState{{StableID: stableID, Path: "/turf/open/floor", Vars: map[string]string{}}}}}}}
}

func sessionReplacementOperation(t *testing.T, snapshot model.Snapshot, actorID model.ActorID, path string) model.Operation {
	t.Helper()
	operation := sessionConflictOperation(t, snapshot, actorID)
	operation.Changes[0].Coord = snapshot.Tiles[0].Coord
	operation.Changes[0].Before = model.CloneTileState(snapshot.Tiles[0].State)
	operation.Changes[0].After.Prefabs[0].Path = path
	return operation
}

func mustRawJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
