// APHELION EDIT ADDITION START - PATH MIGRATION
package editor

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

type PathMigrationResult struct {
	Report repath.Report
	Err    error
}

type bulkCapableExecutor interface {
	BulkEditsEnabled() bool
}

func pathMigrationDefaults() (repath.Defaults, error) {
	area, err := captureBulkPrefab(dmmap.BaseArea)
	if err != nil {
		return repath.Defaults{}, err
	}
	turf, err := captureBulkPrefab(dmmap.BaseTurf)
	if err != nil {
		return repath.Defaults{}, err
	}
	return repath.Defaults{Area: area, Turf: turf}, nil
}

func touchedCoords(tiles map[model.Coord]model.TileState, t *repath.Transformer) []model.Coord {
	var coords []model.Coord
	for coord, state := range tiles {
		for _, prefab := range state.Prefabs {
			if t.Touches(prefab.Path) {
				coords = append(coords, coord)
				break
			}
		}
	}
	sort.Slice(coords, func(i, j int) bool {
		a, b := coords[i], coords[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return coords
}

func visitCoords(coords []model.Coord) func(func(model.Coord) bool) {
	return func(visit func(model.Coord) bool) {
		for _, coord := range coords {
			if !visit(coord) {
				return
			}
		}
	}
}

// ApplyPathMigration rewrites the transformer's unknown paths as one
// operation with one history entry. Unshared documents prepare on the local
// work owner; session documents submit one tile-change operation. Failure
// before acceptance changes nothing. done runs on the UI thread after an
// unshared edit is accepted, or after a session operation is submitted.
func (e *Editor) ApplyPathMigration(t *repath.Transformer, label string, done func(PathMigrationResult)) error {
	if t == nil {
		return fmt.Errorf("no path migration plan")
	}
	if !e.CanStartMapEdit() || e.HasPastePlacement() || e.localWork != nil {
		return fmt.Errorf("finish or cancel the current map edit first")
	}
	defaults, err := pathMigrationDefaults()
	if err != nil {
		return err
	}
	ids := func() (model.StableID, error) { return model.NewStableID() }
	if local, ok := e.executor.(localEditExecutor); ok && !e.sessionOwned {
		return e.applyLocalPathMigration(local, t, label, ids, defaults, done)
	}
	return e.applySessionPathMigration(t, label, ids, defaults, done)
}

func (e *Editor) applyLocalPathMigration(local localEditExecutor, t *repath.Transformer, label string, ids repath.IDSource, defaults repath.Defaults, done func(PathMigrationResult)) error {
	base, generation := e.authoritativeTiles, e.attachmentGeneration
	report := repath.NewReport()
	return e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
		coords := touchedCoords(base, t)
		bytes := uint64(1024)
		for _, coord := range coords {
			bytes = addWorkBytes(bytes, localTileBytes(base[coord]))
		}
		if bytes > ^uint64(0)/4 {
			bytes = ^uint64(0)
		} else {
			bytes *= 4
		}
		if err := reservation.Resize(bytes); err != nil {
			return nil, err
		}
		read := func(coord model.Coord) (model.TileState, bool) {
			state, ok := base[coord]
			return state, ok
		}
		changes, built, err := repath.BuildChanges(ctx, visitCoords(coords), read, t, ids, defaults)
		report = built
		return changes, err
	}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
		if generation != e.attachmentGeneration || e.mapViewClosed {
			return
		}
		if err == nil && len(accepted.Changes) > 0 {
			e.pushLocalCommandOwned(local, label, accepted.Changes, backward, nil)
		}
		if done != nil {
			done(PathMigrationResult{Report: report, Err: err})
		}
	})
}

func (e *Editor) applySessionPathMigration(t *repath.Transformer, label string, ids repath.IDSource, defaults repath.Defaults, done func(PathMigrationResult)) error {
	coords := touchedCoords(e.authoritativeTiles, t)
	if len(coords) == 0 {
		if done != nil {
			done(PathMigrationResult{Report: repath.NewReport()})
		}
		return nil
	}
	if len(coords) > protocol.MaxOperationChanges {
		if bulk, ok := e.executor.(bulkCapableExecutor); !ok || !bulk.BulkEditsEnabled() {
			return fmt.Errorf("this migration touches %d tiles; the session accepts at most %d per edit unless the host enables bulk edits", len(coords), protocol.MaxOperationChanges)
		}
	}
	points := make([]util.Point, len(coords))
	for n, coord := range coords {
		points[n] = util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
	}
	if !e.TryBeginTileChange(points...) {
		return fmt.Errorf("unable to capture the affected tiles")
	}
	release := func() { clear(e.pendingChanges) }
	report := repath.NewReport()
	afters := make([]model.TileState, len(coords))
	changed := make([]bool, len(coords))
	for n, coord := range coords {
		after, ok, err := t.Tile(coord, e.pendingChanges[coord], ids, defaults, &report)
		if err != nil {
			release()
			return err
		}
		afters[n], changed[n] = after, ok
	}
	if len(report.Conflicts) != 0 {
		release()
		first := report.Conflicts[0]
		return fmt.Errorf("%d tiles would lose or gain an area or turf, first at (%d,%d,%d): %s", len(report.Conflicts), first.Coord.X, first.Coord.Y, first.Coord.Z, first.Msg)
	}
	// Prepare every display tile before changing any, so a failure leaves the
	// display and the captured before-states untouched.
	prepared := make([]dmmap.Instances, len(coords))
	for n, coord := range coords {
		if !changed[n] {
			continue
		}
		instances, err := e.preparePasteTileState(coord, afters[n])
		if err != nil {
			release()
			return err
		}
		prepared[n] = instances
	}
	for n, coord := range coords {
		if changed[n] {
			e.dmm.GetTile(util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}).Set(prepared[n])
		}
	}
	for _, ok := range changed {
		if ok {
			report.Tiles++
		}
	}
	e.CommitOperation(label)
	if done != nil {
		done(PathMigrationResult{Report: report})
	}
	return nil
}

// APHELION EDIT ADDITION END
