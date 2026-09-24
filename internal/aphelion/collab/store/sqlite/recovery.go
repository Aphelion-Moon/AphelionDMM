package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (store *Store) LoadRecovery(ctx context.Context, documentID model.DocumentID) (engine.RecoveryState, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return engine.RecoveryState{}, collabstore.ErrStoreClosed
	}
	transaction, err := store.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return engine.RecoveryState{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	state, err := readRecovery(ctx, transaction, documentID, nil, store.schemaVersion)
	if err != nil {
		return engine.RecoveryState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return engine.RecoveryState{}, err
	}
	return state, nil
}

func readRecovery(ctx context.Context, database queryer, documentID model.DocumentID, encodedBytes *int, schemaVersions ...int) (engine.RecoveryState, error) {
	schemaVersion := 3
	if len(schemaVersions) != 0 {
		schemaVersion = schemaVersions[0]
	}
	var state engine.RecoveryState
	var data []byte
	var revision model.Revision
	err := database.QueryRowContext(ctx, "SELECT snapshot, snapshot_revision, snapshot_hash FROM documents WHERE document_id = ?", documentID).Scan(&data, &revision, &state.SnapshotHash)
	if errors.Is(err, sql.ErrNoRows) {
		return state, collabstore.ErrSessionMissing
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state.Snapshot); err != nil {
		return state, fmt.Errorf("decode snapshot: %w", err)
	}
	if state.Snapshot.DocumentID != documentID || state.Snapshot.Revision != revision {
		return state, fmt.Errorf("stored snapshot identity/revision differs from row")
	}
	if encodedBytes != nil {
		*encodedBytes = len(data) + len(state.SnapshotHash) + 8
	}
	state.Hashes = make(map[model.Revision]string)
	rows, err := database.QueryContext(ctx, "SELECT revision, map_hash FROM revision_hashes WHERE document_id = ? ORDER BY revision", documentID)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&revision, &hash); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Hashes[revision] = hash
		state.HeadRevision, state.HeadHash = revision, hash
		if encodedBytes != nil {
			*encodedBytes += len(hash) + 8
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return state, err
	}
	operationQuery := "SELECT operation_id, revision, accepted, map_hash FROM operations WHERE document_id = ? ORDER BY revision"
	if schemaVersion >= 4 {
		operationQuery = "SELECT operation_id, revision, accepted, map_hash, storage_version, body_digest, body_bytes, change_count FROM operations WHERE document_id = ? ORDER BY revision"
	}
	rows, err = database.QueryContext(ctx, operationQuery, documentID)
	if err != nil {
		return state, err
	}
	type operationRow struct {
		operationID model.OperationID
		revision    model.Revision
		data        []byte
		hash        string
		version     int
		digest      sql.NullString
		bodyBytes   sql.NullInt64
		changeCount sql.NullInt64
		resultIndex int
	}
	var versionedRows []operationRow
	for rows.Next() {
		var operationID model.OperationID
		var hash string
		var storageVersion = collabstore.LegacyTransactionVersion
		var bodyDigest sql.NullString
		var bodyBytes, changeCount sql.NullInt64
		if schemaVersion >= 4 {
			if err := rows.Scan(&operationID, &revision, &data, &hash, &storageVersion, &bodyDigest, &bodyBytes, &changeCount); err != nil {
				_ = rows.Close()
				return state, err
			}
			if storageVersion == collabstore.LegacyTransactionVersion {
				accepted, err := readStoredAccepted(ctx, database, documentID, operationID, revision, hash, data, storageVersion, bodyDigest, bodyBytes, changeCount)
				if err != nil {
					_ = rows.Close()
					return state, err
				}
				if accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || hash != state.Hashes[revision] {
					_ = rows.Close()
					return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
				}
				state.Operations = append(state.Operations, accepted)
				if encodedBytes != nil {
					*encodedBytes += len(data) + len(operationID) + len(hash) + 8
				}
				continue
			}
			resultIndex := len(state.Operations)
			state.Operations = append(state.Operations, model.AcceptedOperation{})
			versionedRows = append(versionedRows, operationRow{operationID, revision, append([]byte(nil), data...), hash, storageVersion, bodyDigest, bodyBytes, changeCount, resultIndex})
			continue
		} else if err := rows.Scan(&operationID, &revision, &data, &hash); err != nil {
			_ = rows.Close()
			return state, err
		}
		accepted, err := decodeAccepted(data)
		if err != nil {
			_ = rows.Close()
			return state, err
		}
		if accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || hash != state.Hashes[revision] {
			_ = rows.Close()
			return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
		}
		state.Operations = append(state.Operations, accepted)
		if encodedBytes != nil {
			*encodedBytes += len(data) + len(operationID) + len(hash) + 8
		}
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return state, rowsErr
	}
	if closeErr != nil {
		return state, closeErr
	}
	for _, row := range versionedRows {
		accepted, err := readStoredAccepted(ctx, database, documentID, row.operationID, row.revision, row.hash, row.data, row.version, row.digest, row.bodyBytes, row.changeCount)
		if err != nil {
			return state, err
		}
		if accepted.DocumentID != documentID || accepted.OperationID != row.operationID || accepted.Revision != row.revision || row.hash != state.Hashes[row.revision] {
			return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
		}
		state.Operations[row.resultIndex] = accepted
		if encodedBytes != nil {
			*encodedBytes += len(row.data) + len(row.operationID) + len(row.hash) + 8
			if row.bodyBytes.Valid && row.bodyBytes.Int64 > 0 {
				if row.bodyBytes.Int64 > int64(recoveryCacheBytes) || *encodedBytes > recoveryCacheBytes-int(row.bodyBytes.Int64) {
					*encodedBytes = recoveryCacheBytes + 1
				} else {
					*encodedBytes += int(row.bodyBytes.Int64)
				}
			}
		}
	}
	return state, nil
}
