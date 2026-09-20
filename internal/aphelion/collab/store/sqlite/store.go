package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"

	_ "modernc.org/sqlite"
)

const minimumSQLiteVersion = "3.51.3"

type Store struct {
	mutex    sync.RWMutex
	database *sql.DB
	version  string
	closed   bool
	recovery recoveryCache
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("open SQLite store: path is empty")
	}
	parameters := url.Values{}
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "foreign_keys(1)")
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "synchronous(FULL)")
	dsn := "file:" + filepath.ToSlash(path) + "?" + parameters.Encode()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx := context.Background()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}
	var version string
	if err := database.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("query SQLite version: %w", err)
	}
	if compareVersions(version, minimumSQLiteVersion) < 0 {
		_ = database.Close()
		return nil, fmt.Errorf("SQLite version is %s, require at least %s", version, minimumSQLiteVersion)
	}
	if err := verifyPragmas(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := migrate(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{database: database, version: version}, nil
}

func (store *Store) Version() string {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	return store.version
}

func (store *Store) Create(ctx context.Context, snapshot model.Snapshot) error {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := engine.NewDocument(snapshot); err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var present int
	err = transaction.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", snapshot.DocumentID).Scan(&present)
	if err == nil {
		return collabstore.ErrSessionExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing document: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO documents(document_id, snapshot, snapshot_revision, snapshot_hash) VALUES(?, ?, ?, ?)", snapshot.DocumentID, encoded, snapshot.Revision, mapHash); err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)", snapshot.DocumentID, snapshot.Revision, mapHash); err != nil {
		return fmt.Errorf("insert initial revision hash: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit create: %w", err)
	}
	return nil
}

func (store *Store) Append(ctx context.Context, accepted model.AcceptedOperation) error {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin append: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var priorData []byte
	err = transaction.QueryRowContext(ctx, "SELECT accepted FROM operations WHERE document_id = ? AND operation_id = ?", accepted.DocumentID, accepted.OperationID).Scan(&priorData)
	if err == nil {
		prior, decodeErr := decodeAccepted(priorData)
		if decodeErr != nil {
			return decodeErr
		}
		if !reflect.DeepEqual(prior, accepted) {
			return fmt.Errorf("operation %q conflicts with stored revision %d", accepted.OperationID, prior.Revision)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lookup duplicate operation: %w", err)
	}
	var encodedBytes int
	state, err := readRecovery(ctx, transaction, accepted.DocumentID, &encodedBytes)
	if err != nil {
		return err
	}
	document, err := store.recovery.restore(state)
	if err != nil {
		return err
	}
	verified, err := document.Apply(accepted.Operation, accepted.AcceptedAt)
	if err != nil {
		return fmt.Errorf("verify accepted operation: %w", err)
	}
	if !reflect.DeepEqual(verified, accepted) {
		return fmt.Errorf("accepted operation does not match verified operation")
	}
	mapHash, err := document.Snapshot().Hash()
	if err != nil {
		return fmt.Errorf("hash accepted revision: %w", err)
	}
	encoded, err := json.Marshal(accepted)
	if err != nil {
		return fmt.Errorf("encode accepted operation: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO operations(document_id, operation_id, revision, accepted, map_hash) VALUES(?, ?, ?, ?, ?)", accepted.DocumentID, accepted.OperationID, accepted.Revision, encoded, mapHash); err != nil {
		return fmt.Errorf("insert operation: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)", accepted.DocumentID, accepted.Revision, mapHash); err != nil {
		return fmt.Errorf("insert revision hash: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit append: %w", err)
	}
	state.Operations = append(state.Operations, model.CloneAcceptedOperation(accepted))
	state.Hashes[accepted.Revision] = mapHash
	state.HeadRevision, state.HeadHash = accepted.Revision, mapHash
	encodedBytes += len(encoded) + len(accepted.OperationID) + 2*len(mapHash) + 16
	store.recovery.retain(state, document, encodedBytes)
	return nil
}

func (store *Store) Load(ctx context.Context, documentID model.DocumentID) (model.Snapshot, []model.AcceptedOperation, error) {
	state, err := store.LoadRecovery(ctx, documentID)
	if err != nil {
		return model.Snapshot{}, nil, err
	}
	if _, err := state.Restore(); err != nil {
		return model.Snapshot{}, nil, err
	}
	replay := make([]model.AcceptedOperation, 0)
	for _, accepted := range state.Operations {
		if accepted.Revision > state.Snapshot.Revision {
			replay = append(replay, accepted)
		}
	}
	return model.CloneSnapshot(state.Snapshot), replay, nil
}

func (store *Store) SaveSnapshot(ctx context.Context, snapshot model.Snapshot) error {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	state, err := loadRecovery(ctx, transaction, snapshot.DocumentID)
	if err != nil {
		return err
	}
	retained, err := state.RestoreAt(snapshot.Revision)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(retained.Snapshot(), model.CloneSnapshot(snapshot)) {
		return fmt.Errorf("snapshot does not match retained revision")
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	result, err := transaction.ExecContext(ctx, "UPDATE documents SET snapshot = ?, snapshot_revision = ?, snapshot_hash = ? WHERE document_id = ?", encoded, snapshot.Revision, mapHash, snapshot.DocumentID)
	if err != nil {
		return fmt.Errorf("update snapshot: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read snapshot update result: %w", err)
	}
	if rows != 1 {
		return collabstore.ErrSessionMissing
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}
	return nil
}

func (store *Store) RevisionHash(ctx context.Context, documentID model.DocumentID, revision model.Revision) (string, bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return "", false, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	var hash string
	err := store.database.QueryRowContext(ctx, "SELECT map_hash FROM revision_hashes WHERE document_id = ? AND revision = ?", documentID, revision).Scan(&hash)
	if err == nil {
		return hash, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("query revision hash: %w", err)
	}
	var present int
	err = store.database.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", documentID).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, collabstore.ErrSessionMissing
	}
	if err != nil {
		return "", false, fmt.Errorf("query document: %w", err)
	}
	return "", false, nil
}

func (store *Store) LookupOperation(ctx context.Context, documentID model.DocumentID, operationID model.OperationID) (model.AcceptedOperation, bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.AcceptedOperation{}, false, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, false, err
	}
	var data []byte
	err := store.database.QueryRowContext(ctx, "SELECT accepted FROM operations WHERE document_id = ? AND operation_id = ?", documentID, operationID).Scan(&data)
	if err == nil {
		accepted, decodeErr := decodeAccepted(data)
		return accepted, true, decodeErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.AcceptedOperation{}, false, fmt.Errorf("lookup operation: %w", err)
	}
	var present int
	err = store.database.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", documentID).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AcceptedOperation{}, false, collabstore.ErrSessionMissing
	}
	if err != nil {
		return model.AcceptedOperation{}, false, fmt.Errorf("query document: %w", err)
	}
	return model.AcceptedOperation{}, false, nil
}

func (store *Store) CreateExportCheckpoint(ctx context.Context, checkpoint model.ExportCheckpoint) (model.ExportCheckpoint, bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.ExportCheckpoint{}, false, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if err := checkpoint.Validate(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if checkpoint.Status != model.ExportCheckpointPending {
		return model.ExportCheckpoint{}, false, fmt.Errorf("new export checkpoint must be pending")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("begin export checkpoint: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	prior, found, err := lookupCheckpointByKey(ctx, transaction, checkpoint.DocumentID, checkpoint.IdempotencyKey)
	if err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if found {
		if collabstore.SameCheckpointRequest(prior, checkpoint) {
			return prior, false, nil
		}
		return model.ExportCheckpoint{}, false, collabstore.ErrCheckpointConflict
	}
	prior, found, err = lookupCheckpoint(ctx, transaction, checkpoint.DocumentID, checkpoint.CheckpointID)
	if err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if found {
		if collabstore.SameCheckpointRequest(prior, checkpoint) {
			return prior, false, nil
		}
		return model.ExportCheckpoint{}, false, collabstore.ErrCheckpointConflict
	}
	var retainedHash string
	if err := transaction.QueryRowContext(ctx, "SELECT map_hash FROM revision_hashes WHERE document_id = ? AND revision = ?", checkpoint.DocumentID, checkpoint.Revision).Scan(&retainedHash); errors.Is(err, sql.ErrNoRows) {
		return model.ExportCheckpoint{}, false, fmt.Errorf("checkpoint revision/hash is not retained")
	} else if err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("query checkpoint revision hash: %w", err)
	}
	if retainedHash != checkpoint.MapHash {
		return model.ExportCheckpoint{}, false, fmt.Errorf("checkpoint revision/hash is not retained")
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO export_checkpoints(checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, checkpoint.CheckpointID, checkpoint.DocumentID, checkpoint.SessionID, checkpoint.IdempotencyKey, checkpoint.Revision, checkpoint.MapHash, checkpoint.RequestedBy, checkpoint.CreatedAt.UTC().Format(time.RFC3339Nano), checkpoint.Status); err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("insert export checkpoint: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("commit export checkpoint: %w", err)
	}
	return model.CloneExportCheckpoint(checkpoint), true, nil
}

func (store *Store) LookupExportCheckpoint(ctx context.Context, documentID model.DocumentID, checkpointID model.CheckpointID) (model.ExportCheckpoint, bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.ExportCheckpoint{}, false, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	checkpoint, found, err := lookupCheckpoint(ctx, store.database, documentID, checkpointID)
	if err != nil || found {
		return checkpoint, found, err
	}
	var present int
	if err := store.database.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", documentID).Scan(&present); errors.Is(err, sql.ErrNoRows) {
		return model.ExportCheckpoint{}, false, collabstore.ErrSessionMissing
	} else if err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("query checkpoint document: %w", err)
	}
	return model.ExportCheckpoint{}, false, nil
}

func (store *Store) CompleteExportCheckpoint(ctx context.Context, documentID model.DocumentID, checkpointID model.CheckpointID, completion model.ExportCheckpointCompletion) (model.ExportCheckpoint, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.ExportCheckpoint{}, collabstore.ErrStoreClosed
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return model.ExportCheckpoint{}, fmt.Errorf("begin checkpoint completion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	checkpoint, found, err := lookupCheckpoint(ctx, transaction, documentID, checkpointID)
	if err != nil {
		return model.ExportCheckpoint{}, err
	}
	if !found {
		return model.ExportCheckpoint{}, collabstore.ErrCheckpointMissing
	}
	if checkpoint.Status != model.ExportCheckpointPending {
		if collabstore.CheckpointMatchesCompletion(checkpoint, completion) {
			return checkpoint, nil
		}
		return model.ExportCheckpoint{}, collabstore.ErrCheckpointTerminal
	}
	completed, err := checkpoint.Complete(completion)
	if err != nil {
		return model.ExportCheckpoint{}, err
	}
	var artifactHash, diagnosticCode any
	if completed.ArtifactHash != "" {
		artifactHash = completed.ArtifactHash
	}
	if completed.DiagnosticCode != "" {
		diagnosticCode = completed.DiagnosticCode
	}
	result, err := transaction.ExecContext(ctx, `UPDATE export_checkpoints SET status = ?, artifact_hash = ?, verifier = ?, verifier_version = ?, diagnostic_code = ?, completed_at = ? WHERE document_id = ? AND checkpoint_id = ? AND status = 'pending'`, completed.Status, artifactHash, completed.Verifier, completed.VerifierVersion, diagnosticCode, completed.CompletedAt.UTC().Format(time.RFC3339Nano), documentID, checkpointID)
	if err != nil {
		return model.ExportCheckpoint{}, fmt.Errorf("update export checkpoint: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return model.ExportCheckpoint{}, fmt.Errorf("read export checkpoint update result: %w", err)
	}
	if rows != 1 {
		return model.ExportCheckpoint{}, fmt.Errorf("complete export checkpoint changed %d rows", rows)
	}
	if err := transaction.Commit(); err != nil {
		return model.ExportCheckpoint{}, fmt.Errorf("commit checkpoint completion: %w", err)
	}
	return completed, nil
}

const checkpointColumns = `checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status, artifact_hash, verifier, verifier_version, diagnostic_code, completed_at`

type checkpointQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func lookupCheckpoint(ctx context.Context, database checkpointQueryer, documentID model.DocumentID, checkpointID model.CheckpointID) (model.ExportCheckpoint, bool, error) {
	return scanCheckpoint(database.QueryRowContext(ctx, "SELECT "+checkpointColumns+" FROM export_checkpoints WHERE document_id = ? AND checkpoint_id = ?", documentID, checkpointID))
}

func lookupCheckpointByKey(ctx context.Context, database checkpointQueryer, documentID model.DocumentID, idempotencyKey string) (model.ExportCheckpoint, bool, error) {
	return scanCheckpoint(database.QueryRowContext(ctx, "SELECT "+checkpointColumns+" FROM export_checkpoints WHERE document_id = ? AND idempotency_key = ?", documentID, idempotencyKey))
}

func scanCheckpoint(row *sql.Row) (model.ExportCheckpoint, bool, error) {
	var checkpoint model.ExportCheckpoint
	var createdAt string
	var artifactHash, verifier, verifierVersion, diagnosticCode, completedAt sql.NullString
	if err := row.Scan(&checkpoint.CheckpointID, &checkpoint.DocumentID, &checkpoint.SessionID, &checkpoint.IdempotencyKey, &checkpoint.Revision, &checkpoint.MapHash, &checkpoint.RequestedBy, &createdAt, &checkpoint.Status, &artifactHash, &verifier, &verifierVersion, &diagnosticCode, &completedAt); errors.Is(err, sql.ErrNoRows) {
		return model.ExportCheckpoint{}, false, nil
	} else if err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("scan export checkpoint: %w", err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("parse export checkpoint creation time: %w", err)
	}
	checkpoint.CreatedAt = parsedCreatedAt
	if artifactHash.Valid {
		checkpoint.ArtifactHash = artifactHash.String
	}
	if verifier.Valid {
		checkpoint.Verifier = verifier.String
	}
	if verifierVersion.Valid {
		checkpoint.VerifierVersion = verifierVersion.String
	}
	if diagnosticCode.Valid {
		checkpoint.DiagnosticCode = diagnosticCode.String
	}
	if completedAt.Valid {
		parsedCompletedAt, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return model.ExportCheckpoint{}, false, fmt.Errorf("parse export checkpoint completion time: %w", err)
		}
		checkpoint.CompletedAt = &parsedCompletedAt
	}
	if err := checkpoint.Validate(); err != nil {
		return model.ExportCheckpoint{}, false, fmt.Errorf("validate stored export checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func (store *Store) Close() error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	store.recovery.clear()
	return store.database.Close()
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func decodeAccepted(data []byte) (model.AcceptedOperation, error) {
	var accepted model.AcceptedOperation
	if err := json.Unmarshal(data, &accepted); err != nil {
		return model.AcceptedOperation{}, fmt.Errorf("decode accepted operation: %w", err)
	}
	return model.CloneAcceptedOperation(accepted), nil
}

func verifyPragmas(ctx context.Context, database *sql.DB) error {
	expectations := []struct {
		name string
		want string
	}{
		{name: "foreign_keys", want: "1"},
		{name: "journal_mode", want: "wal"},
		{name: "busy_timeout", want: "5000"},
		{name: "synchronous", want: "2"},
	}
	for _, expectation := range expectations {
		var actual string
		if err := database.QueryRowContext(ctx, "PRAGMA "+expectation.name).Scan(&actual); err != nil {
			return fmt.Errorf("query PRAGMA %s: %w", expectation.name, err)
		}
		if !strings.EqualFold(actual, expectation.want) {
			return fmt.Errorf("PRAGMA %s is %q, want %q", expectation.name, actual, expectation.want)
		}
	}
	return nil
}

func compareVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		var leftValue, rightValue int
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}
