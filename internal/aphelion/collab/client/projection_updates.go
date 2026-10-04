package client

import (
	"cmp"
	"slices"

	"sdmm/internal/aphelion/collab/model"
)

// ProjectionUpdate pins immutable authority and visible state. Dirty coordinates
// include every skipped publication, including speculation that disappeared.
// Consumers may materialize individual tiles without copying the whole document.
type ProjectionUpdate struct {
	capture ProjectionCapture
	coords  []model.Coord
	full    bool
}

func (update ProjectionUpdate) Capture() ProjectionCapture   { return update.capture }
func (update ProjectionUpdate) Full() bool                   { return update.full }
func (update ProjectionUpdate) ChangedCoords() []model.Coord { return slices.Clone(update.coords) }

// Coalesce keeps this newer state and the older publication's dirty coverage.
// Neither immutable input is modified, including coordinates held by a reader.
func (update ProjectionUpdate) Coalesce(older ProjectionUpdate) ProjectionUpdate {
	update.full = update.full || older.full
	if update.full {
		update.coords = nil
		return update
	}
	seen := make(map[model.Coord]struct{}, len(update.coords)+len(older.coords))
	for _, coord := range older.coords {
		seen[coord] = struct{}{}
	}
	for _, coord := range update.coords {
		seen[coord] = struct{}{}
	}
	update.coords = make([]model.Coord, 0, len(seen))
	for coord := range seen {
		update.coords = append(update.coords, coord)
	}
	slices.SortFunc(update.coords, func(a, b model.Coord) int {
		if a.Z != b.Z {
			return cmp.Compare(a.Z, b.Z)
		}
		if a.Y != b.Y {
			return cmp.Compare(a.Y, b.Y)
		}
		return cmp.Compare(a.X, b.X)
	})
	return update
}

func (network *NetworkExecutor) captureProjectionLocked() ProjectionCapture {
	return ProjectionCapture{projection: network.projection, tileIndexes: network.tileIndexes, mapHash: network.acknowledgedHash,
		visibleTiles: network.visibleTiles, visibleCoords: network.visibleCoords}
}

func (network *NetworkExecutor) ProjectionChanges() <-chan ProjectionUpdate { return network.changes }

func (network *NetworkExecutor) publishChangesLocked(changes []model.TileChange) {
	current := network.captureProjectionLocked()
	update := ProjectionUpdate{capture: current,
		full: current.BaseRevision() != network.published.BaseRevision() && len(changes) == 0}
	if !update.full {
		update.coords = make([]model.Coord, 0, len(changes)+len(current.visibleCoords))
		for _, change := range changes {
			update.coords = append(update.coords, change.Coord)
		}
		update.coords = append(update.coords, current.visibleCoords...)
		update = update.Coalesce(ProjectionUpdate{coords: network.published.visibleCoords})
	}
	network.published = current
	// Exactly one producer owns the mutex. A concurrent consumer either already
	// has the old update or we merge its dirty coverage before replacing it.
	select {
	case older := <-network.changes:
		update = update.Coalesce(older)
	default:
	}
	network.changes <- update
}
