package postgres

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func assertRecoversTo(t *testing.T, store *Store, documentID model.DocumentID, document *engine.Document) {
	t.Helper()
	state, err := store.LoadRecovery(context.Background(), documentID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := state.Restore()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := document.Metadata()
	got, _ := restored.Metadata()
	if got.Revision != want.Revision || got.MapHash != want.MapHash {
		t.Fatalf("recovered %d/%s, want %d/%s", got.Revision, got.MapHash, want.Revision, want.MapHash)
	}
}

// A cached verified head is only a shortcut: another instance's append, a
// snapshot publication and a rejected append must each force the full
// recovery path, and appends must keep verifying against the durable head.
func TestVerifiedHeadFallsBackWhenTheDurableHeadMoves(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	first, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	second, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	const size = 12
	snapshot := appendLatencySnapshot(size)
	if err := first.Create(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	step := 0
	next := func() model.AcceptedOperation {
		step++
		return nextSingleTileEdit(t, document, size, snapshot.EnvironmentHash, step)
	}
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatal(err)
	}
	// Another server instance advances the head behind first's cache.
	if err := second.Append(context.Background(), next()); err != nil {
		t.Fatal(err)
	}
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatalf("append after another writer: %v", err)
	}
	// A replayed accepted operation for a revision already taken is refused
	// and must not leave a usable cached head behind.
	stale := next()
	if err := first.Append(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	conflicting := stale
	conflicting.OperationID = "01890f3e-7b5c-7abc-8def-0fffffffffff"
	if err := first.Append(context.Background(), conflicting); err == nil {
		t.Fatal("non-contiguous append was accepted")
	}
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatalf("append after a refused append: %v", err)
	}
	// Publishing a snapshot changes the retained base revision.
	wrong := document.Snapshot()
	wrong.Tiles[0].State.Prefabs[0].Vars["dir"] = "tampered"
	if err := first.SaveSnapshot(context.Background(), wrong); err == nil {
		t.Fatal("snapshot with different content was published")
	}
	future := document.Snapshot()
	future.Revision++
	if err := first.SaveSnapshot(context.Background(), future); err == nil {
		t.Fatal("snapshot beyond the durable head was published")
	}
	if err := first.SaveSnapshot(context.Background(), document.Snapshot()); err != nil {
		t.Fatal(err)
	}
	// Publication keeps the verified head, so the next append does not replay.
	metadata, _ := document.Metadata()
	key := collabstore.HeadKey{SnapshotRevision: metadata.Revision, Revision: metadata.Revision, Hash: metadata.MapHash}
	cached := first.heads.Take(snapshot.DocumentID, key)
	if cached == nil {
		t.Fatal("snapshot publication dropped the verified head")
	}
	first.heads.Put(snapshot.DocumentID, key, cached)
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatalf("append after snapshot: %v", err)
	}
	if err := second.Append(context.Background(), next()); err != nil {
		t.Fatalf("second instance append after first's cached appends: %v", err)
	}
	assertRecoversTo(t, first, snapshot.DocumentID, document)
}
