package editing

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestPlacementRetainsCopiedIdentityAcrossTransformsAndTargets(t *testing.T) {
	ctx := context.Background()
	document, _ := model.NewDocumentID()
	actor, _ := model.NewActorID()
	snapshot := model.Snapshot{ProtocolVersion: 1, SchemaVersion: 1, DocumentID: document, EnvironmentHash: strings.Repeat("a", 64), MaxX: 4, MaxY: 4, MaxZ: 1}
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 4; x++ {
			snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}})
		}
	}
	source := []dmmap.Tile{{Coord: util.Point{X: 1, Y: 1, Z: 1}}, {Coord: util.Point{X: 2, Y: 1, Z: 1}}}
	for i := range source {
		for _, path := range []string{"/area/test", "/turf/test", "/obj/test"} {
			vars := &dmvars.MutableVariables{}
			vars.Put("dir", "2")
			source[i].InstancesAdd(dmmprefab.New(0, path, vars.ToImmutable()))
		}
		for _, instance := range source[i].Instances() {
			instance.SetStableID(string(testStableID(t)))
		}
	}
	identities := make(map[model.StableID]model.StableID)
	build := func(template []dmmap.Tile, target util.Point) model.Operation {
		t.Helper()
		lease, err := ReservePlacementMemory(snapshot, template, 0, resources.NewFixedBudget(32<<20))
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		op, err := BuildPlacementProposalReservedWithIdentities(ctx, snapshot, actor, template, func(string) bool { return true }, target, nil, lease, identities)
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	first := build(source, util.Point{X: 1, Y: 1, Z: 1})
	rotated, err := TransformPlacementTemplate(ctx, source, PlacementRotateRight)
	if err != nil {
		t.Fatal(err)
	}
	second := build(rotated, util.Point{X: 3, Y: 2, Z: 1})
	firstIDs := make(map[model.StableID]bool)
	for _, change := range first.Changes {
		for _, prefab := range change.After.Prefabs {
			firstIDs[prefab.StableID] = true
		}
	}
	for _, change := range second.Changes {
		for _, prefab := range change.After.Prefabs {
			if !firstIDs[prefab.StableID] {
				t.Fatal("transform or target change regenerated copied identity")
			}
			if prefab.Vars["dir"] != "8" {
				t.Fatalf("rotated dir = %s", prefab.Vars["dir"])
			}
		}
	}
}
