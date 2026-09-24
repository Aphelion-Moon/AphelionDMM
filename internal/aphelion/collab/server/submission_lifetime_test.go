package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

type heldReconciliationStore struct {
	*MemoryStore
	committed atomic.Bool
	verifying chan struct{}
	release   chan struct{}
}

func (s *heldReconciliationStore) Append(ctx context.Context, op model.AcceptedOperation) error {
	if err := s.MemoryStore.Append(ctx, op); err != nil {
		return err
	}
	s.committed.Store(true)
	return errors.New("acknowledgement lost after durable commit")
}

func (s *heldReconciliationStore) LookupOperation(ctx context.Context, doc model.DocumentID, id model.OperationID) (model.AcceptedOperation, bool, error) {
	if s.committed.Load() {
		close(s.verifying)
		select {
		case <-s.release:
		case <-ctx.Done():
			return model.AcceptedOperation{}, false, ctx.Err()
		}
	}
	return s.MemoryStore.LookupOperation(ctx, doc, id)
}

func TestCanceledSubmissionRetainsOwnershipUntilReconciliationFinishes(t *testing.T) {
	store := &heldReconciliationStore{MemoryStore: NewMemoryStore(), verifying: make(chan struct{}), release: make(chan struct{})}
	snapshot := testSnapshot(t, 1)
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, store, DocumentConfig{BulkEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	t.Cleanup(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	operation := testOperation(t, snapshot, 1)
	result := make(chan error, 1)
	go func() { _, err := owner.Submit(ctx, operation); result <- err }()
	select {
	case <-store.verifying:
	case <-time.After(time.Second):
		t.Fatal("reconciliation did not begin")
	}
	cancel()
	// The caller owns the incoming body's admission lease. Returning here would
	// release it while the owner still retains the operation and recovery work.
	select {
	case err := <-result:
		t.Fatalf("submission released ownership during reconciliation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("durable result after reconciliation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("submission did not finish after reconciliation")
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil || current.Revision != snapshot.Revision+1 {
		t.Fatalf("reconciled authority revision=%d error=%v", current.Revision, err)
	}
}
