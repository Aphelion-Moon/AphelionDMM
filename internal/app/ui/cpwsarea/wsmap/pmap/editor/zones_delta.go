// APHELION EDIT ADDITION START - AREA DELTAS
package editor

import (
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
)

type areaIndex struct {
	zone    int
	members map[util.Point]bool
	borders map[util.Point]int
}

func (e *Editor) updateAreaDelta(change model.TileChange) {
	if e.areaIndexes == nil {
		e.updateAreasZones()
		return
	}
	point := util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z}
	before, after := areaPaths(change.Before), areaPaths(change.After)
	for path := range before {
		if !after[path] {
			e.updateAreaMembership(path, point, false)
		}
	}
	for path := range after {
		if !before[path] {
			e.updateAreaMembership(path, point, true)
		}
	}
}

func areaPaths(state model.TileState) map[string]bool {
	paths := make(map[string]bool)
	for _, p := range state.Prefabs {
		if dm.IsPath(p.Path, "/area") {
			paths[p.Path] = true
		}
	}
	return paths
}

func (e *Editor) updateAreaMembership(path string, point util.Point, present bool) {
	e.areaBordersGeneration++
	index := e.areaIndexes[path]
	if index == nil {
		if !present {
			return
		}
		index = &areaIndex{zone: len(e.areasZones), members: make(map[util.Point]bool), borders: make(map[util.Point]int)}
		e.areaIndexes[path] = index
		e.areasZones = append(e.areasZones, AreaZone{Name: path})
	}
	if present {
		index.members[point] = true
	} else {
		delete(index.members, point)
	}
	zone := &e.areasZones[index.zone]
	recompute := func(coord util.Point) {
		dirs := 0
		if index.members[coord] {
			for shift, dir := range zoneDirs {
				if !index.members[coord.Plus(shift)] {
					dirs |= dir
				}
			}
		}
		position, exists := index.borders[coord]
		if dirs == 0 {
			if !exists {
				return
			}
			last := len(zone.Borders) - 1
			zone.Borders[position] = zone.Borders[last]
			index.borders[zone.Borders[position].Coord] = position
			zone.Borders[last] = AreaBorder{}
			zone.Borders = zone.Borders[:last]
			delete(index.borders, coord)
		} else if exists {
			zone.Borders[position].Dirs = dirs
		} else {
			index.borders[coord] = len(zone.Borders)
			zone.Borders = append(zone.Borders, AreaBorder{Coord: coord, Dirs: dirs})
		}
	}
	recompute(point)
	for shift := range zoneDirs {
		recompute(point.Plus(shift))
	}
	if len(index.members) == 0 {
		last := len(e.areasZones) - 1
		e.areasZones[index.zone] = e.areasZones[last]
		e.areaIndexes[e.areasZones[index.zone].Name].zone = index.zone
		e.areasZones[last] = AreaZone{}
		e.areasZones = e.areasZones[:last]
		delete(e.areaIndexes, path)
	}
}

// APHELION EDIT ADDITION END
