package store

import (
	"context"
	"errors"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

var (
	ErrSessionExists      = errors.New("session already exists")
	ErrSessionMissing     = errors.New("session does not exist")
	ErrStoreClosed        = errors.New("session store is closed")
	ErrCheckpointConflict = errors.New("export checkpoint conflicts with an existing idempotency key")
	ErrCheckpointMissing  = errors.New("export checkpoint does not exist")
	ErrCheckpointTerminal = errors.New("export checkpoint already has a terminal result")
)

type SessionStore interface {
	Create(context.Context, model.Snapshot) error
	Append(context.Context, model.AcceptedOperation) error
	Load(context.Context, model.DocumentID) (model.Snapshot, []model.AcceptedOperation, error)
	LoadRecovery(context.Context, model.DocumentID) (engine.RecoveryState, error)
	SaveSnapshot(context.Context, model.Snapshot) error
	RevisionHash(context.Context, model.DocumentID, model.Revision) (string, bool, error)
	LookupOperation(context.Context, model.DocumentID, model.OperationID) (model.AcceptedOperation, bool, error)
	CreateExportCheckpoint(context.Context, model.ExportCheckpoint) (model.ExportCheckpoint, bool, error)
	LookupExportCheckpoint(context.Context, model.DocumentID, model.CheckpointID) (model.ExportCheckpoint, bool, error)
	CompleteExportCheckpoint(context.Context, model.DocumentID, model.CheckpointID, model.ExportCheckpointCompletion) (model.ExportCheckpoint, error)
	Close() error
}

// ReplayStore optionally loads a validated reconnect snapshot, its replay suffix,
// and revision hashes in one consistent read. Returned data belongs to the caller.
// Stores without this interface use Load and individual RevisionHash reads.
type ReplayStore interface {
	LoadReplay(context.Context, model.DocumentID) (model.Snapshot, []model.AcceptedOperation, map[model.Revision]string, error)
}

func SameCheckpointRequest(left, right model.ExportCheckpoint) bool {
	return left.IdempotencyKey == right.IdempotencyKey &&
		left.DocumentID == right.DocumentID &&
		left.SessionID == right.SessionID &&
		left.Revision == right.Revision &&
		left.MapHash == right.MapHash &&
		left.RequestedBy == right.RequestedBy
}

func CheckpointMatchesCompletion(checkpoint model.ExportCheckpoint, completion model.ExportCheckpointCompletion) bool {
	return checkpoint.Status == completion.Status &&
		checkpoint.ArtifactHash == completion.ArtifactHash &&
		checkpoint.Verifier == completion.Verifier &&
		checkpoint.VerifierVersion == completion.VerifierVersion &&
		checkpoint.DiagnosticCode == completion.DiagnosticCode &&
		checkpoint.CompletedAt != nil && checkpoint.CompletedAt.Equal(completion.CompletedAt)
}
