package editor

import (
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func authoritativeCopyFixture(cells int) model.Snapshot {
	snapshot := model.Snapshot{Tiles: make([]model.Tile, cells)}
	for index := range snapshot.Tiles {
		snapshot.Tiles[index] = model.Tile{
			Coord: model.Coord{X: index + 1, Y: 1, Z: 1},
			State: model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/test", Vars: map[string]string{"dir": "2"}}}},
		}
	}
	return snapshot
}

func TestAuthoritativePublicationKeepsInputAndPriorCapturesDetached(t *testing.T) {
	input := authoritativeCopyFixture(1)
	editor := &Editor{}
	editor.setAuthoritative(input)
	coord := input.Tiles[0].Coord
	prior := editor.authoritativeTiles
	input.Tiles[0].State.Prefabs[0].Vars["dir"] = "4"
	if prior[coord].Prefabs[0].Vars["dir"] != "2" || editor.authoritative.Tiles[0].State.Prefabs[0].Vars["dir"] != "2" {
		t.Fatal("borrowed input changed the published authority")
	}
	editor.setAuthoritative(input)
	if prior[coord].Prefabs[0].Vars["dir"] != "2" {
		t.Fatal("new publication changed a worker's prior capture")
	}
	if editor.authoritativeTiles[coord].Prefabs[0].Vars["dir"] != "4" || editor.authoritativePositions[coord] != 0 {
		t.Fatal("new authority indexes do not match the snapshot")
	}
}

func TestAuthoritativePublicationDoesNotDuplicateTilePayloads(t *testing.T) {
	input := authoritativeCopyFixture(1000)
	var copy model.Snapshot
	cloneAllocs := testing.AllocsPerRun(5, func() { copy = model.CloneSnapshot(input) })
	editor := &Editor{}
	publicationAllocs := testing.AllocsPerRun(5, func() { editor.setAuthoritative(input) })
	// Two coordinate indexes need map storage, but publication must not add
	// another allocation for every prefab and variable map in the document.
	if publicationAllocs > cloneAllocs+128 {
		t.Fatalf("publication duplicates tile payloads: %.0f allocations versus %.0f for the isolated snapshot", publicationAllocs, cloneAllocs)
	}
	if len(copy.Tiles) != len(editor.authoritativeTiles) {
		t.Fatal("publication lost tiles")
	}
}

func BenchmarkAuthoritativePublication(b *testing.B) {
	input := authoritativeCopyFixture(10000)
	editor := &Editor{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		editor.setAuthoritative(input)
	}
}
