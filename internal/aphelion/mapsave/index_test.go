package mapsave

import (
	"testing"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestNormalizeSharesOnlyEqualSortedStacks(t *testing.T) {
	makeTile := func(coord util.Point, paths ...string) *dmmap.Tile {
		tile := &dmmap.Tile{Coord: coord}
		for _, path := range paths {
			tile.InstancesAdd(dmmprefab.New(dmmprefab.IdNone, path, &dmvars.Variables{}))
		}
		return tile
	}
	first := util.Point{X: 1, Y: 1, Z: 1}
	second := util.Point{X: 2, Y: 1, Z: 1}
	changed := util.Point{X: 3, Y: 1, Z: 1}
	stacks := Normalize(&dmmap.Dmm{
		MaxX: 3, MaxY: 1, MaxZ: 1,
		Tiles: []*dmmap.Tile{
			makeTile(first, "/area/test", "/turf/test", "/obj/repeated"),
			makeTile(second, "/area/test", "/turf/test", "/obj/repeated"),
			makeTile(changed, "/area/test", "/turf/test", "/obj/changed"),
		},
	})
	if stacks[first].Hash != stacks[second].Hash || &stacks[first].Prefabs[0] != &stacks[second].Prefabs[0] {
		t.Fatal("equal normalized stacks were not shared")
	}
	if stacks[first].Hash == stacks[changed].Hash || stacks[first].Prefabs.Equals(stacks[changed].Prefabs) {
		t.Fatal("different normalized stack was aliased")
	}
}

func TestContentIndexHandlesHashCollisionsAndKeyMoves(t *testing.T) {
	first := dmmdata.Prefabs{dmmprefab.New(dmmprefab.IdNone, "/obj/first", &dmvars.Variables{})}
	second := dmmdata.Prefabs{dmmprefab.New(dmmprefab.IdNone, "/obj/second", &dmvars.Variables{})}
	index := NewContentIndex(nil)
	index.Add(7, "a", first)
	index.Add(7, "b", second)
	if key, ok := index.Find(7, second); !ok || key != "b" {
		t.Fatalf("collision resolved to %q, %t", key, ok)
	}
	index.Add(8, "a", second)
	if _, ok := index.Find(7, first); ok {
		t.Fatal("moved key remained in its stale hash bucket")
	}
	if key, ok := index.Find(8, second); !ok || key != "a" {
		t.Fatalf("moved key lookup = %q, %t", key, ok)
	}
}

func TestLocationsByKeyIndexesOnlyOriginalCoordinates(t *testing.T) {
	a, b := util.Point{X: 1, Y: 1, Z: 1}, util.Point{X: 2, Y: 1, Z: 1}
	index := LocationsByKey(&dmmdata.DmmData{Grid: dmmdata.DataGrid{a: "a", b: "b", {X: 3, Y: 1, Z: 1}: "a"}})
	if len(index["a"]) != 2 || index["a"][0] != a || index["a"][1].X != 3 || len(index["b"]) != 1 || index["b"][0] != b {
		t.Fatalf("location index = %#v", index)
	}
}
