package executor

import (
	"context"
	"sdmm/internal/aphelion/collab/engine"
)

// CaptureSnapshot briefly takes authority ownership; the caller materializes
// the immutable captured revision after releasing this executor's mutation lock.
func (local *Local) CaptureSnapshot(ctx context.Context) (engine.SnapshotCapture, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return engine.SnapshotCapture{}, err
	}
	return local.document.CaptureSnapshot(), nil
}

func (local *Local) CaptureTiles(ctx context.Context) (engine.TileCapture, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return engine.TileCapture{}, err
	}
	return local.document.CaptureTiles(), nil
}
