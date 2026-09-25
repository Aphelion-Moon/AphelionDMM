package mapping

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmenv"
)

func TestAcceptedRootIdentitySurvivesMoveAndUnrelatedEdit(t *testing.T) {
	env := &dmenv.Dme{Objects: map[string]*dmenv.Object{}}
	hash, _ := env.EnvironmentHash()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: hash, MaxX: 2, MaxY: 1, MaxZ: 1,
		Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-0123456789ac", Path: "/obj/modular_map_root", Vars: map[string]string{}}}}}}}
	rootID := func(generation uint64, parent string) string {
		source, err := FromAccepted(context.Background(), "parent.dmm", env, AcceptedSource{Snapshot: snapshotFixture{snapshot}, Generation: generation})
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		catalog := NewCatalog(env)
		defer catalog.Close()
		roots, _ := catalog.Roots(context.Background(), source, Transform{}, parent)
		if len(roots) != 1 {
			t.Fatalf("roots = %d", len(roots))
		}
		return roots[0].ID
	}
	before := rootID(1, "")
	snapshot.Tiles[0].Coord.X = 2
	snapshot.Revision++
	if after := rootID(1, ""); after != before {
		t.Fatal("moving the same accepted instance changed occurrence identity")
	}
	snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-0123456789ad", Path: "/obj/other", Vars: map[string]string{}}}}})
	if rootID(1, "") != before {
		t.Fatal("unrelated tile edit changed occurrence identity")
	}
	if rootID(2, "") == before || rootID(1, "another-parent") == before {
		t.Fatal("attachment or occurrence ancestry did not fence identity")
	}
	snapshot.Tiles[0].State.Prefabs[0].StableID = "01890f3e-7b5c-7abc-8def-0123456789ae"
	if rootID(1, "") == before {
		t.Fatal("recreated root inherited deleted identity")
	}
}
