package engine

import (
	"sync"
	"sync/atomic"

	"sdmm/internal/aphelion/collab/model"
)

// ScopedTileCapture pins source tiles for a temporary worker. Release ends all
// reads, including through copied handles; later reads return unavailable.
// Public SnapshotCapture and TileCapture retain their unlimited lifetime.
type ScopedTileCapture struct{ state *scopedTileCapture }

type scopedTileCapture struct {
	mu      sync.RWMutex
	capture TileCapture
	readers *atomic.Int64
}

func (document *Document) CaptureScopedTiles() ScopedTileCapture {
	if document.scopedTileReaders == nil {
		document.scopedTileReaders = &atomic.Int64{}
	}
	document.scopedTileReaders.Add(1)
	// An old reader can outlive a table replacement, then observe a later
	// insertion. Keep the independent coordinate index immutable as well.
	document.sharedTileIndexes = true
	return ScopedTileCapture{state: &scopedTileCapture{
		capture: TileCapture{SnapshotCapture: SnapshotCapture{snapshot: document.snapshot}, indexes: document.tileIndexes},
		readers: document.scopedTileReaders,
	}}
}

func (capture ScopedTileCapture) DocumentID() model.DocumentID {
	if capture.state == nil {
		return ""
	}
	capture.state.mu.RLock()
	defer capture.state.mu.RUnlock()
	return capture.state.capture.DocumentID()
}

func (capture ScopedTileCapture) Revision() model.Revision {
	if capture.state == nil {
		return 0
	}
	capture.state.mu.RLock()
	defer capture.state.mu.RUnlock()
	return capture.state.capture.Revision()
}

func (capture ScopedTileCapture) Tile(coord model.Coord) (model.TileState, bool) {
	if capture.state == nil {
		return model.TileState{}, false
	}
	capture.state.mu.RLock()
	defer capture.state.mu.RUnlock()
	return capture.state.capture.Tile(coord)
}

func (capture ScopedTileCapture) EstimatedTileBytes(coord model.Coord) (uint64, bool) {
	if capture.state == nil {
		return 0, false
	}
	capture.state.mu.RLock()
	defer capture.state.mu.RUnlock()
	return capture.state.capture.EstimatedTileBytes(coord)
}

// Release waits for in-flight reads and drops retained source references before
// allowing the authority owner to reuse its table. It is safe to call again.
func (capture ScopedTileCapture) Release() {
	if capture.state == nil {
		return
	}
	capture.state.mu.Lock()
	defer capture.state.mu.Unlock()
	if capture.state.readers == nil {
		return
	}
	capture.state.capture = TileCapture{}
	capture.state.readers.Add(-1)
	capture.state.readers = nil
}
