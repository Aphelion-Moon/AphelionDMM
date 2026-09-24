// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
package editor

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// FillSelection shares placement composition: hidden channels survive,
// singletons replace, and collections append or replace as explicitly chosen.
func (e *Editor) FillSelection(selection editing.Selection, prefab *dmmprefab.Prefab, replace bool) error {
	if !e.CanStartMapEdit() || selection.Level() != e.pMap.ActiveLevel() {
		return fmt.Errorf("finish the current edit on the visible level")
	}
	if selection.Len() == 0 || prefab == nil || prefab.Vars() == nil {
		return fmt.Errorf("select tiles and a prefab first")
	}
	filter := e.app.PathsFilter().Copy()
	if !filter.IsVisiblePath(prefab.Path()) {
		return fmt.Errorf("the selected prefab is excluded by the current filter")
	}
	source := model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
	for _, name := range prefab.Vars().Iterate() {
		value, ok := prefab.Vars().ExplicitValue(name)
		if !ok {
			return fmt.Errorf("prefab value %q is unavailable", name)
		}
		source.Vars[name] = value
	}
	channel := editing.ChannelForPath(source.Path)
	policy := editing.PastePolicy{Mode: editing.ApplyOver, Channels: 1 << channel}
	label := "Fill Selection"
	if replace {
		policy.Mode = editing.OnlyOverwriteWithData
		label = "Replace Selection Channel"
	}
	base := e.authoritativeTiles
	prepare := func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		bytes := uint64(1024)
		var failure error
		selection.Visit(func(p util.Point) {
			if failure != nil {
				return
			}
			if failure = ctx.Err(); failure != nil {
				return
			}
			state, ok := base[model.Coord{X: p.X, Y: p.Y, Z: p.Z}]
			if !ok {
				failure = fmt.Errorf("selection destination is unavailable")
				return
			}
			bytes = addWorkBytes(bytes, addWorkBytes(localTileBytes(state), localTileBytes(model.TileState{Prefabs: []model.PrefabState{source}})))
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
		changes := make([]model.TileChange, 0, selection.Len())
		selection.Visit(func(p util.Point) {
			if failure != nil {
				return
			}
			if failure = ctx.Err(); failure != nil {
				return
			}
			coord := model.Coord{X: p.X, Y: p.Y, Z: p.Z}
			before := base[coord]
			placed := source
			placed.StableID, failure = model.NewStableID()
			if failure != nil {
				return
			}
			intent := editing.TileIntent{}
			intent[channel] = editing.ChannelIntent{Action: editing.Set, Data: []model.PrefabState{placed}}
			after, err := editing.ComposeTile(before, intent, policy, filter.IsVisiblePath)
			if err != nil {
				failure = err
				return
			}
			changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
		})
		return changes, failure
	}
	if local, ok := e.executor.(localEditExecutor); ok && !e.sessionOwned {
		generation := e.attachmentGeneration
		return e.startLocalWork(local, true, 1024, prepare, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
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
				e.pushLocalCommandOwned(local, label, accepted.Changes, backward, nil)
			}
		})
	}
	reservation, err := e.editWorkBudget().Reserve(1024)
	if err != nil {
		return err
	}
	defer reservation.Release()
	changes, err := prepare(context.Background(), reservation)
	if err != nil {
		return err
	}
	coords := make([]util.Point, len(changes))
	for i, c := range changes {
		coords[i] = util.Point{X: c.Coord.X, Y: c.Coord.Y, Z: c.Coord.Z}
	}
	if !e.TryBeginTileChange(coords...) {
		return fmt.Errorf("unable to capture selection")
	}
	for _, change := range changes {
		if err := e.applyPasteTileState(change.Coord, change.After); err != nil {
			e.collaborationErr = err
			return err
		}
	}
	e.CommitOperation(label)
	return nil
}

// APHELION EDIT ADDITION END
