package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (backend *fakeHostedBackend) EndHostedSession(_ context.Context, id string) error {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	delete(backend.sessions, id)
	delete(backend.members, id)
	for hash, invitation := range backend.invitations {
		if invitation.SessionID == id {
			delete(backend.invitations, hash)
		}
	}
	return nil
}

func TestHostedEmptySessionExpiresWithoutDeletingDocument(t *testing.T) {
	now := time.Now()
	actor, _ := model.NewActorID()
	backend := newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: now.Add(time.Hour)}})
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	created := createHostedBrowserTestSession(t, server.URL, "owner", testSnapshot(t, 1), collabstore.HostedSessionMetadata{})
	service.mutex.RLock()
	owner := service.sessions[created.SessionID].owner
	service.mutex.RUnlock()
	// Creation has a separate grace period for initial snapshot transfer.
	if err := service.expireHostedSessions(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("session expired before initial join")
	}
	first := service.trackHostedConnection(created.SessionID, actor, now.Add(time.Hour))
	second := service.trackHostedConnection(created.SessionID, actor, now.Add(time.Hour))
	first()
	if err := service.expireHostedSessions(now.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("duplicate socket cleanup ended an occupied session")
	}
	second()
	if err := service.expireHostedSessions(now.Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reconnected := service.trackHostedConnection(created.SessionID, actor, now.Add(time.Hour))
	if err := service.expireHostedSessions(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("reconnect did not cancel expiration")
	}
	reconnected()
	releaseSnapshot, available := service.holdHostedSnapshot(created.SessionID)
	if !available {
		t.Fatal("snapshot transfer was not admitted")
	}
	if err := service.expireHostedSessions(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("snapshot transfer did not fence expiration")
	}
	releaseSnapshot()
	if err := service.expireHostedSessions(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("empty session remained available")
	}
	sessions, err := backend.ListHostedSessions(context.Background())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("stale registry: %v %v", sessions, err)
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("document worker leaked")
	}
	if _, err := service.store.LoadRecovery(context.Background(), created.DocumentID); err != nil {
		t.Fatalf("durable document lost: %v", err)
	}
	if err := service.RecoverHostedSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("ended session recovered")
	}
}

type blockedSessionEnd struct {
	*fakeHostedBackend
	entered chan struct{}
	release chan struct{}
}

func (backend *blockedSessionEnd) EndHostedSession(ctx context.Context, id string) error {
	close(backend.entered)
	<-backend.release
	return backend.fakeHostedBackend.EndHostedSession(ctx, id)
}

func TestHostedExpiryFencesConcurrentReconnect(t *testing.T) {
	now := time.Now()
	actor, _ := model.NewActorID()
	backend := &blockedSessionEnd{fakeHostedBackend: newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: now.Add(time.Hour)}}), entered: make(chan struct{}), release: make(chan struct{})}
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	created := createHostedBrowserTestSession(t, server.URL, "owner", testSnapshot(t, 1), collabstore.HostedSessionMetadata{})
	finished := make(chan error, 1)
	go func() { finished <- service.expireHostedSessions(now.Add(6 * time.Minute)) }()
	<-backend.entered
	cleanup := service.trackHostedConnection(created.SessionID, actor, now.Add(time.Hour))
	if cleanup != nil {
		cleanup()
		t.Error("socket registered while session was ending")
	}
	if service.hostedDocumentAvailable(created.SessionID) {
		t.Error("ending session advertised as available")
	}
	close(backend.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestHostedNeverJoinedSessionExpires(t *testing.T) {
	now := time.Now()
	actor, _ := model.NewActorID()
	backend := newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: now.Add(time.Hour)}})
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	created := createHostedBrowserTestSession(t, server.URL, "owner", testSnapshot(t, 1), collabstore.HostedSessionMetadata{})
	if err := service.expireHostedSessions(now.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if service.hostedDocumentAvailable(created.SessionID) {
		t.Fatal("abandoned creation remained available")
	}
}

func TestHostedCreationCannotAdoptRetainedDocument(t *testing.T) {
	now := time.Now()
	actor, _ := model.NewActorID()
	backend := newFakeHostedBackend(map[string]auth.Session{"new-user": {Token: "new-user", ActorID: actor, Issuer: "issuer", Subject: "different-user", DisplayName: "New user", ExpiresAt: now.Add(time.Hour)}})
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend})
	defer func() { _ = service.Shutdown(context.Background()) }()
	snapshot := testSnapshot(t, 1)
	// This is an ended document: durable map/history, no live membership.
	owner, err := StartDocument(context.Background(), snapshot, service.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Submit(context.Background(), testOperation(t, snapshot, 1)); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"snapshot": snapshot})
	request := httptest.NewRequest("POST", "/v1/hosted/sessions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer new-user")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != 409 {
		t.Fatalf("old baseline claimed retained history: HTTP %d", response.Code)
	}
	sessions, _ := backend.ListHostedSessions(context.Background())
	if len(sessions) != 0 {
		t.Fatal("retained document assigned to a new owner")
	}
}

type cancelAfterDocumentCreate struct {
	*MemoryStore
	cancel context.CancelFunc
}

func (store *cancelAfterDocumentCreate) Create(ctx context.Context, snapshot model.Snapshot) error {
	err := store.MemoryStore.Create(ctx, snapshot)
	store.cancel()
	return err
}

func TestHostedCanceledResponseDoesNotOrphanStoredDocument(t *testing.T) {
	actor, _ := model.NewActorID()
	backend := newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: time.Now().Add(time.Hour)}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelAfterDocumentCreate{MemoryStore: NewMemoryStore(), cancel: cancel}
	service := NewService(ServiceConfig{Store: store, HostedAuth: backend, HostedRegistry: backend})
	defer func() { _ = service.Shutdown(context.Background()) }()
	snapshot := testSnapshot(t, 1)
	body, _ := json.Marshal(map[string]any{"snapshot": snapshot})
	request := httptest.NewRequest("POST", "/v1/hosted/sessions", bytes.NewReader(body)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer owner")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	sessions, err := backend.ListHostedSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("request cancellation orphaned a stored map: %v %v", sessions, err)
	}
	if !service.hostedDocumentAvailable(string(snapshot.DocumentID)) {
		t.Fatal("request cancellation stopped the registered owner")
	}
}
