package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// appendLatencySnapshot is a station-sized level: 255x255 tiles, three
// prefabs each, close to MetaStation's tile and prefab counts.
func appendLatencySnapshot(size int) model.Snapshot {
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

// nextSingleTileEdit is one quick remote-style edit accepted by document.
func nextSingleTileEdit(t testing.TB, document *engine.Document, size int, environment string, step int) model.AcceptedOperation {
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

// TestAppendLatencyDoesNotGrowWithHistory is the hosted journal path: each
// quick single-tile edit is appended while history accumulates since the last
// snapshot (the hosted server snapshots every 1000 operations). An append must
// cost about the same at the 1st and the Nth operation.
func TestAppendLatencyDoesNotGrowWithHistory(t *testing.T) {
	dsn, schema := isolatedSchema(t)
	size, operations := 96, 60
	if os.Getenv("APHELION_APPEND_LATENCY_STATION") != "" {
		size, operations = 255, 200
	}
	store, err := Open(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	snapshot := appendLatencySnapshot(size)
	if err := store.Create(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, 0, operations)
	for step := 0; step < operations; step++ {
		accepted := nextSingleTileEdit(t, document, size, snapshot.EnvironmentHash, step)
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
	// The stored history must still recover to the in-memory head.
	state, err := store.LoadRecovery(context.Background(), snapshot.DocumentID)
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
