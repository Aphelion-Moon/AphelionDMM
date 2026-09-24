package editing

import (
	"reflect"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestAreaMaskConnectedGlobalAndExplicitOverrides(t *testing.T) {
	m := rotationMap()
	area := dmmprefab.New(0, "/area/room", (&dmvars.MutableVariables{}).ToImmutable())
	cells := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 1, Y: 2, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 4, Y: 4, Z: 1}}
	for _, p := range cells {
		m.GetTile(p).InstancesAdd(area)
	}
	different := dmmprefab.New(0, area.Path(), dmvars.Set(area.Vars(), "name", "\"same display\""))
	m.GetTile(util.Point{X: 2, Y: 2, Z: 1}).InstancesAdd(different)
	connected, err := SelectAreaMask(m, cells[0], false)
	if err != nil || connected.Len() != 3 || connected.Contains(util.Point{X: 2, Y: 2, Z: 1}) {
		t.Fatalf("connected mask: %+v %v", connected, err)
	}
	all, err := SelectAreaMask(m, cells[0], true)
	if err != nil || all.Len() != 4 {
		t.Fatalf("global mask: %+v %v", all, err)
	}
	m.GetTile(util.Point{X: 3, Y: 1, Z: 1}).InstancesAdd(area)
	if all.Len() != 4 {
		t.Fatal("persistent mask grew after area edit")
	}
}

func TestMaskTransformAndMovePreserveUnselectedHole(t *testing.T) {
	m := rotationMap()
	s, err := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 1, Y: 3, Z: 1}, {X: 3, Y: 1, Z: 1}, {X: 3, Y: 3, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	hole := util.Point{X: 2, Y: 2, Z: 1}
	before := m.GetTile(hole).Copy()
	plan, err := RotateMask(m, s, true, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tiles) != 4 {
		t.Fatalf("rotation expanded mask: %d", len(plan.Tiles))
	}
	for _, tile := range plan.Tiles {
		if tile.Coord == hole {
			t.Fatal("rotation writes hole")
		}
	}
	move, err := NewMaskMove(m, s, func(string) bool { return true }, func(p util.Point) error {
		if p == hole {
			t.Fatal("move captured hole")
		}
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := move.Preview(util.Point{X: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.GetTile(hole), &before) {
		t.Fatal("move changed a bounding-box hole")
	}
	move.Finish(true)
}
