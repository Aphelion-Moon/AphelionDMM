package engine

import (
	"math"

	"sdmm/internal/aphelion/collab/model"
)

// SnapshotCapture pins immutable tile payloads at one revision without copying
// a document on the UI owner. Materialization may run on a different goroutine;
// later mutations copy the tile table before replacing any retained entries.
type SnapshotCapture struct{ snapshot model.Snapshot }

func (document *Document) CaptureSnapshot() SnapshotCapture {
	document.sharedTiles = true
	return SnapshotCapture{snapshot: document.snapshot}
}
func (capture SnapshotCapture) Snapshot() model.Snapshot {
	return model.CloneSnapshot(capture.snapshot)
}
func (capture SnapshotCapture) DocumentID() model.DocumentID { return capture.snapshot.DocumentID }
func (capture SnapshotCapture) Revision() model.Revision     { return capture.snapshot.Revision }

// EstimatedBytes conservatively estimates the allocations made by Snapshot.
// It only reads pinned immutable state and does not allocate, so a worker can
// reserve capacity before materializing the captured revision.
func (capture SnapshotCapture) EstimatedBytes() uint64 {
	return EstimateSnapshotBytes(capture.snapshot)
}

// EstimateSnapshotBytes covers a cloned tile table, prefab slices, variable
// maps, and their contents. It includes a fixed allowance for per-map overhead.
func EstimateSnapshotBytes(snapshot model.Snapshot) uint64 {
	bytes := uint64(1 << 20)
	for _, tile := range snapshot.Tiles {
		bytes = addEstimatedBytes(bytes, EstimateTileStateBytes(tile.State))
	}
	return bytes
}

// EstimateTileStateBytes covers one cloned tile state and its variable maps.
func EstimateTileStateBytes(state model.TileState) uint64 {
	bytes := uint64(96)
	for _, prefab := range state.Prefabs {
		bytes = addEstimatedBytes(bytes, 96+uint64(len(prefab.Path))+uint64(len(prefab.StableID)))
		for name, value := range prefab.Vars {
			bytes = addEstimatedBytes(bytes, 48+uint64(len(name))+uint64(len(value)))
		}
	}
	return bytes
}

func addEstimatedBytes(total, add uint64) uint64 {
	if add > math.MaxUint64-total {
		return math.MaxUint64
	}
	return total + add
}

// TileCapture additionally pins the coordinate index for sparse worker reads.
// Keep it separate from a save capture, which never needs that index.
type TileCapture struct {
	SnapshotCapture
	indexes map[model.Coord]int
}

func (document *Document) CaptureTiles() TileCapture {
	document.sharedIndexes = true
	return TileCapture{SnapshotCapture: document.CaptureSnapshot(), indexes: document.tileIndexes}
}
func (capture TileCapture) Tile(coord model.Coord) (model.TileState, bool) {
	index, ok := capture.indexes[coord]
	if !ok {
		return model.TileState{}, false
	}
	return model.CloneTileState(capture.snapshot.Tiles[index].State), true
}

// EstimatedTileBytes estimates the clone work for one pinned tile without
// materializing its prefab or variable state.
func (capture TileCapture) EstimatedTileBytes(coord model.Coord) (uint64, bool) {
	index, ok := capture.indexes[coord]
	if !ok {
		return 0, false
	}
	return EstimateTileStateBytes(capture.snapshot.Tiles[index].State), true
}
