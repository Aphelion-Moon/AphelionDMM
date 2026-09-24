package engine

import "sdmm/internal/aphelion/collab/model"

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
