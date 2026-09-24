// APHELION EDIT ADDITION START - ACCEPTED TILE DISPLAY
package editor

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func (e *Editor) applyPasteTileState(coord model.Coord, state model.TileState) error {
	point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
	if !e.dmm.HasTile(point) {
		return fmt.Errorf("accepted coordinate (%d,%d,%d) is outside the map", coord.X, coord.Y, coord.Z)
	}
	instances := make(dmmap.Instances, 0, len(state.Prefabs))
	for _, saved := range state.Prefabs {
		variables := &dmvars.MutableVariables{}
		for _, name := range sortedVariableNames(saved.Vars) {
			variables.Put(name, saved.Vars[name])
		}
		prefab := dmmprefab.New(dmmprefab.IdNone, saved.Path, variables.ToImmutable())
		if environment := e.app.LoadedEnvironment(); environment != nil {
			if object := environment.Objects[saved.Path]; object != nil {
				prefab.Vars().LinkParent(object.Vars)
			}
		}
		prefab = dmmap.PrefabStorage.Put(prefab)
		instance := dmminstance.New(point, prefab)
		instance.SetStableID(string(saved.StableID))
		instances = append(instances, instance)
	}
	e.dmm.GetTile(point).Set(instances)
	return nil
}

func (e *Editor) editWorkBudget() *resources.Budget {
	if e == nil || e.workBudget == nil {
		return resources.DefaultBudget()
	}
	return e.workBudget
}

func preparePlacementSource(ctx context.Context, source []dmmap.Tile) ([]dmmap.Tile, error) {
	prepared := make([]dmmap.Tile, len(source))
	seen := make(map[model.StableID]struct{})
	for tileIndex, sourceTile := range source {
		if tileIndex&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		tile := dmmap.Tile{Coord: sourceTile.Coord}
		instances := make(dmmap.Instances, 0, len(sourceTile.Instances()))
		for _, sourceInstance := range sourceTile.Instances() {
			if sourceInstance == nil || sourceInstance.Prefab() == nil || sourceInstance.Prefab().Vars() == nil {
				return nil, fmt.Errorf("paste source contains an invalid instance")
			}
			instance := sourceInstance.Copy()
			// Copies receive one identity for their entire placement lifetime.
			// Source identities belong to committed instances and cannot be reused.
			stableID := model.StableID("")
			for stableID == "" {
				generated, err := model.NewStableID()
				if err != nil {
					return nil, fmt.Errorf("assign paste source identity: %w", err)
				}
				if _, duplicate := seen[generated]; duplicate {
					continue
				}
				stableID = generated
			}
			seen[stableID] = struct{}{}
			instance.SetStableID(string(stableID))
			instances = append(instances, &instance)
		}
		tile.Set(instances)
		prepared[tileIndex] = tile
	}
	return prepared, nil
}

func (e *Editor) syncPasteSnapshot(coords []model.Coord) {
	initial := e.pMap.Snapshot().Initial()
	if initial == nil {
		return
	}
	// Dimension replacement is a full-install boundary, not an ordinary edit.
	if initial.MaxX != e.dmm.MaxX || initial.MaxY != e.dmm.MaxY || initial.MaxZ != e.dmm.MaxZ || len(initial.Tiles) != len(e.dmm.Tiles) {
		e.pMap.Snapshot().Sync()
		return
	}
	for _, coord := range coords {
		point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
		current, before := e.dmm.GetTile(point), initial.GetTile(point)
		if current != nil && before != nil {
			before.Set(current.Instances().Copy())
		}
	}
}

func sortedVariableNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// APHELION EDIT ADDITION END
