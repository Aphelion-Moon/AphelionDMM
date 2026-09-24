package editing

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestBuildPlacementProposalPreservesUnknownAndHiddenValues(t *testing.T) {
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	document, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion,
		SchemaVersion:   model.SchemaVersion,
		DocumentID:      document,
		EnvironmentHash: strings.Repeat("a", 64),
		MaxX:            2,
		MaxY:            1,
		MaxZ:            1,
		Tiles: []model.Tile{
			{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: testStableID(t), Path: "/area", Vars: map[string]string{"tag": `"kept"`}}, {StableID: testStableID(t), Path: "/obj/old", Vars: map[string]string{"opaque": `list(1, 2)`}}}}},
			{Coord: model.Coord{X: 2, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: testStableID(t), Path: "/area", Vars: map[string]string{"tag": `"hidden"`}}, {StableID: testStableID(t), Path: "/turf", Vars: map[string]string{"kind": `"hidden"`}}}}},
		},
	}
	sourceVars := &dmvars.MutableVariables{}
	sourceVars.Put("unknown", `list("a", 42)`)
	source := []dmmap.Tile{{Coord: util.Point{X: 7, Y: 9, Z: 3}}}
	source[0].InstancesAdd(dmmprefab.New(0, "/obj/custom", sourceVars.ToImmutable()))

	operation, err := BuildPlacementProposal(context.Background(), snapshot, actor, source, func(path string) bool {
		return path != "/area" && path != "/turf"
	}, util.Point{X: 2, Y: 1, Z: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(operation.Changes) != 1 || operation.Changes[0].Coord != (model.Coord{X: 2, Y: 1, Z: 1}) {
		t.Fatalf("proposal changes = %+v", operation.Changes)
	}
	change := operation.Changes[0]
	if change.Before.Prefabs[0].Path != "/area" || change.Before.Prefabs[0].Vars["tag"] != `"hidden"` {
		t.Fatalf("before hidden prefab was not preserved: %+v", change.Before)
	}
	if len(change.After.Prefabs) != 3 || change.After.Prefabs[0].Path != "/area" || change.After.Prefabs[1].Path != "/turf" || change.After.Prefabs[2].Path != "/obj/custom" {
		t.Fatalf("after state did not retain hidden content and add source: %+v", change.After)
	}
	if change.After.Prefabs[2].Vars["unknown"] != `list("a", 42)` || change.After.Prefabs[2].StableID == "" {
		t.Fatalf("copied unknown values or stable identity missing: %+v", change.After.Prefabs[2])
	}
	if change.After.Prefabs[2].StableID == change.Before.Prefabs[0].StableID {
		t.Fatal("paste reused an existing instance identity")
	}
	if !snapshot.Tiles[1].State.Equal(change.Before) {
		t.Fatal("proposal mutated its immutable base snapshot")
	}
}

func TestBuildPlacementProposalChecksBoundsAndCancellationBeforeReturning(t *testing.T) {
	actor, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	document, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: document, EnvironmentHash: strings.Repeat("a", 64), MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}}}}
	source := []dmmap.Tile{{Coord: util.Point{X: 1, Y: 1, Z: 1}}}

	if _, err := BuildPlacementProposal(context.Background(), snapshot, actor, source, func(string) bool { return true }, util.Point{X: 2, Y: 1, Z: 1}, nil); err == nil {
		t.Fatal("out-of-bounds paste was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildPlacementProposal(ctx, snapshot, actor, source, func(string) bool { return true }, util.Point{X: 1, Y: 1, Z: 1}, nil); err == nil {
		t.Fatal("cancelled paste proposal was accepted")
	}
}

func testStableID(t *testing.T) model.StableID {
	t.Helper()
	id, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
