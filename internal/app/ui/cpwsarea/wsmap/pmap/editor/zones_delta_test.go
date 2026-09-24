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
