package tools

import (
	"errors"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestGrabRotationUpdatesMoveSelectionOnlyOnSuccess(t *testing.T) {
	oldEditor := ed
	defer func() { ed = oldEditor }()
	m := &dmmap.Dmm{MaxX: 2, MaxY: 2, MaxZ: 1}
	for y := 1; y <= 2; y++ {
		for x := 1; x <= 2; x++ {
			m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}})
		}
	}
	ed = &lifecycleEditor{m: m}
	g := newGrab()
	g.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	before := g.Bounds()
	if err := g.Rotate(true, func(util.Bounds, int, bool) (util.Bounds, error) { return util.Bounds{}, errors.New("rejected") }); err == nil {
		t.Fatal("lost rejection")
	}
	if g.Bounds() != before {
		t.Fatal("rejected rotation changed selection")
	}
	g.dragging = true
	if err := g.Rotate(true, func(util.Bounds, int, bool) (util.Bounds, error) {
		t.Fatal("rotated open gesture")
		return util.Bounds{}, nil
	}); err == nil {
		t.Fatal("dragging rotation accepted")
	}
	g.dragging = false
	if err := g.Rotate(true, func(area util.Bounds, z int, cw bool) (util.Bounds, error) {
		if area != before || z != 1 || !cw {
			t.Fatal("wrong transform inputs")
		}
		return util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 2}, nil
	}); err != nil {
		t.Fatal(err)
	}
	coords := g.selectedCoordinates()
	if g.mode != tSelectModeMoveArea || len(coords) != 2 || coords[1] != (util.Point{X: 1, Y: 2, Z: 1}) || g.fillAreaInit != g.fillArea {
		t.Fatal("rotation left stale drag coordinates")
	}
}

func TestGrabMoveReadsCurrentContentsAfterUndo(t *testing.T) {
	oldEditor := ed
	defer func() { ed = oldEditor }()
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "4")
	tile.InstancesAdd(dmmprefab.New(0, "/obj/foo", vars.ToImmutable()))
	m := &dmmap.Dmm{MaxX: 2, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile, {Coord: util.Point{X: 2, Y: 1, Z: 1}}}}
	owner := &lifecycleEditor{m: m}
	ed = owner
	g := newGrab()
	g.SelectArea([]util.Point{tile.Coord})
	g.mode = tSelectModeMoveArea
	i := tile.Instances()[0]
	i.SetPrefab(dmmprefab.New(0, "/obj/foo", dmvars.Set(i.Prefab().Vars(), "dir", "2"))) // Acknowledged undo/remote update.
	g.onStart(tile.Coord)
	g.onMove(util.Point{X: 2, Y: 1, Z: 1})
	if len(owner.sourceDirs) != 1 || owner.sourceDirs[0] != "2" {
		t.Fatalf("pure move did not capture current post-undo source, got %v", owner.sourceDirs)
	}
	if m.Tiles[0].Instances()[0] != i || len(m.Tiles[1].Instances()) != 0 {
		t.Fatal("pure move hover replaced the source or populated its destination")
	}
}
