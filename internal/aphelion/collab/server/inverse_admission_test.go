package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/transaction"
)

func TestBulkInverseReservesWorkingMemoryBeforeMaterializing(t *testing.T) {
	snapshot := testSnapshot(t, 1)
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, NewMemoryStore(), DocumentConfig{BulkEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	principal := testPrincipal(t, "Owner", RoleOwner)
	hub := NewHub(time.Minute)
	if err := hub.Create("inverse-admission", owner, principal); err != nil {
		t.Fatal(err)
	}
	accepted, err := hub.Submit(context.Background(), "inverse-admission", principal, testOperation(t, snapshot, 1))
	if err != nil {
		t.Fatal(err)
	}
	codec := bulktransport.NewWithWorkingBudget(t.TempDir(), 1<<20, 1)
	ctx := context.WithValue(context.Background(), bulkContextKey{}, bulkConnection{codec: codec, canUpload: true})
	_, err = hub.Inverse(ctx, "inverse-admission", principal, accepted.OperationID)
	var admission *transaction.AdmissionError
	if !errors.As(err, &admission) {
		t.Fatalf("inverse escaped working admission: %v", err)
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil || current.Revision != accepted.Revision {
		t.Fatalf("rejected inverse changed authority: %d, %v", current.Revision, err)
	}
}

func TestCanceledInverseReleasesAdmissionBeforeReturning(t *testing.T) {
	snapshot := testSnapshot(t, 1)
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, NewMemoryStore(), DocumentConfig{BulkEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	accepted, err := owner.Submit(context.Background(), testOperation(t, snapshot, 1))
	if err != nil {
		t.Fatal(err)
	}
	inverseID, err := model.NewOperationID()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admissionEntered := make(chan struct{})
	allowAdmissionReturn := make(chan struct{})
	var allowOnce sync.Once
	allowAdmission := func() { allowOnce.Do(func() { close(allowAdmissionReturn) }) }
	defer allowAdmission()
	var leaseReleased atomic.Bool
	var releaseOnce sync.Once
	type result struct {
		operation model.Operation
		release   func()
		err       error
	}
	completed := make(chan result, 1)
	go func() {
		operation, release, err := owner.buildInverseAdmitted(ctx, accepted.ActorID, accepted.OperationID, inverseID, func(required int64) (func(), error) {
			if required <= 0 {
				return nil, context.Canceled
			}
			close(admissionEntered)
			<-allowAdmissionReturn
			return func() { releaseOnce.Do(func() { leaseReleased.Store(true) }) }, nil
		})
		completed <- result{operation: operation, release: release, err: err}
	}()

	select {
	case <-admissionEntered:
	case <-time.After(time.Second):
		allowAdmission()
		t.Fatal("inverse admission did not start")
	}
	cancel()
	select {
	case got := <-completed:
		t.Fatalf("canceled inverse returned before its admission work settled: %v", got.err)
	case <-time.After(30 * time.Millisecond):
	}
	if leaseReleased.Load() {
		t.Fatal("inverse admission lease was released before the owner stopped using the work")
	}
	allowAdmission()

	select {
	case got := <-completed:
		if got.release != nil {
			got.release()
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled inverse error = %v, want context canceled", got.err)
		}
		if got.operation.OperationID != "" {
			t.Fatalf("canceled inverse returned an operation: %q", got.operation.OperationID)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled inverse did not finish after admission returned")
	}
	if !leaseReleased.Load() {
		t.Fatal("inverse admission lease was not released after cancellation")
	}
	current, err := owner.Snapshot(context.Background())
	if err != nil || current.Revision != accepted.Revision {
		t.Fatalf("canceled inverse changed authority: revision=%d error=%v", current.Revision, err)
	}
}
