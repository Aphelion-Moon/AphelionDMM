// APHELION EDIT ADDITION START - LOCAL TRANSFORM PREPARATION
package editor

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func (e *Editor) tryScheduleSelectionTransform(source, destination editing.Selection, transform editing.PlacementTransform, label string) (bool, error) {
	local, ok := e.executor.(localEditExecutor)
	if !ok || e.sessionOwned || source.Len() <= directLocalTiles {
		return false, nil
	}
	if !e.CanStartMapEdit() {
		return true, fmt.Errorf("finish the current edit before transforming")
	}
	area := destination.Bounds()
	if area.X1 < 1 || area.Y1 < 1 || area.X2 > float32(e.dmm.MaxX) || area.Y2 > float32(e.dmm.MaxY) {
		return true, fmt.Errorf("transformed selection leaves the map")
	}
	defaults := make([]model.PrefabState, 0, 2)
	for _, prefab := range []*dmmprefab.Prefab{dmmap.BaseArea, dmmap.BaseTurf} {
		value, err := captureBulkPrefab(prefab)
		if err != nil || value == nil {
			return true, fmt.Errorf("map defaults are unavailable")
		}
		defaults = append(defaults, *value)
	}
	base, generation, outcome := e.authoritativeTiles, e.attachmentGeneration, e.selectionOutcome
	filter, environment := e.app.PathsFilter().Copy(), e.app.LoadedEnvironment()
	parent := func(path string) *dmvars.Variables {
		if environment != nil {
			if object, ok := environment.Objects[path]; ok {
				return object.Vars
			}
		}
		return nil
	}
	err := e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		// The owner prevents authority changes until this worker completes. Estimate
		// both footprints before allocating a union or cloning variable maps.
		bytes := uint64(1024)
		var failure error
		visit := func(p util.Point) {
			if failure != nil {
				return
			}
			if failure = ctx.Err(); failure != nil {
				return
			}
			state, ok := base[model.Coord{X: p.X, Y: p.Y, Z: p.Z}]
			if !ok {
				failure = fmt.Errorf("selection is outside the map")
				return
			}
			bytes = addWorkBytes(bytes, localTileBytes(state))
		}
		source.Visit(visit)
		destination.Visit(visit)
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
		return editing.TransformModelSelection(ctx, source, transform, filter.IsVisiblePath, func(c model.Coord) (model.TileState, bool) { state, ok := base[c]; return state, ok }, parent, defaults)
	}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
		if generation != e.attachmentGeneration || e.mapViewClosed {
			return
		}
		if err != nil {
			selectionApplied(outcome, false)
			if len(accepted.Changes) > 0 {
				e.collaborationErr = err
			}
			e.reportCollaborationError("Unable to transform selection", err)
			return
		}
		selectionApplied(outcome, len(accepted.Changes) > 0)
		if len(accepted.Changes) > 0 {
			e.pushLocalCommandOwned(local, label, accepted.Changes, backward, outcome)
		}
	})
	return true, err
}

// APHELION EDIT ADDITION END
