// APHELION EDIT ADDITION START - PINNED STAMP CAPTURE
package editor

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/util"
)

func (e *Editor) StampCaptureReason() string {
	if e.mapViewClosed {
		return "Source map is closed."
	}
	if _, ready := e.MapViewVersion(); !ready || e.collaborationErr != nil {
		return "Finish or resolve the current map edit before capturing."
	}
	if len(e.unresolvedSubmissions) != 0 {
		return "Wait for pending map changes to complete."
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return "Wait for map acknowledgement before capturing."
	}
	if e.executor == nil {
		return "Map authority is unavailable."
	}
	return ""
}

// PrepareStampCapture pins the source on the UI owner. The returned function
// can read that revision off-thread without copying or hashing the whole map.
func (e *Editor) PrepareStampCapture(name string, selection editing.Selection) (func(context.Context) (*stamps.Stamp, error), error) {
	if reason := e.StampCaptureReason(); reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	if selection.Len() == 0 {
		return nil, fmt.Errorf("select tiles on the source map first")
	}
	filter := e.app.PathsFilter().Copy()
	environment := e.authoritative.EnvironmentHash
	budget := e.editWorkBudget()
	if !e.sessionOwned {
		capturer, ok := e.executor.(localTileCapturer)
		if !ok {
			return nil, fmt.Errorf("map executor cannot pin a selection capture")
		}
		capture, err := capturer.CaptureTiles(context.Background())
		if err != nil {
			return nil, err
		}
		if capture.DocumentID() != e.documentID || capture.Revision() != e.authoritative.Revision {
			return nil, fmt.Errorf("map authority changed before capture")
		}
		return func(ctx context.Context) (*stamps.Stamp, error) {
			return stamps.CaptureModel(ctx, name, environment, selection, &filter, capture.Tile, budget)
		}, nil
	}
	capturer, ok := e.executor.(projectionCapturer)
	if !ok {
		return nil, fmt.Errorf("map executor cannot pin a selection capture")
	}
	capture, err := capturer.CaptureProjection(context.Background())
	if err != nil {
		return nil, err
	}
	if capture.HasPending() || capture.DocumentID() != e.documentID || capture.BaseRevision() != e.authoritative.Revision {
		return nil, fmt.Errorf("wait for acknowledged source projection")
	}
	return func(ctx context.Context) (*stamps.Stamp, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contains := func(c model.Coord) bool { return selection.Contains(util.Point{X: c.X, Y: c.Y, Z: c.Z}) }
		reservation, err := budget.Reserve(capture.EstimatedVisibleTilesBytes(contains))
		if err != nil {
			return nil, err
		}
		defer reservation.Release()
		// Incoming publications mutate the editor's authority index. The stamp
		// must continue reading the revision selected when capture was requested.
		tiles, err := capture.VisibleTiles(contains)
		if err != nil {
			return nil, err
		}
		read := func(c model.Coord) (model.TileState, bool) { state, ok := tiles[c]; return state, ok }
		return stamps.CaptureModel(ctx, name, environment, selection, &filter, read, budget)
	}, nil
}

// APHELION EDIT ADDITION END
