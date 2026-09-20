// APHELION EDIT ADDITION START - LOCAL EDIT RECOVERY
package editor

import (
	"context"
	"fmt"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing/recovery"
)

// LocalRecovery binds an immutable record to the display and attachment that
// were inspected. All editor methods here run on the UI thread.
type LocalRecovery struct {
	*recovery.Record
	owner      *Editor
	attachment uint64
	view       uint64
}

func (e *Editor) HasLocalRecovery() bool {
	return !e.mapViewClosed && (e.collaborationErr != nil || len(e.pendingChanges) != 0)
}

func (e *Editor) InspectLocalRecovery() (*LocalRecovery, error) {
	if !e.HasLocalRecovery() {
		return nil, fmt.Errorf("no retained local edit is available")
	}
	record, err := recovery.Capture(e.dmm, e.authoritative, e.pendingChanges)
	if err != nil {
		return nil, err
	}
	return &LocalRecovery{Record: record, owner: e, attachment: e.attachmentGeneration, view: e.mapViewGeneration}, nil
}

func (e *Editor) DiscardLocalRecovery(draft *LocalRecovery) error {
	if draft == nil || draft.owner != e || draft.attachment != e.attachmentGeneration || draft.view != e.mapViewGeneration || !e.HasLocalRecovery() {
		return fmt.Errorf("the map changed; inspect the retained edit again before discarding")
	}
	if e.executor == nil || !e.history.Valid() {
		return fmt.Errorf("the map authority or history is unavailable")
	}
	if e.selectionMove != nil || len(e.unresolvedSubmissions) != 0 {
		return fmt.Errorf("finish or cancel the preview and wait for submitted operations before discarding")
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return fmt.Errorf("wait for operation acknowledgement before discarding")
	}
	// Inherited display writes do not all advance MapViewVersion. Compare the
	// full raw record as well so a late property change cannot be discarded by
	// confirmation of an earlier inspection.
	current, err := recovery.Capture(e.dmm, e.authoritative, e.pendingChanges)
	if err != nil {
		return err
	}
	if !draft.Same(current) {
		return fmt.Errorf("retained contents changed; inspect them again before discarding")
	}
	snapshot, err := e.executor.Snapshot(context.Background())
	if err != nil {
		return fmt.Errorf("read recovery authority: %w", err)
	}
	environmentHash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	if err != nil {
		return err
	}
	if snapshot.DocumentID != e.documentID || snapshot.EnvironmentHash != environmentHash {
		return fmt.Errorf("recovery authority does not match this map and environment")
	}
	// Apply validates and stages every tile before replacing display state. No
	// journal, fault, callback generation or history is cleared on failure.
	if err := mapadapter.ApplyWithEnvironment(e.dmm, snapshot, e.app.LoadedEnvironment()); err != nil {
		return err
	}
	e.pendingChanges = make(map[model.Coord]model.TileState)
	e.collaborationErr = nil
	e.setAuthoritative(snapshot)
	e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, snapshot)
	return nil
}

// APHELION EDIT ADDITION END
