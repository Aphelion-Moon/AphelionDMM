package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func incrementalHashSnapshot() model.Snapshot {
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: strings.Repeat("a", 64), MaxX: 40, MaxY: 40, MaxZ: 1}
	for i := 0; i < 1200; i++ { // sparse: later edits also append coordinates
		coord := model.Coord{X: i%40 + 1, Y: i/40 + 1, Z: 1}
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: coord, State: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/thing", Vars: map[string]string{"dir": "2"}}}}})
	}
	return snapshot
}

func editTile(t *testing.T, document *Document, coord model.Coord, value string, id int) model.AcceptedOperation {
	t.Helper()
	current := document.Snapshot()
	before := model.TileState{}
	for _, tile := range current.Tiles {
		if tile.Coord == coord {
			before = tile.State
		}
	}
	hash, ok := document.CachedHash()
	if !ok {
		t.Fatal("session document has no cached hash")
	}
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: current.DocumentID, EnvironmentHash: current.EnvironmentHash,
		OperationID: model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-f%011x", id)), ActorID: "01890f3e-7b5c-7abc-8def-0123456789bc",
		BaseRevision: current.Revision, BaseMapHash: hash, Kind: model.OperationKindTileChange,
		Changes: []model.TileChange{{Coord: coord, Before: before, After: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-e%011x", id)), Path: "/obj/edited", Vars: map[string]string{"v": value}}}}}}}
	accepted, err := document.Apply(operation, time.Unix(int64(id), 0))
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}

func assertFullHash(t *testing.T, document *Document) {
	t.Helper()
	want, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := document.CachedHash(); got != want {
		t.Fatalf("incremental document hash %s, full hash %s", got, want)
	}
}

func TestIncrementalDocumentHashMatchesFullHashAcrossBranches(t *testing.T) {
	document, err := NewDocument(incrementalHashSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	id := 0
	for _, coord := range []model.Coord{{X: 40, Y: 30, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: 20, Y: 15, Z: 1}, {X: 5, Y: 38, Z: 1}} {
		id++
		editTile(t, document, coord, fmt.Sprint(id), id)
		assertFullHash(t, document)
	}
	// Server submissions apply to a clone; a discarded branch must not disturb
	// the document it came from, and both must keep hashing correctly.
	branch := document.Clone()
	id++
	editTile(t, branch, model.Coord{X: 2, Y: 2, Z: 1}, "branch", id)
	assertFullHash(t, branch)
	assertFullHash(t, document)
	id++
	editTile(t, document, model.Coord{X: 39, Y: 2, Z: 1}, "main", id)
	assertFullHash(t, document)
	id++
	editTile(t, branch, model.Coord{X: 39, Y: 2, Z: 1}, "branch-2", id)
	assertFullHash(t, branch)
}
