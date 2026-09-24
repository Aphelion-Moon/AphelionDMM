package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

// UpgradeTransactions copies a validated V3 database into a distinct staged
// V4 file. It never changes the source and refuses to overwrite the destination.
func UpgradeTransactions(ctx context.Context, sourcePath, destinationPath string) (err error) {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("transaction upgrade requires source and destination paths")
	}
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve transaction upgrade source: %w", err)
	}
	destinationAbs, err := filepath.Abs(destinationPath)
	if err != nil {
		return fmt.Errorf("resolve transaction upgrade destination: %w", err)
	}
	if filepath.Clean(sourceAbs) == filepath.Clean(destinationAbs) {
		return fmt.Errorf("transaction upgrade source and destination must be distinct")
	}
	if _, statErr := os.Lstat(destinationAbs); statErr == nil {
		return fmt.Errorf("transaction upgrade destination already exists")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("check transaction upgrade destination: %w", statErr)
	}
	sourceURIPath := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23", "&", "%26").Replace(filepath.ToSlash(sourceAbs))
	sourceURL := "file:" + sourceURIPath + "?mode=ro&_pragma=foreign_keys(1)"
	source, err := sql.Open("sqlite", sourceURL)
	if err != nil {
		return fmt.Errorf("open transaction upgrade source read-only: %w", err)
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	source.SetMaxOpenConns(1)
	if err := source.PingContext(ctx); err != nil {
		return fmt.Errorf("read transaction upgrade source: %w", err)
	}
	var version int
	if err := source.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read transaction upgrade source version: %w", err)
	}
	if version != 3 {
		return fmt.Errorf("transaction upgrade source schema is %d; require V3", version)
	}
	documentIDs, sourceStates, err := validateUpgradeSource(ctx, source)
	if err != nil {
		return err
	}
	stagingFile, err := os.CreateTemp(filepath.Dir(destinationAbs), filepath.Base(destinationAbs)+".upgrade-*.db")
	if err != nil {
		return fmt.Errorf("create staged transaction upgrade file: %w", err)
	}
	stagingPath := stagingFile.Name()
	if err := stagingFile.Close(); err != nil {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("close staged transaction upgrade file: %w", err)
	}
	if err := os.Remove(stagingPath); err != nil {
		return fmt.Errorf("prepare staged transaction upgrade file: %w", err)
	}
	defer func() {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(stagingPath + suffix)
		}
	}()
	target, err := Open(stagingPath)
	if err != nil {
		return fmt.Errorf("create staged V4 transaction database: %w", err)
	}
	if err := copyV3Rows(ctx, source, target.database); err != nil {
		_ = target.Close()
		return err
	}
	if err := closeStagedStore(ctx, target); err != nil {
		return fmt.Errorf("close staged V4 database: %w", err)
	}
	probe, err := Open(stagingPath)
	if err != nil {
		return fmt.Errorf("reopen staged V4 database: %w", err)
	}
	for _, documentID := range documentIDs {
		state, loadErr := probe.LoadRecovery(ctx, documentID)
		if loadErr != nil {
			_ = probe.Close()
			return fmt.Errorf("validate staged document %q: %w", documentID, loadErr)
		}
		if _, restoreErr := state.Restore(); restoreErr != nil {
			_ = probe.Close()
			return fmt.Errorf("validate staged document %q history: %w", documentID, restoreErr)
		}
		if !reflect.DeepEqual(state, sourceStates[documentID]) {
			_ = probe.Close()
			return fmt.Errorf("staged document %q differs from validated V3 source", documentID)
		}
		if version, versionErr := probe.TransactionVersion(ctx, documentID); versionErr != nil || version != collabstore.LegacyTransactionVersion {
			_ = probe.Close()
			return fmt.Errorf("staged document %q transaction version = %d, %v; want V1", documentID, version, versionErr)
		}
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close staged V4 verification store: %w", err)
	}
	if _, err := os.Stat(destinationAbs); err == nil {
		return fmt.Errorf("transaction upgrade destination appeared during staging")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("recheck transaction upgrade destination: %w", err)
	}
	if err := os.Rename(stagingPath, destinationAbs); err != nil {
		return fmt.Errorf("publish staged V4 transaction database: %w", err)
	}
	return nil
}

func closeStagedStore(ctx context.Context, staged *Store) error {
	var busy, logFrames, checkpointed int
	checkpointErr := staged.database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	if checkpointErr == nil && busy != 0 {
		checkpointErr = fmt.Errorf("SQLite WAL checkpoint left %d busy frame(s)", busy)
	}
	return errors.Join(checkpointErr, staged.Close())
}

func validateUpgradeSource(ctx context.Context, database *sql.DB) ([]model.DocumentID, map[model.DocumentID]engine.RecoveryState, error) {
	rows, err := database.QueryContext(ctx, "SELECT document_id FROM documents ORDER BY document_id")
	if err != nil {
		return nil, nil, fmt.Errorf("list transaction upgrade source documents: %w", err)
	}
	var documentIDs []model.DocumentID
	for rows.Next() {
		var documentID model.DocumentID
		if err := rows.Scan(&documentID); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		documentIDs = append(documentIDs, documentID)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if err := errors.Join(rowsErr, closeErr); err != nil {
		return nil, nil, fmt.Errorf("read transaction upgrade source documents: %w", err)
	}
	states := make(map[model.DocumentID]engine.RecoveryState, len(documentIDs))
	for _, documentID := range documentIDs {
		state, err := readRecovery(ctx, database, documentID, nil, 3)
		if err != nil {
			return nil, nil, fmt.Errorf("validate V3 source document %q: %w", documentID, err)
		}
		if _, err := state.Restore(); err != nil {
			return nil, nil, fmt.Errorf("validate V3 source document %q history: %w", documentID, err)
		}
		states[documentID] = state
	}
	return documentIDs, states, nil
}

func copyV3Rows(ctx context.Context, source *sql.DB, target *sql.DB) error {
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin staged SQLite V4 copy: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	documents, err := source.QueryContext(ctx, "SELECT document_id, snapshot, snapshot_revision, snapshot_hash FROM documents ORDER BY document_id")
	if err != nil {
		return err
	}
	for documents.Next() {
		var documentID string
		var snapshot []byte
		var revision int64
		var hash string
		if err := documents.Scan(&documentID, &snapshot, &revision, &hash); err != nil {
			_ = documents.Close()
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO documents(document_id, snapshot, snapshot_revision, snapshot_hash, transaction_version) VALUES(?, ?, ?, ?, ?)", documentID, snapshot, revision, hash, collabstore.LegacyTransactionVersion); err != nil {
			_ = documents.Close()
			return fmt.Errorf("copy V3 document: %w", err)
		}
	}
	if err := errors.Join(documents.Err(), documents.Close()); err != nil {
		return fmt.Errorf("read V3 documents: %w", err)
	}
	operations, err := source.QueryContext(ctx, "SELECT document_id, operation_id, revision, accepted, map_hash FROM operations ORDER BY document_id, revision")
	if err != nil {
		return err
	}
	for operations.Next() {
		var documentID, operationID, hash string
		var revision int64
		var accepted []byte
		if err := operations.Scan(&documentID, &operationID, &revision, &accepted, &hash); err != nil {
			_ = operations.Close()
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO operations(document_id, operation_id, revision, accepted, map_hash, storage_version) VALUES(?, ?, ?, ?, ?, ?)", documentID, operationID, revision, accepted, hash, collabstore.LegacyTransactionVersion); err != nil {
			_ = operations.Close()
			return fmt.Errorf("copy V3 operation: %w", err)
		}
	}
	if err := errors.Join(operations.Err(), operations.Close()); err != nil {
		return fmt.Errorf("read V3 operations: %w", err)
	}
	hashes, err := source.QueryContext(ctx, "SELECT document_id, revision, map_hash FROM revision_hashes ORDER BY document_id, revision")
	if err != nil {
		return err
	}
	for hashes.Next() {
		var documentID, hash string
		var revision int64
		if err := hashes.Scan(&documentID, &revision, &hash); err != nil {
			_ = hashes.Close()
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO revision_hashes(document_id, revision, map_hash) VALUES(?, ?, ?)", documentID, revision, hash); err != nil {
			_ = hashes.Close()
			return fmt.Errorf("copy V3 revision hash: %w", err)
		}
	}
	if err := errors.Join(hashes.Err(), hashes.Close()); err != nil {
		return fmt.Errorf("read V3 revision hashes: %w", err)
	}
	checkpoints, err := source.QueryContext(ctx, "SELECT checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status, artifact_hash, verifier, verifier_version, diagnostic_code, completed_at FROM export_checkpoints ORDER BY document_id, checkpoint_id")
	if err != nil {
		return err
	}
	for checkpoints.Next() {
		var checkpointID, documentID, sessionID, idempotencyKey, mapHash, requestedBy, createdAt, status string
		var revision int64
		var artifactHash, verifier, verifierVersion, diagnosticCode, completedAt sql.NullString
		if err := checkpoints.Scan(&checkpointID, &documentID, &sessionID, &idempotencyKey, &revision, &mapHash, &requestedBy, &createdAt, &status, &artifactHash, &verifier, &verifierVersion, &diagnosticCode, &completedAt); err != nil {
			_ = checkpoints.Close()
			return err
		}
		values := make([]any, 14)
		values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8] = checkpointID, documentID, sessionID, idempotencyKey, revision, mapHash, requestedBy, createdAt, status
		for index, nullable := range []sql.NullString{artifactHash, verifier, verifierVersion, diagnosticCode, completedAt} {
			if nullable.Valid {
				values[9+index] = nullable.String
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO export_checkpoints(checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status, artifact_hash, verifier, verifier_version, diagnostic_code, completed_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", values...); err != nil {
			_ = checkpoints.Close()
			return fmt.Errorf("copy V3 export checkpoint: %w", err)
		}
	}
	if err := errors.Join(checkpoints.Err(), checkpoints.Close()); err != nil {
		return fmt.Errorf("read V3 export checkpoints: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit staged SQLite V4 copy: %w", err)
	}
	return nil
}
