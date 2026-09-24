package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
	"testing"
	"time"
)

type upgradeRequiredStore struct{ *MemoryStore }

func (s *upgradeRequiredStore) ConfigureTransactions(context.Context, model.DocumentID, int) error {
	return collabstore.ErrTransactionUpgradeRequired
}

func TestBulkCreationReportsRequiredStorageUpgrade(t *testing.T) {
	service := NewService(ServiceConfig{Store: &upgradeRequiredStore{NewMemoryStore()}, Document: DocumentConfig{BulkEdits: true}})
	defer func() { _ = service.Shutdown(context.Background()) }()
	token, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1), "bulk_edits": true})
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("transaction_upgrade_required")) {
		t.Fatalf("upgrade presented as invalid map: %d %s", response.Code, response.Body.String())
	}
}

type disconnectedLookupStore struct {
	*MemoryStore
	started   chan struct{}
	cancelled chan struct{}
}

func (s *disconnectedLookupStore) LookupOperation(ctx context.Context, doc model.DocumentID, id model.OperationID) (model.AcceptedOperation, bool, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	return model.AcceptedOperation{}, false, ctx.Err()
}

func TestDisconnectCancelsBeforeDurableBoundary(t *testing.T) {
	store := &disconnectedLookupStore{MemoryStore: NewMemoryStore(), started: make(chan struct{}), cancelled: make(chan struct{})}
	service := NewService(ServiceConfig{Store: store, AllowedOrigins: []string{"http://127.0.0.1"}})
	defer func() { _ = service.Shutdown(context.Background()) }()
	host := httptest.NewServer(service.Handler())
	defer host.Close()
	snapshot := testSnapshot(t, 1)
	token, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	created := createTestSession(t, host.URL, token, snapshot)
	connection := connectTestClient(t, host.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()
	submitTestOperation(t, connection, created.SessionID, "disconnect-before-commit", testOperation(t, snapshot, 1))
	select {
	case <-store.started:
	case <-time.After(2 * time.Second):
		t.Fatal("submission never started")
	}
	_ = connection.CloseNow()
	select {
	case <-store.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected operation kept running before commit")
	}
	current, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil || current.Revision != 0 {
		t.Fatalf("disconnect changed authority: revision=%d err=%v", current.Revision, err)
	}
}

func TestBulkCapabilitySurvivesDocumentRecovery(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	snapshot := testSnapshot(t, 1)
	owner, err := StartDocumentWithConfig(ctx, snapshot, store, DocumentConfig{BulkEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.bulkEdits {
		t.Fatal("bulk capability lost on create")
	}
	_ = owner.Close(ctx)
	recovered, err := RecoverDocument(ctx, snapshot.DocumentID, store)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = recovered.Close(ctx) }()
	if !recovered.bulkEdits {
		t.Fatal("recovery downgraded capability")
	}
}

type cancellationLookupStore struct {
	*MemoryStore
	started chan struct{}
	release chan struct{}
}

func (s *cancellationLookupStore) LookupOperation(ctx context.Context, doc model.DocumentID, id model.OperationID) (model.AcceptedOperation, bool, error) {
	close(s.started)
	<-s.release
	return s.MemoryStore.LookupOperation(ctx, doc, id)
}
func TestCancellationDuringSubmissionDoesNotCommit(t *testing.T) {
	store := &cancellationLookupStore{MemoryStore: NewMemoryStore(), started: make(chan struct{}), release: make(chan struct{})}
	snapshot := testSnapshot(t, 1)
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, store, DocumentConfig{BulkEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := owner.Submit(ctx, testOperation(t, snapshot, 1)); result <- err }()
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("submit did not start")
	}
	cancel()
	close(store.release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result: %v", err)
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != snapshot.Revision {
		t.Fatal("cancelled submission committed")
	}
}
