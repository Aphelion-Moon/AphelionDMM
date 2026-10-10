package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func headSnapshot(size int) model.Snapshot {
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: size, MaxY: size, MaxZ: 1}
	id := 0
	for y := 1; y <= size; y++ {
		for x := 1; x <= size; x++ {
			state := model.TileState{}
			for _, path := range []string{"/area/station/hallway", "/turf/open/floor/iron/dark", "/obj/effect/turf_decal/tile/blue"} {
				id++
				state.Prefabs = append(state.Prefabs, model.PrefabState{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", id)), Path: path, Vars: map[string]string{"dir": "4"}})
			}
			snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}, State: state})
		}
	}
	return snapshot
}

func headEdit(t testing.TB, document *engine.Document, size int, environment string, step int) model.AcceptedOperation {
	t.Helper()
	metadata, err := document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	coord := model.Coord{X: step*37%size + 1, Y: step*53%size + 1, Z: 1}
	before := document.Tile(coord)
	after := model.CloneTileState(before)
	after.Prefabs[1].Vars["dir"] = fmt.Sprint(step%4 + 1)
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: metadata.DocumentID, EnvironmentHash: environment,
		OperationID: model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-f%011x", step+1)), ActorID: "01890f3e-7b5c-7abc-8def-0123456789bc",
		BaseRevision: metadata.Revision, BaseMapHash: metadata.MapHash, Kind: model.OperationKindTileChange,
		Changes: []model.TileChange{{Coord: coord, Before: before, After: after}}}
	accepted, err := document.Apply(operation, time.Unix(int64(step+1), 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}

func assertSQLiteRecoversTo(t *testing.T, store *Store, documentID model.DocumentID, document *engine.Document) {
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

// A large map's snapshot alone exceeds the validated-load cache budget, so
// before the verified head every append replayed the whole session history.
func TestSQLiteAppendLatencyDoesNotGrowWithHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "collab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const size, operations = 160, 60
	snapshot := headSnapshot(size)
	if err := store.Create(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, 0, operations)
	for step := 0; step < operations; step++ {
		accepted := headEdit(t, document, size, snapshot.EnvironmentHash, step)
		started := time.Now()
		if err := store.Append(context.Background(), accepted); err != nil {
			t.Fatalf("append %d: %v", step, err)
		}
		durations = append(durations, time.Since(started))
	}
	average := func(values []time.Duration) time.Duration {
		var total time.Duration
		for _, value := range values {
			total += value
		}
		return total / time.Duration(len(values))
	}
	early, late := average(durations[1:11]), average(durations[len(durations)-10:])
	t.Logf("%dx%d map: first append %v, appends 2-11 %v, last 10 appends %v", size, size, durations[0], early, late)
	if late > 2*early+10*time.Millisecond {
		t.Fatalf("append latency grew with history: early %v, late %v", early, late)
	}
	assertSQLiteRecoversTo(t, store, snapshot.DocumentID, document)
}

func TestSQLiteVerifiedHeadFallsBackWhenTheDurableHeadMoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collab.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	const size = 12
	snapshot := headSnapshot(size)
	if err := first.Create(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	step := 0
	next := func() model.AcceptedOperation {
		step++
		return headEdit(t, document, size, snapshot.EnvironmentHash, step)
	}
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatal(err)
	}
	if err := second.Append(context.Background(), next()); err != nil {
		t.Fatal(err)
	}
	if err := first.Append(context.Background(), next()); err != nil {
		t.Fatalf("append after another writer: %v", err)
	}
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
	// Reconnect replay still validates and serves the latest head.
	if _, replay, hashes, err := first.LoadReplay(context.Background(), snapshot.DocumentID); err != nil || len(replay) != 1 || hashes[model.Revision(step)] == "" {
		t.Fatalf("replay after cached appends: %d ops, %v", len(replay), err)
	}
	if err := second.Append(context.Background(), next()); err != nil {
		t.Fatalf("second instance append after first's cached appends: %v", err)
	}
	assertSQLiteRecoversTo(t, first, snapshot.DocumentID, document)
}
