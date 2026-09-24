// APHELION EDIT ADDITION START - LOCAL BULK PREPARATION
package editor

import (
	"context"
	"fmt"
	"maps"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/aphelion/search"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

func captureBulkPrefab(prefab *dmmprefab.Prefab) (*model.PrefabState, error) {
	if prefab == nil {
		return nil, nil
	}
	if prefab.Path() == "" || prefab.Vars() == nil {
		return nil, fmt.Errorf("prefab contents are unavailable")
	}
	result := &model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
	for _, key := range prefab.Vars().Iterate() {
		value, ok := prefab.Vars().ExplicitValue(key)
		if !ok {
			return nil, fmt.Errorf("prefab variable %q is unavailable", key)
		}
		result.Vars[key] = value
	}
	return result, nil
}

func (e *Editor) trySchedulePrefabBatch(old, replacement *dmmprefab.Prefab, label string) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || len(e.dmm.Tiles) <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	before, err := captureBulkPrefab(old)
	if err != nil || before == nil {
		e.reportCollaborationError("Unable to prepare edit", fmt.Errorf("source prefab is unavailable"))
		return true
	}
	after, err := captureBulkPrefab(replacement)
	if err != nil {
		e.reportCollaborationError("Unable to prepare edit", err)
		return true
	}
	base := e.authoritativeTiles
	visit := func(visitor func(model.Coord)) {
		for coord := range base {
			visitor(coord)
		}
	}
	match := func(_ model.Coord, p model.PrefabState) bool {
		return p.Path == before.Path && maps.Equal(p.Vars, before.Vars)
	}
	e.scheduleBulkEdit(local, label, visit, match, after, false, false)
	return true
}

func (e *Editor) tryScheduleSelectionDelete(selection editing.Selection) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || selection.Len() <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	filter := e.app.PathsFilter().Copy()
	visit := func(visitor func(model.Coord)) {
		selection.Visit(func(p util.Point) { visitor(model.Coord{X: p.X, Y: p.Y, Z: p.Z}) })
	}
	e.scheduleBulkEdit(local, "Delete Selection", visit, func(_ model.Coord, p model.PrefabState) bool { return filter.IsVisiblePath(p.Path) }, nil, false, true)
	return true
}

func (e *Editor) tryScheduleInstanceBatch(instances []*dmminstance.Instance, replacement *dmmprefab.Prefab, label string) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || len(instances) <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	// Exact UI pointer membership is checked once. Only immutable identity values
	// cross the worker boundary; the expensive before/after capture happens there.
	tiles, err := search.CurrentTiles(e.dmm, instances)
	if err != nil {
		e.reportCollaborationError("Unable to apply search action", err)
		return true
	}
	after, err := captureBulkPrefab(replacement)
	if err != nil {
		e.reportCollaborationError("Unable to apply search action", err)
		return true
	}
	ids := make(map[model.StableID]model.Coord, len(instances))
	for _, instance := range instances {
		p := instance.Coord()
		ids[model.StableID(instance.StableID())] = model.Coord{X: p.X, Y: p.Y, Z: p.Z}
	}
	coords := make([]model.Coord, len(tiles))
	for n, tile := range tiles {
		p := tile.Coord
		coords[n] = model.Coord{X: p.X, Y: p.Y, Z: p.Z}
	}
	visit := func(visitor func(model.Coord)) {
		for _, coord := range coords {
			visitor(coord)
		}
	}
	match := func(coord model.Coord, p model.PrefabState) bool { at, ok := ids[p.StableID]; return ok && at == coord }
	e.scheduleBulkEdit(local, label, visit, match, after, true, false)
	return true
}

// All bulk adapters share the existing document work owner, accepted revision,
// chunk publication and history path. There is no per-tool worker lifecycle.
func (e *Editor) scheduleBulkEdit(local localEditExecutor, label string, visit func(func(model.Coord)), match func(model.Coord, model.PrefabState) bool, replacement *model.PrefabState, sameBase, allTiles bool) {
	defaults := make([]model.PrefabState, 0, 2)
	if replacement == nil {
		for _, prefab := range []*dmmprefab.Prefab{dmmap.BaseTurf, dmmap.BaseArea} {
			value, err := captureBulkPrefab(prefab)
			if err != nil || value == nil {
				e.reportCollaborationError("Unable to prepare edit", fmt.Errorf("map defaults are unavailable"))
				return
			}
			defaults = append(defaults, *value)
		}
	}
	base, generation := e.authoritativeTiles, e.attachmentGeneration
	err := e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		bytes := uint64(1024)
		var failure error
		visit(func(coord model.Coord) {
			if failure != nil {
				return
			}
			if failure = ctx.Err(); failure != nil {
				return
			}
			state, ok := base[coord]
			if !ok {
				failure = fmt.Errorf("bulk destination is unavailable")
				return
			}
			bytes = addWorkBytes(bytes, localTileBytes(state))
		})
		if failure != nil {
			return nil, failure
		}
		if bytes > ^uint64(0)/16 {
			bytes = ^uint64(0)
		} else {
			bytes *= 16
		}
		if err := reservation.Resize(bytes); err != nil {
			return nil, err
		}
		var changes []model.TileChange
		visit(func(coord model.Coord) {
			if failure != nil {
				return
			}
			if failure = ctx.Err(); failure != nil {
				return
			}
			before := base[coord]
			after := model.TileState{Prefabs: make([]model.PrefabState, 0, len(before.Prefabs)+2)}
			changed := allTiles
			for _, prefab := range before.Prefabs {
				if !match(coord, prefab) || (replacement != nil && sameBase && !dm.IsPathBaseSame(prefab.Path, replacement.Path)) {
					after.Prefabs = append(after.Prefabs, prefab)
					continue
				}
				changed = true
				if replacement != nil {
					placed := *replacement
					placed.StableID = prefab.StableID
					after.Prefabs = append(after.Prefabs, placed)
				}
			}
			if !changed {
				return
			}
			for _, fallback := range defaults {
				found := false
				channel := editing.ChannelForPath(fallback.Path)
				for _, prefab := range after.Prefabs {
					if editing.ChannelForPath(prefab.Path) == channel {
						found = true
						break
					}
				}
				if !found {
					fallback.StableID, failure = model.NewStableID()
					if failure != nil {
						return
					}
					after.Prefabs = append(after.Prefabs, fallback)
				}
			}
			changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
		})
		return changes, failure
	}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
		if generation != e.attachmentGeneration || e.mapViewClosed {
			return
		}
		if err != nil {
			if len(accepted.Changes) > 0 {
				e.collaborationErr = err
			}
			e.reportCollaborationError("Unable to apply bulk edit", err)
			return
		}
		if len(accepted.Changes) > 0 {
			e.pushLocalCommandOwned(local, label, accepted.Changes, backward, nil)
		}
	})
	if err != nil {
		e.reportCollaborationError("Unable to schedule bulk edit", err)
	}
}

// APHELION EDIT ADDITION END
