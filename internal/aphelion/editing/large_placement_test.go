package editing

import (
	"testing"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestPlacementWholeLevelPreviewAndCancel(t *testing.T) {
	const width = 256
	m := &dmmap.Dmm{MaxX: width, MaxY: width, MaxZ: 1}
	source := make([]dmmap.Tile, 0, width*width)
	prefab := dmmprefab.New(0, "/obj/unknown", dmvars.Set(&dmvars.Variables{}, "raw", `list("a" = 12)`))
	for y := 1; y <= width; y++ {
		for x := 1; x <= width; x++ {
			coord := util.Point{X: x, Y: y, Z: 1}
			m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: coord})
			tile := dmmap.Tile{Coord: coord}
			tile.InstancesAdd(prefab)
			source = append(source, tile)
		}
	}
	captured := make(map[util.Point]bool)
	move, err := NewPlacement(m, source, 1, func(string) bool { return true }, func(p util.Point) error { captured[p] = true; return nil }, nil, func(p util.Point) { delete(captured, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != 0 {
		t.Fatal("constructor captured before preview")
	}
	if _, err := move.Preview(util.Point{}); err != nil {
		t.Fatal(err)
	}
	if len(captured) != width*width {
		t.Fatal("preview truncated")
	}
	seen := make(map[string]bool)
	for _, tile := range m.Tiles {
		if len(tile.Instances()) != 1 {
			t.Fatal("paste lost instance")
		}
		id := tile.Instances()[0].StableID()
		if id == "" || seen[id] {
			t.Fatal("copy identity missing or duplicated")
		}
		seen[id] = true
	}
	move.Finish(true)
	if len(captured) != 0 {
		t.Fatal("cancel retained capture")
	}
	for _, tile := range m.Tiles {
		if len(tile.Instances()) != 0 {
			t.Fatal("cancel left partial paste")
		}
	}
}
