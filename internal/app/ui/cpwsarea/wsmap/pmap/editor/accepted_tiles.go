// APHELION EDIT ADDITION START - ACCEPTED SELECTION CAPTURE
package editor

import (
	"context"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// AcceptedTileCapture pins a validated revision for sparse off-thread readers.
// Header has no tile table; every Tile result belongs to the caller.
type AcceptedTileCapture struct {
	header   model.Snapshot
	read     func(model.Coord) (model.TileState, bool)
	estimate func(model.Coord) (uint64, bool)
}

func (capture AcceptedTileCapture) Header() model.Snapshot { return capture.header }
func (capture AcceptedTileCapture) Tile(coord model.Coord) (model.TileState, bool) {
	return capture.read(coord)
}
func (capture AcceptedTileCapture) EstimatedTileBytes(coord model.Coord) (uint64, bool) {
	return capture.estimate(coord)
}

func (e *Editor) CaptureAcceptedTiles(ctx context.Context) (AcceptedTileCapture, SaveCapture, error) {
	handle, version, err := e.CaptureSaveSnapshot(ctx)
	if err != nil {
		return AcceptedTileCapture{}, SaveCapture{}, err
	}
	header := e.authoritative
	header.Tiles = nil
	if header.DocumentID != version.DocumentID || header.Revision != version.Revision {
		return AcceptedTileCapture{}, SaveCapture{}, fmt.Errorf("wait for the displayed accepted source")
	}
	result := AcceptedTileCapture{header: header}
	if accepted, ok := handle.(acknowledgedSaveSnapshot); ok {
		result.read, result.estimate = accepted.AcceptedTile, accepted.EstimatedTileBytes
		return result, version, nil
	}
	if capturer, ok := e.executor.(localTileCapturer); ok && !e.sessionOwned {
		capture, err := capturer.CaptureTiles(ctx)
		if err != nil {
			return AcceptedTileCapture{}, SaveCapture{}, err
		}
		if capture.DocumentID() != version.DocumentID || capture.Revision() != version.Revision {
			return AcceptedTileCapture{}, SaveCapture{}, fmt.Errorf("source changed while capturing selected tiles")
		}
		result.read, result.estimate = capture.Tile, capture.EstimatedTileBytes
		return result, version, nil
	}
	// Opaque compatibility executors expose only detached whole snapshots.
	snapshot := handle.Snapshot()
	tiles := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	for _, tile := range snapshot.Tiles {
		tiles[tile.Coord] = tile.State
	}
	result.read = func(coord model.Coord) (model.TileState, bool) {
		state, exists := tiles[coord]
		return model.CloneTileState(state), exists
	}
	result.estimate = func(coord model.Coord) (uint64, bool) {
		state, exists := tiles[coord]
		return engine.EstimateTileStateBytes(state), exists
	}
	return result, version, nil
}

// APHELION EDIT ADDITION END
