// APHELION EDIT ADDITION START - DOCKING OVERLAY
package pmap

import (
	"testing"

	"sdmm/internal/aphelion/docking"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func dockingTestMap(t *testing.T) *dmmap.Dmm {
	t.Helper()
	dmm := &dmmap.Dmm{MaxX: 10, MaxY: 10, MaxZ: 1}
	dmm.Tiles = make([]*dmmap.Tile, 100)
	for y := 1; y <= 10; y++ {
		for x := 1; x <= 10; x++ {
			tile := &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}}
			tile.InstancesSet(dmmdata.Prefabs{dmmprefab.New(dmmprefab.IdNone, "/turf/open/space", nil)})
			dmm.Tiles[(y-1)*10+(x-1)] = tile
		}
	}
	return dmm
}

func TestBuildDockingEntriesInheritsEnvironmentVars(t *testing.T) {
	dmm := dockingTestMap(t)
	parent := dmvars.FromParent(nil)
	parent = dmvars.Set(parent, "width", "3")
	parent = dmvars.Set(parent, "height", "2")
	vars := dmvars.FromParent(parent)
	vars = dmvars.Set(vars, "dir", "8")
	dmm.GetTile(util.Point{X: 5, Y: 5, Z: 1}).InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/docking_port/stationary/x", vars))
	dmm.GetTile(util.Point{X: 5, Y: 5, Z: 1}).InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/item", nil))
	dmm.GetTile(util.Point{X: 5, Y: 5, Z: 1}).InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/turf/open/floor", nil))

	entries := buildDockingEntries(dmm, 1)
	if len(entries) != 1 || !entries[0].Known {
		t.Fatalf("entries %+v", entries)
	}
	// WEST (shuttle.dm:81-108): width runs along +y, height along -x.
	if got, want := entries[0].Rect, (docking.Rect{MinX: 4, MinY: 5, MaxX: 5, MaxY: 7}); got != want {
		t.Fatalf("rect %+v want %+v", got, want)
	}
	if entries[0].Warnings&docking.WarnNonSpace == 0 {
		t.Fatal("floor under the port tile must warn for a stationary port")
	}
	if buildDockingEntries(dmm, 2) != nil {
		t.Fatal("other level must be empty")
	}
}

func TestDockingOverlayCacheReusesUntilInputsChange(t *testing.T) {
	dmm := dockingTestMap(t)
	dmm.GetTile(util.Point{X: 2, Y: 2, Z: 1}).InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/docking_port/mobile", dmvars.Set(dmvars.Set(dmvars.FromParent(nil), "width", "1"), "height", "1")))
	var cache dockingOverlayCache
	first := cache.resolve(dmm, nil, 1)
	if len(first) != 1 {
		t.Fatal("expected one port")
	}
	dmm.GetTile(util.Point{X: 3, Y: 3, Z: 1}).InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/docking_port/mobile", dmvars.Set(dmvars.Set(dmvars.FromParent(nil), "width", "1"), "height", "1")))
	if len(cache.resolve(dmm, nil, 1)) != 1 {
		t.Fatal("cache must not rescan without a version change")
	}
	if len(cache.resolve(dmm, nil, 2)) != 0 {
		t.Fatal("level change must recompute")
	}
}

// APHELION EDIT ADDITION END
