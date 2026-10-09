package repath

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestBuildInventoryCountsVariantsInOrder(t *testing.T) {
	known := func(path string) bool { return path == "/turf/open/floor" }
	tiles := map[model.Coord]model.TileState{
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{prefab(t, "/obj/b", map[string]string{"dir": "4"}), prefab(t, "/obj/b", map[string]string{"dir": "4"}), prefab(t, "/turf/open/floor", nil)}},
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{prefab(t, "/obj/b", nil), prefab(t, "/obj/a", nil), prefab(t, "/turf/open/floor", nil)}},
		{X: 1, Y: 2, Z: 1}: {Prefabs: []model.PrefabState{prefab(t, "/turf/open/floor", nil)}},
	}
	header := model.Snapshot{Revision: 7, MaxX: 2, MaxY: 2, MaxZ: 1}
	inventory, err := BuildInventory(context.Background(), header, readMap(tiles), known)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Revision != 7 || inventory.Instances != 4 || inventory.Tiles != 2 || len(inventory.Entries) != 2 {
		t.Fatalf("inventory = %#v", inventory)
	}
	a, b := inventory.Entries[0], inventory.Entries[1]
	if a.Path != "/obj/a" || a.Count != 1 || b.Path != "/obj/b" || b.Count != 3 || b.Tiles != 2 {
		t.Fatalf("entries = %#v", inventory.Entries)
	}
	if len(b.Variants) != 2 || b.Variants[0].Count != 2 || b.Variants[0].Vars["dir"] != "4" || b.Variants[1].Key != "" {
		t.Fatalf("variants = %#v", b.Variants)
	}
	if b.Samples[0] != (model.Coord{X: 1, Y: 1, Z: 1}) || b.Variants[0].Sample != (model.Coord{X: 2, Y: 1, Z: 1}) {
		t.Fatalf("samples = %#v", b.Samples)
	}
}

func TestBuildInventoryCapsVariants(t *testing.T) {
	tile := model.TileState{}
	for n := 0; n < maxVariants+3; n++ {
		tile.Prefabs = append(tile.Prefabs, prefab(t, "/obj/a", map[string]string{"n": string(rune('A' + n))}))
	}
	tiles := map[model.Coord]model.TileState{{X: 1, Y: 1, Z: 1}: tile}
	inventory, err := BuildInventory(context.Background(), model.Snapshot{MaxX: 1, MaxY: 1, MaxZ: 1}, readMap(tiles), func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if entry := inventory.Entries[0]; len(entry.Variants) != maxVariants || entry.Overflow != 3 || entry.Count != maxVariants+3 {
		t.Fatalf("entry = %d variants, overflow %d", len(entry.Variants), entry.Overflow)
	}
}

func TestPathHelpers(t *testing.T) {
	for path, want := range map[string]string{"/obj": "/obj", "/obj/item/x": "/obj", "": "", "obj": "", "//x": ""} {
		if got := Base(path); got != want {
			t.Errorf("Base(%q) = %q want %q", path, got, want)
		}
	}
	if SameBase("/obj", "/turf") || !SameBase("/obj/a", "/obj") || SameBase("", "") {
		t.Fatal("SameBase")
	}
	if Parent("/obj/a/b") != "/obj/a" || Parent("/obj") != "" || Leaf("/obj/a/b") != "b" {
		t.Fatal("Parent/Leaf")
	}
	index := testIndex(t, MapSource{"/obj": {}, "/obj/a": {}, "/obj/a/b": {}, "/obj/ab": {}})
	if got := index.WithPrefix("/obj/a", 2); len(got) != 2 || got[0] != "/obj/a" || got[1] != "/obj/a/b" {
		t.Fatalf("WithPrefix = %v", got)
	}
	if index.NearestAncestor("/obj/a/b/c/d") != "/obj/a/b" || index.NearestAncestor("/mob/x") != "" {
		t.Fatal("NearestAncestor")
	}
}
