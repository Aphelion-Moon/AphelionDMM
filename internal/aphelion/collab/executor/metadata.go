package executor

import (
	"context"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// Metadata reads the current canonical hash without materializing tile payloads.
// Unshared edits invalidate the cache; the owner refreshes it before returning.
func (local *Local) Metadata(ctx context.Context) (engine.Metadata, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return engine.Metadata{}, err
	}
	return local.document.Metadata()
}

// SnapshotWithMetadata keeps the owned snapshot and its verified header in one
// owner turn, including when an inactive resize checkpoint has been edited.
func (local *Local) SnapshotWithMetadata(ctx context.Context) (model.Snapshot, engine.Metadata, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, engine.Metadata{}, err
	}
	metadata, err := local.document.Metadata()
	if err != nil {
		return model.Snapshot{}, engine.Metadata{}, err
	}
	return local.document.Snapshot(), metadata, nil
}
