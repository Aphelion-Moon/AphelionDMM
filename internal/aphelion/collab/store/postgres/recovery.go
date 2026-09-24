package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return engine.RecoveryState{}, err
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	state, err := loadRecovery(ctx, transaction, documentID, false, store.schemaVersion)
	if err != nil {
		return engine.RecoveryState{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return engine.RecoveryState{}, err
	}
	return state, nil
}

func loadRecovery(ctx context.Context, database queryer, documentID model.DocumentID, lock bool, schemaVersions ...int) (engine.RecoveryState, error) {
	schemaVersion := 3
	if len(schemaVersions) != 0 {
		schemaVersion = schemaVersions[0]
	}
	var state engine.RecoveryState
	var data []byte
	var revision model.Revision
	query := "SELECT snapshot, snapshot_revision, snapshot_hash, current_revision, current_hash FROM collaboration_documents WHERE document_id = $1"
	if lock {
		query += " FOR UPDATE"
	}
	err := database.QueryRow(ctx, query, documentID).Scan(&data, &revision, &state.SnapshotHash, &state.HeadRevision, &state.HeadHash)
	if errors.Is(err, pgx.ErrNoRows) {
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
	state.Hashes = make(map[model.Revision]string)
	rows, err := database.Query(ctx, "SELECT revision, map_hash FROM collaboration_revision_hashes WHERE document_id = $1 ORDER BY revision", documentID)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&revision, &hash); err != nil {
			rows.Close()
			return state, err
		}
		state.Hashes[revision] = hash
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	operationQuery := "SELECT operation_id, revision, accepted, map_hash FROM collaboration_operations WHERE document_id = $1 ORDER BY revision"
	if schemaVersion >= 4 {
		operationQuery = "SELECT operation_id, revision, accepted, map_hash, storage_version, body_digest, body_bytes, change_count FROM collaboration_operations WHERE document_id = $1 ORDER BY revision"
	}
	rows, err = database.Query(ctx, operationQuery, documentID)
	if err != nil {
		return state, err
	}
	type operationRow struct {
		operationID model.OperationID
		revision    model.Revision
		data        []byte
		hash        string
		version     int
		digest      *string
		bodyBytes   *int64
		changeCount *int64
		resultIndex int
	}
	var versionedRows []operationRow
	for rows.Next() {
		var operationID model.OperationID
		var hash string
		var storageVersion = collabstore.LegacyTransactionVersion
		var bodyDigest *string
		var bodyBytes, changeCount *int64
		if schemaVersion >= 4 {
			if err := rows.Scan(&operationID, &revision, &data, &hash, &storageVersion, &bodyDigest, &bodyBytes, &changeCount); err != nil {
				rows.Close()
				return state, err
			}
			hash = strings.TrimSpace(hash)
			if storageVersion == collabstore.LegacyTransactionVersion {
				accepted, err := readStoredAccepted(ctx, database, documentID, operationID, revision, hash, data, storageVersion, bodyDigest, bodyBytes, changeCount)
				if err != nil {
					rows.Close()
					return state, err
				}
				if accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || hash != state.Hashes[revision] {
					rows.Close()
					return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
				}
				state.Operations = append(state.Operations, accepted)
				continue
			}
			resultIndex := len(state.Operations)
			state.Operations = append(state.Operations, model.AcceptedOperation{})
			versionedRows = append(versionedRows, operationRow{operationID, revision, append([]byte(nil), data...), hash, storageVersion, bodyDigest, bodyBytes, changeCount, resultIndex})
			continue
		}
		if err := rows.Scan(&operationID, &revision, &data, &hash); err != nil {
			rows.Close()
			return state, err
		}
		accepted, err := decodeAccepted(data)
		if err != nil {
			rows.Close()
			return state, err
		}
		if accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || hash != state.Hashes[revision] {
			rows.Close()
			return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
		}
		state.Operations = append(state.Operations, accepted)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return state, rowsErr
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
	}
	return state, nil
}
