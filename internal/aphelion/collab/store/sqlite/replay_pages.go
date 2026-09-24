package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (store *Store) LoadReplayPage(ctx context.Context, documentID model.DocumentID, after, through model.Revision, expectedSnapshot *model.Revision, limit int) (collabstore.ReplayPage, error) {
	if err := ctx.Err(); err != nil {
		return collabstore.ReplayPage{}, err
	}
	if err := collabstore.ValidateReplayPageRequest(after, through, limit); err != nil {
		return collabstore.ReplayPage{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ReplayPage{}, collabstore.ErrStoreClosed
	}
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("begin SQLite replay page: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var snapshotRevision, headRevision model.Revision
	var snapshotHash, snapshotLedgerHash, headHash string
	err = tx.QueryRowContext(ctx, `SELECT d.snapshot_revision, d.snapshot_hash, COALESCE(s.map_hash, ''),
		COALESCE((SELECT revision FROM revision_hashes WHERE document_id = d.document_id ORDER BY revision DESC LIMIT 1), d.snapshot_revision),
		COALESCE((SELECT map_hash FROM revision_hashes WHERE document_id = d.document_id ORDER BY revision DESC LIMIT 1), d.snapshot_hash)
		FROM documents d LEFT JOIN revision_hashes s ON s.document_id = d.document_id AND s.revision = d.snapshot_revision
		WHERE d.document_id = ?`, documentID).Scan(&snapshotRevision, &snapshotHash, &snapshotLedgerHash, &headRevision, &headHash)
	if errors.Is(err, sql.ErrNoRows) {
		return collabstore.ReplayPage{}, collabstore.ErrSessionMissing
	}
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("read SQLite replay checkpoint: %w", err)
	}
	if !collabstore.ValidReplayHash(snapshotHash) || snapshotHash != snapshotLedgerHash || !collabstore.ValidReplayHash(headHash) || headRevision < snapshotRevision {
		return collabstore.ReplayPage{}, fmt.Errorf("SQLite replay checkpoint differs from its revision hash ledger")
	}
	if err := collabstore.CheckReplaySnapshot(expectedSnapshot, snapshotRevision); err != nil {
		return collabstore.ReplayPage{}, err
	}
	page := collabstore.ReplayPage{SnapshotRevision: snapshotRevision, HeadRevision: headRevision, ThroughRevision: through, Entries: make([]collabstore.ReplayEntry, 0, min(limit, 32))}
	if after > headRevision || through > headRevision {
		return collabstore.ReplayPage{}, collabstore.ErrReplayRevisionRange
	}
	if after < snapshotRevision || after == through {
		if err := tx.Commit(); err != nil {
			return collabstore.ReplayPage{}, fmt.Errorf("finish SQLite replay page: %w", err)
		}
		return page, nil
	}
	query := `SELECT o.operation_id, o.revision, o.accepted, o.map_hash, o.storage_version, o.body_digest, o.body_bytes, o.change_count, h.map_hash
		FROM operations o LEFT JOIN revision_hashes h ON h.document_id = o.document_id AND h.revision = o.revision
		WHERE o.document_id = ? AND o.revision > ? AND o.revision <= ? ORDER BY o.revision LIMIT ?`
	if store.schemaVersion < 4 {
		// Retained V3 databases do not have V2 body-index columns. Synthesize
		// their explicit legacy values without migrating or rewriting the source.
		query = `SELECT o.operation_id, o.revision, o.accepted, o.map_hash, 1 AS storage_version,
			NULL AS body_digest, NULL AS body_bytes, NULL AS change_count, h.map_hash
			FROM operations o LEFT JOIN revision_hashes h ON h.document_id = o.document_id AND h.revision = o.revision
			WHERE o.document_id = ? AND o.revision > ? AND o.revision <= ? ORDER BY o.revision LIMIT ?`
	}
	rows, err := tx.QueryContext(ctx, query, documentID, after, through, limit+1)
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("query SQLite replay page: %w", err)
	}
	var inlineBytes int
	for rows.Next() {
		var operationID model.OperationID
		var revision model.Revision
		var acceptedBytes []byte
		var mapHash string
		var storageVersion int
		var digest, ledgerHash sql.NullString
		var bodyBytes, changeCount sql.NullInt64
		if err := rows.Scan(&operationID, &revision, &acceptedBytes, &mapHash, &storageVersion, &digest, &bodyBytes, &changeCount, &ledgerHash); err != nil {
			_ = rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("read SQLite replay row: %w", err)
		}
		if len(page.Entries) >= limit {
			page.HasMore = true
			break
		}
		if len(acceptedBytes) > collabstore.MaxReplayPageInlineBytes {
			_ = rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("SQLite replay row exceeds the bounded metadata page")
		}
		if inlineBytes+len(acceptedBytes) > collabstore.MaxReplayPageInlineBytes {
			page.HasMore = true
			break
		}
		if !ledgerHash.Valid || !collabstore.ValidReplayHash(mapHash) || ledgerHash.String != mapHash {
			_ = rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("SQLite replay row hash differs from its revision ledger")
		}
		entry, err := sqliteReplayEntry(documentID, operationID, revision, mapHash, acceptedBytes, storageVersion, digest, bodyBytes, changeCount)
		if err != nil {
			_ = rows.Close()
			return collabstore.ReplayPage{}, err
		}
		page.Entries = append(page.Entries, entry)
		inlineBytes += len(acceptedBytes)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("iterate SQLite replay page: %w", rowsErr)
	}
	if closeErr != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("close SQLite replay page rows: %w", closeErr)
	}
	if err := collabstore.ValidateReplayPageSequence(page.Entries, after, through, page.HasMore); err != nil {
		return collabstore.ReplayPage{}, err
	}
	for _, entry := range page.Entries {
		if entry.StorageVersion != collabstore.BulkTransactionVersion {
			continue
		}
		var storedBytes int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(length(data)), 0) FROM transaction_chunks WHERE document_id = ? AND operation_id = ?", documentID, entry.Accepted.OperationID).Scan(&storedBytes); err != nil {
			return collabstore.ReplayPage{}, fmt.Errorf("read SQLite replay transaction size: %w", err)
		}
		if storedBytes != entry.BodyBytes {
			return collabstore.ReplayPage{}, fmt.Errorf("SQLite replay transaction chunk size differs from indexed byte length")
		}
	}
	if err := tx.Commit(); err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("finish SQLite replay page: %w", err)
	}
	return page, nil
}

func sqliteReplayEntry(documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, acceptedBytes []byte, storageVersion int, digest sql.NullString, bodyBytes, changeCount sql.NullInt64) (collabstore.ReplayEntry, error) {
	switch storageVersion {
	case collabstore.LegacyTransactionVersion:
		if digest.Valid || bodyBytes.Valid || changeCount.Valid {
			return collabstore.ReplayEntry{}, fmt.Errorf("SQLite legacy replay row contains V2 body metadata")
		}
		accepted, err := decodeAccepted(acceptedBytes)
		if err != nil {
			return collabstore.ReplayEntry{}, fmt.Errorf("decode SQLite legacy replay row: %w", err)
		}
		return collabstore.ValidateLegacyReplayEntry(documentID, operationID, revision, mapHash, len(acceptedBytes), accepted)
	case collabstore.BulkTransactionVersion:
		if !digest.Valid || !bodyBytes.Valid || !changeCount.Valid {
			return collabstore.ReplayEntry{}, fmt.Errorf("SQLite versioned replay row is missing body metadata")
		}
		return collabstore.ValidateVersionedReplayEntry(documentID, operationID, revision, mapHash, acceptedBytes, digest.String, bodyBytes.Int64, changeCount.Int64)
	default:
		return collabstore.ReplayEntry{}, collabstore.ErrUnsupportedTransactionVersion
	}
}
