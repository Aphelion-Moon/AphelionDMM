package mapsave

import (
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type Metadata struct {
	Name   string
	Path   dmmap.DmmPath
	Backup string
}

// Project builds a private map for serialization without reading or mutating
// editor-global prefab storage or environment-linked variables.
func Project(snapshot model.Snapshot, metadata Metadata) (*dmmap.Dmm, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("validate save snapshot: %w", err)
	}
	cellCount, err := snapshot.CellCount()
	if err != nil {
		return nil, fmt.Errorf("count save snapshot cells: %w", err)
	}
	if len(snapshot.Tiles) != cellCount {
		return nil, fmt.Errorf("save snapshot has %d tiles, want %d", len(snapshot.Tiles), cellCount)
	}

	document := &dmmap.Dmm{
		Name:   metadata.Name,
		Path:   metadata.Path,
		Backup: metadata.Backup,
		MaxX:   snapshot.MaxX,
		MaxY:   snapshot.MaxY,
		MaxZ:   snapshot.MaxZ,
		Tiles:  make([]*dmmap.Tile, cellCount),
	}
	for _, sourceTile := range snapshot.Tiles {
		coord := sourceTile.Coord
		point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
		tile := &dmmap.Tile{Coord: point}
		instances := make(dmmap.Instances, len(sourceTile.State.Prefabs))
		for index, state := range sourceTile.State.Prefabs {
			variables := &dmvars.MutableVariables{}
			names := make([]string, 0, len(state.Vars))
			for name := range state.Vars {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				variables.Put(name, state.Vars[name])
			}
			prefab := dmmprefab.New(dmmprefab.IdNone, state.Path, variables.ToImmutable())
			instance := dmminstance.New(point, prefab)
			instance.SetStableID(string(state.StableID))
			instances[index] = instance
		}
		tile.Set(instances)
		document.Tiles[tileIndex(snapshot.MaxX, snapshot.MaxY, coord.X, coord.Y, coord.Z)] = tile
	}
	for index, tile := range document.Tiles {
		if tile == nil {
			return nil, fmt.Errorf("save snapshot is missing tile at index %d", index)
		}
	}
	return document, nil
}

func tileIndex(maxX, maxY, x, y, z int) int {
	return maxX*maxY*(z-1) + maxX*(y-1) + (x - 1)
}
