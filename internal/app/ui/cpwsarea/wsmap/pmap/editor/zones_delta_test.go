package editor

import (
	"reflect"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
	"testing"
)

func TestAreaDeltaMatchesFullRebuildAndInverse(t *testing.T) {
	e := selectionEditor(t)
	before := model.CloneTileState(e.authoritative.Tiles[0].State)
	after := model.CloneTileState(before)
	after.Prefabs[0].Path = "/area/other"
	change := model.TileChange{Coord: e.authoritative.Tiles[0].Coord, Before: before, After: after}
	if err := e.applyPasteTileState(change.Coord, after); err != nil {
		t.Fatal(err)
	}
	e.updateAreaDelta(change)
	want := areaBorderSet(e.AreasZones())
	e.updateAreasZones()
	if !reflect.DeepEqual(want, areaBorderSet(e.AreasZones())) {
		t.Fatal("delta borders differ from full rebuild")
	}
	change.Before, change.After = change.After, change.Before
	if err := e.applyPasteTileState(change.Coord, before); err != nil {
		t.Fatal(err)
	}
	e.updateAreaDelta(change)
	want = areaBorderSet(e.AreasZones())
	e.updateAreasZones()
	if !reflect.DeepEqual(want, areaBorderSet(e.AreasZones())) {
		t.Fatal("inverse borders differ from full rebuild")
	}
}
func areaBorderSet(zones []AreaZone) map[string]map[util.Point]int {
	result := make(map[string]map[util.Point]int)
	for _, zone := range zones {
		result[zone.Name] = make(map[util.Point]int)
		for _, border := range zone.Borders {
			result[zone.Name][border.Coord] = border.Dirs
		}
	}
	return result
}

func TestAreaGenerationChangesOnlyWithMembership(t *testing.T) {
	e := &Editor{areaIndexes: make(map[string]*areaIndex)}
	e.updateAreaMembership("/area/a", util.Point{X: 1, Y: 1, Z: 1}, true)
	e.updateAreaMembership("/area/b", util.Point{X: 3, Y: 1, Z: 1}, true)
	generation := func(path string) uint64 { return e.areasZones[e.areaIndexes[path].zone].Generation }
	a, b := generation("/area/a"), generation("/area/b")
	if a == 0 || b == 0 {
		t.Fatal("new area geometry has no version")
	}
	e.updateAreaMembership("/area/a", util.Point{X: 2, Y: 1, Z: 1}, true)
	if generation("/area/a") == a || generation("/area/b") != b {
		t.Fatal("membership invalidated unrelated area geometry")
	}
	changed := generation("/area/a")
	state := model.TileState{Prefabs: []model.PrefabState{{Path: "/area/a"}, {Path: "/obj/a"}}}
	after := model.CloneTileState(state)
	after.Prefabs[1].Path = "/obj/b"
	e.updateAreaDelta(model.TileChange{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: state, After: after})
	if generation("/area/a") != changed || generation("/area/b") != b {
		t.Fatal("object edit invalidated area geometry")
	}
}
