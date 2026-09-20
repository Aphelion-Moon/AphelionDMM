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
	state, err := loadRecovery(ctx, transaction, documentID)
	if err != nil {
		return engine.RecoveryState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return engine.RecoveryState{}, err
	}
	return state, nil
}

func loadRecovery(ctx context.Context, database queryer, documentID model.DocumentID) (engine.RecoveryState, error) {
	return readRecovery(ctx, database, documentID, nil)
}

func readRecovery(ctx context.Context, database queryer, documentID model.DocumentID, encodedBytes *int) (engine.RecoveryState, error) {
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
	rows, err = database.QueryContext(ctx, "SELECT operation_id, revision, accepted, map_hash FROM operations WHERE document_id = ? ORDER BY revision", documentID)
	if err != nil {
		return state, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var operationID model.OperationID
		var hash string
		if err := rows.Scan(&operationID, &revision, &data, &hash); err != nil {
			return state, err
		}
		accepted, err := decodeAccepted(data)
		if err != nil {
			return state, err
		}
		if accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || hash != state.Hashes[revision] {
			return state, fmt.Errorf("stored operation identity/revision/hash differs from row/ledger")
		}
		state.Operations = append(state.Operations, accepted)
		if encodedBytes != nil {
			*encodedBytes += len(data) + len(operationID) + len(hash) + 8
		}
	}
	if err := rows.Err(); err != nil {
		return state, err
	}
	return state, nil
}
