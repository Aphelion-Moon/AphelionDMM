package tools

import (
	"reflect"
	"testing"

	"sdmm/internal/util"
)

func TestGrabNudgeUsesCurrentContentsAndKeepsSelection(t *testing.T) {
	g, e := lifecycleFixture(t)
	id := e.m.Tiles[0].Instances()[0].StableID()
	if err := g.Nudge(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	if e.commits != 1 || !g.Stale() || g.Bounds() != (util.Bounds{X1: 2, Y1: 1, X2: 2, Y2: 1}) {
		t.Fatal("nudge did not commit once and move the selection")
	}
	if e.m.Tiles[1].Instances()[0].StableID() != id || g.selectedCoordinates()[0].X != 2 {
		t.Fatal("nudge lost instance identity or selection coordinates")
	}
	// A subsequent mouse gesture starts from the nudged selection.
	g.onStart(util.Point{X: 2, Y: 1, Z: 1})
	g.onMove(util.Point{X: 3, Y: 1, Z: 1})
	g.onStop(util.Point{X: 3, Y: 1, Z: 1})
	if e.m.Tiles[2].Instances()[0].StableID() != id || e.commits != 2 {
		t.Fatal("drag after nudge did not use the moved source")
	}
}

func TestGrabNudgeMultipleTilesIsOneMove(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		t.Run(map[bool]string{false: "horizontal", true: "vertical"}[vertical], func(t *testing.T) {
			g, e := lifecycleFixture(t)
			shift := util.Point{X: 2}
			if vertical {
				e.m.MaxX, e.m.MaxY = 1, 4
				for _, tile := range e.m.Tiles {
					tile.Coord.X, tile.Coord.Y = tile.Coord.Y, tile.Coord.X
				}
				shift = util.Point{Y: 2}
			}
			id := e.m.Tiles[0].Instances()[0].StableID()
			passed := e.m.Tiles[1].Copy()
			if err := g.Nudge(shift); err != nil {
				t.Fatal(err)
			}
			if e.commits != 1 || e.m.Tiles[2].Instances()[0].StableID() != id || !reflect.DeepEqual(*e.m.Tiles[1], passed) {
				t.Fatal("multi-tile nudge changed passed-over contents, identity or operation count")
			}
			before := e.m.Copy()
			area := g.Bounds()
			err := g.Nudge(shift)
			// Dmm.Copy normalizes empty instance slices; compare two copies.
			after := e.m.Copy()
			if err == nil || !reflect.DeepEqual(after, before) || g.Bounds() != area || e.commits != 1 {
				t.Fatal("out-of-map grid step was clipped or changed map/history")
			}
			if err := g.Nudge(util.Point{X: -shift.X, Y: -shift.Y}); err != nil {
				t.Fatal(err)
			}
			if e.commits != 2 || e.m.Tiles[0].Instances()[0].StableID() != id || g.Bounds() != (util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}) {
				t.Fatal("negative grid step failed to restore the selection position")
			}
		})
	}
}

func TestGrabNudgeRejectsInvalidMovesWithoutMutation(t *testing.T) {
	g, e := lifecycleFixture(t)
	before := e.m.Copy()
	area := g.Bounds()
	for _, shift := range []util.Point{{}, {X: -1}, {X: 4}, {Z: 1}, {X: 1, Y: 1}} {
		if err := g.Nudge(shift); err == nil {
			t.Fatalf("accepted invalid nudge %v", shift)
		}
		if !reflect.DeepEqual(e.m, &before) || g.Bounds() != area || e.commits != 0 {
			t.Fatalf("invalid nudge %v changed the map, selection or history", shift)
		}
	}
	g.onStart(util.Point{X: 1, Y: 1, Z: 1})
	if err := g.Nudge(util.Point{X: 1}); err == nil {
		t.Fatal("nudged during a mouse gesture")
	}
	g.OnDeselect()
	if err := g.Nudge(util.Point{X: 1}); err == nil {
		t.Fatal("nudged without a selection")
	}
}
