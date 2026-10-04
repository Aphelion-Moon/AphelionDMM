package server

import (
	"context"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func TestSnapshotThresholdDoesNotBlockAcknowledgement(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(t, 2)
	value := &blockingSnapshotStore{
		MemoryStore: NewMemoryStore(),
		started:     make(chan model.Snapshot, 1),
		release:     make(chan struct{}),
	}
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, value, DocumentConfig{SnapshotOperationThreshold: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	if _, err := owner.Submit(context.Background(), testOperation(t, snapshot, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Submit(context.Background(), testOperation(t, snapshot, 2)); err != nil {
		t.Fatalf("second acknowledgement blocked on snapshot: %v", err)
	}
	select {
	case captured := <-value.started:
		if captured.Revision != 2 {
			t.Fatalf("snapshot revision = %d, want 2", captured.Revision)
		}
		current, err := owner.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		change := testOperation(t, current, 1)
		change.Changes[0].Before = current.Tiles[0].State
		if _, err := owner.Submit(context.Background(), change); err != nil {
			t.Fatal(err)
		}
		if captured.Revision != 2 || !captured.Tiles[0].State.Equal(current.Tiles[0].State) {
			t.Fatal("a later edit changed the in-flight snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot did not start")
	}
	close(value.release)
}

func TestSnapshotIntervalFlushesDirtyDocument(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(t, 1)
	value := &blockingSnapshotStore{
		MemoryStore: NewMemoryStore(),
		started:     make(chan model.Snapshot, 1),
		release:     make(chan struct{}),
	}
	owner, err := StartDocumentWithConfig(context.Background(), snapshot, value, DocumentConfig{SnapshotInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	if _, err := owner.Submit(context.Background(), testOperation(t, snapshot, 1)); err != nil {
		t.Fatal(err)
	}
	select {
	case captured := <-value.started:
		if captured.Revision != 1 {
			t.Fatalf("snapshot revision = %d, want 1", captured.Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("dirty document was not snapshotted on interval")
	}
	close(value.release)
}

type blockingSnapshotStore struct {
	*MemoryStore
	started chan model.Snapshot
	release chan struct{}
}

func (store *blockingSnapshotStore) SaveSnapshot(ctx context.Context, snapshot model.Snapshot) error {
	store.started <- model.CloneSnapshot(snapshot)
	select {
	case <-store.release:
		return store.MemoryStore.SaveSnapshot(ctx, snapshot)
	case <-ctx.Done():
		return ctx.Err()
	}
}
