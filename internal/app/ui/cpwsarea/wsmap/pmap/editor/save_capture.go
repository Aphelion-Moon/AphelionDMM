package editor

import (
	"context"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// SaveCapture identifies the attached committed document revision selected for
// background persistence. Snapshot content is returned separately as owned data.
type SaveCapture struct {
	DocumentID model.DocumentID
	Generation uint64
	Revision   model.Revision
}

// SaveSnapshotCapture materializes an owned snapshot when called. The local
// executor implementation pins an immutable revision so a caller can defer
// that O(map size) work to its save worker.
type SaveSnapshotCapture interface {
	Snapshot() model.Snapshot
	EstimatedBytes() uint64
}

type ownedSaveSnapshot struct{ snapshot model.Snapshot }

func (capture ownedSaveSnapshot) Snapshot() model.Snapshot { return capture.snapshot }
func (capture ownedSaveSnapshot) EstimatedBytes() uint64 {
	return engine.EstimateSnapshotBytes(capture.snapshot)
}

type localSnapshotCapturer interface {
	CaptureSnapshot(context.Context) (engine.SnapshotCapture, error)
}

func (e *Editor) CaptureSaveSnapshot(ctx context.Context) (SaveSnapshotCapture, SaveCapture, error) {
	if e.collaborationErr != nil {
		return nil, SaveCapture{}, e.collaborationErr
	}
	if len(e.unresolvedSubmissions) != 0 {
		return nil, SaveCapture{}, fmt.Errorf("map changes are awaiting completion")
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return nil, SaveCapture{}, fmt.Errorf("map changes are awaiting acknowledgement")
	}
	if e.localWork != nil || e.selectionMove != nil || e.pasteBlocksCommittedView() || len(e.pendingChanges) != 0 {
		return nil, SaveCapture{}, fmt.Errorf("map has an uncommitted edit")
	}

	if !e.sessionOwned {
		if capturer, ok := e.executor.(localSnapshotCapturer); ok {
			handle, err := capturer.CaptureSnapshot(ctx)
			if err != nil {
				return nil, SaveCapture{}, fmt.Errorf("capture local map revision for save: %w", err)
			}
			if handle.DocumentID() != e.documentID {
				return nil, SaveCapture{}, fmt.Errorf("save snapshot belongs to a different attached document")
			}
			return handle, SaveCapture{
				DocumentID: handle.DocumentID(),
				Generation: e.attachmentGeneration,
				Revision:   handle.Revision(),
			}, nil
		}
	}

	// Session-owned executors keep the existing snapshot path, which applies
	// the executor's synchronization and projection rules before returning data.
	snapshot, err := e.SaveSnapshot(ctx)
	if err != nil {
		return nil, SaveCapture{}, err
	}
	if snapshot.DocumentID != e.documentID {
		return nil, SaveCapture{}, fmt.Errorf("save snapshot belongs to a different attached document")
	}
	return ownedSaveSnapshot{snapshot: snapshot}, SaveCapture{
		DocumentID: snapshot.DocumentID,
		Generation: e.attachmentGeneration,
		Revision:   snapshot.Revision,
	}, nil
}

func (e *Editor) SaveCaptureAttached(capture SaveCapture) bool {
	return !e.mapViewClosed && capture.DocumentID == e.documentID && capture.Generation == e.attachmentGeneration
}

func (e *Editor) SaveCaptureReady(capture SaveCapture) bool {
	if !e.SaveCaptureAttached(capture) || e.authoritative.Revision != capture.Revision || e.collaborationErr != nil ||
		e.localWork != nil || e.selectionMove != nil || e.pasteBlocksCommittedView() || len(e.pendingChanges) != 0 ||
		len(e.unresolvedSubmissions) != 0 {
		return false
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return false
	}
	return true
}
