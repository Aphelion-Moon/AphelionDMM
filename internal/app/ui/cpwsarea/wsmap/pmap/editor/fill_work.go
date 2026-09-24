// APHELION EDIT ADDITION START - BOUNDED FILL
package editor

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// TryScheduleFill consumes a large unshared Fill gesture before target
// enumeration/capture. Small edits and session-owned maps retain their adapter.
func (e *Editor) TryScheduleFill(bounds util.Bounds, z int, prefab *dmmprefab.Prefab, alternative, border bool) bool {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned {
		return false
	}
	width, height := int(bounds.X2-bounds.X1)+1, int(bounds.Y2-bounds.Y1)+1
	count := width * height
	if border && width > 1 && height > 1 {
		count = 2*width + 2*height - 4
	}
	if count <= directLocalTiles {
		return false
	}
	if !e.CanStartMapEdit() {
		return true
	}
	if bounds.X1 < 1 || bounds.Y1 < 1 || bounds.X2 > float32(e.dmm.MaxX) || bounds.Y2 > float32(e.dmm.MaxY) || z < 1 || z > e.dmm.MaxZ || prefab == nil || prefab.Vars() == nil {
		e.reportCollaborationError("Unable to fill selection", fmt.Errorf("fill target or prefab is invalid"))
		return true
	}
	source := model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
	for _, name := range prefab.Vars().Iterate() {
		value, exists := prefab.Vars().Value(name)
		if !exists {
			e.reportCollaborationError("Unable to fill selection", fmt.Errorf("prefab value %q is unavailable", name))
			return true
		}
		source.Vars[name] = value
	}
	removePath := ""
	if !alternative {
		if dm.IsPath(source.Path, "/area") {
			removePath = "/area"
		} else if dm.IsPath(source.Path, "/turf") {
			removePath = "/turf"
		}
	} else if dm.IsPath(source.Path, "/obj") {
		removePath = "/obj"
	}
	base, generation := e.authoritativeTiles, e.attachmentGeneration
	err := e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		bytes := uint64(1024)
		err := editing.VisitRectangle(ctx, bounds, z, border, func(p util.Point) error {
			state, exists := base[model.Coord{X: p.X, Y: p.Y, Z: p.Z}]
			if !exists {
				return fmt.Errorf("fill destination is unavailable")
			}
			bytes = addWorkBytes(bytes, addWorkBytes(localTileBytes(state), localTileBytes(model.TileState{Prefabs: []model.PrefabState{source}})))
			return nil
		})
		if err != nil {
			return nil, err
		}
		if bytes > ^uint64(0)/16 {
			bytes = ^uint64(0)
		} else {
			bytes *= 16
		}
		if err := reservation.Resize(bytes); err != nil {
			return nil, err
		}
		changes := make([]model.TileChange, 0, count)
		err = editing.VisitRectangle(ctx, bounds, z, border, func(p util.Point) error {
			coord := model.Coord{X: p.X, Y: p.Y, Z: p.Z}
			before := base[coord]
			after := model.TileState{Prefabs: make([]model.PrefabState, 0, len(before.Prefabs)+1)}
			for _, existing := range before.Prefabs {
				if removePath == "" || !dm.IsPath(existing.Path, removePath) {
					after.Prefabs = append(after.Prefabs, existing)
				}
			}
			placed := source
			id, err := model.NewStableID()
			if err != nil {
				return err
			}
			placed.StableID = id
			after.Prefabs = append(after.Prefabs, placed)
			changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
			return nil
		})
		return changes, err
	}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
		if generation != e.attachmentGeneration || e.mapViewClosed {
			return
		}
		if err != nil {
			if len(accepted.Changes) > 0 {
				e.collaborationErr = err
			}
			e.reportCollaborationError("Unable to fill selection", err)
			return
		}
		if len(accepted.Changes) > 0 {
			e.pushLocalCommandOwned(local, "Fill Atoms", accepted.Changes, backward, nil)
		}
	})
	if err != nil {
		e.reportCollaborationError("Unable to schedule fill", err)
	}
	return true
}

// APHELION EDIT ADDITION END
