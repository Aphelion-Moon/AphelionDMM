package store

import (
	"context"
	"errors"
	"io"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/transaction"
)

const (
	LegacyTransactionVersion = 1
	BulkTransactionVersion   = transaction.Version
)

var (
	ErrSessionExists                 = errors.New("session already exists")
	ErrSessionMissing                = errors.New("session does not exist")
	ErrStoreClosed                   = errors.New("session store is closed")
	ErrCheckpointConflict            = errors.New("export checkpoint conflicts with an existing idempotency key")
	ErrCheckpointMissing             = errors.New("export checkpoint does not exist")
	ErrCheckpointTerminal            = errors.New("export checkpoint already has a terminal result")
	ErrUnsupportedTransactionVersion = errors.New("transaction storage version is unsupported")
	ErrTransactionDowngrade          = errors.New("transaction storage version cannot be downgraded")
	ErrTransactionUpgradeRequired    = errors.New("explicit transaction storage upgrade is required")
	ErrReplayCheckpointChanged       = errors.New("replay checkpoint changed between pages")
	ErrReplayGap                     = errors.New("replay history has a revision gap")
	ErrReplayPageLimit               = errors.New("replay page limit is invalid or exceeds the metadata cap")
	ErrReplayRevisionRange           = errors.New("replay revision range is outside the retained document head")
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

// ReplayEntry contains one bounded replay index row. V1 entries carry their
// legacy inline operation; V2 entries carry header metadata only and leave
// Accepted.Operation.Changes nil. Their body remains available through
// StreamedTransactionStore.OpenTransaction.
type ReplayEntry struct {
	Accepted      model.AcceptedOperation
	MapHash       string
	StorageVersion int
	BodyDigest    string
	BodyBytes     int64
	ChangeCount   int64
}

type ReplayPage struct {
	SnapshotRevision model.Revision
	HeadRevision     model.Revision
	ThroughRevision model.Revision
	Entries          []ReplayEntry
	HasMore          bool
}

const MaxReplayPageEntries = 128

// ReplayPageStore pages the immutable operation index without retaining a DB
// cursor across callers' network writes. Pass nil for the first expected
// snapshot revision, then pin later pages to the value returned by page one.
// A checkpoint change or missing revision is reported instead of silently
// omitting operations.
type ReplayPageStore interface {
	LoadReplayPage(context.Context, model.DocumentID, model.Revision, model.Revision, *model.Revision, int) (ReplayPage, error)
}

// TransactionVersionStore records the negotiated storage format per document.
// Versions only move forward; configuring V2 never rewrites retained V1 rows.
type TransactionVersionStore interface {
	ConfigureTransactions(context.Context, model.DocumentID, int) error
	TransactionVersion(context.Context, model.DocumentID) (int, error)
}

// StreamedTransactionStore exposes the versioned V2 operation body without
// materializing all tile changes in one JSON value. The returned reader must be
// closed; EOF verifies the body digest and its declared record count.
type StreamedTransactionStore interface {
	AppendTransaction(context.Context, *transaction.Body) error
	OpenTransaction(context.Context, model.DocumentID, model.OperationID) (io.ReadCloser, bool, error)
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
