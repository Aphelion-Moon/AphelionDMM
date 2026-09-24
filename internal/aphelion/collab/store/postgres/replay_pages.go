package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("begin PostgreSQL replay page: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var snapshotRevision, headRevision model.Revision
	var snapshotHash, snapshotLedgerHash, headHash, headLedgerHash string
	err = tx.QueryRow(ctx, `SELECT d.snapshot_revision, d.snapshot_hash, sh.map_hash, d.current_revision, d.current_hash, hh.map_hash
		FROM collaboration_documents d
		LEFT JOIN collaboration_revision_hashes sh ON sh.document_id = d.document_id AND sh.revision = d.snapshot_revision
		LEFT JOIN collaboration_revision_hashes hh ON hh.document_id = d.document_id AND hh.revision = d.current_revision
		WHERE d.document_id = $1`, documentID).Scan(&snapshotRevision, &snapshotHash, &snapshotLedgerHash, &headRevision, &headHash, &headLedgerHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return collabstore.ReplayPage{}, collabstore.ErrSessionMissing
	}
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("read PostgreSQL replay checkpoint: %w", err)
	}
	snapshotHash = strings.TrimSpace(snapshotHash)
	snapshotLedgerHash = strings.TrimSpace(snapshotLedgerHash)
	headHash = strings.TrimSpace(headHash)
	headLedgerHash = strings.TrimSpace(headLedgerHash)
	if !collabstore.ValidReplayHash(snapshotHash) || snapshotHash != snapshotLedgerHash || !collabstore.ValidReplayHash(headHash) || headHash != headLedgerHash || headRevision < snapshotRevision {
		return collabstore.ReplayPage{}, fmt.Errorf("PostgreSQL replay checkpoint differs from its revision hash ledger")
	}
	if err := collabstore.CheckReplaySnapshot(expectedSnapshot, snapshotRevision); err != nil {
		return collabstore.ReplayPage{}, err
	}
	page := collabstore.ReplayPage{SnapshotRevision: snapshotRevision, HeadRevision: headRevision, ThroughRevision: through, Entries: make([]collabstore.ReplayEntry, 0, min(limit, 32))}
	if after > headRevision || through > headRevision {
		return collabstore.ReplayPage{}, collabstore.ErrReplayRevisionRange
	}
	if after < snapshotRevision || after == through {
		if err := tx.Commit(ctx); err != nil {
			return collabstore.ReplayPage{}, fmt.Errorf("finish PostgreSQL replay page: %w", err)
		}
		return page, nil
	}
	query := `SELECT o.operation_id, o.revision, o.accepted, o.map_hash, h.map_hash
		FROM collaboration_operations o LEFT JOIN collaboration_revision_hashes h ON h.document_id = o.document_id AND h.revision = o.revision
		WHERE o.document_id = $1 AND o.revision > $2 AND o.revision <= $3 ORDER BY o.revision LIMIT $4`
	if store.schemaVersion >= 4 {
		query = `SELECT o.operation_id, o.revision, o.accepted, o.map_hash, o.storage_version, o.body_digest, o.body_bytes, o.change_count, h.map_hash
			FROM collaboration_operations o LEFT JOIN collaboration_revision_hashes h ON h.document_id = o.document_id AND h.revision = o.revision
			WHERE o.document_id = $1 AND o.revision > $2 AND o.revision <= $3 ORDER BY o.revision LIMIT $4`
	}
	rows, err := tx.Query(ctx, query, documentID, after, through, limit+1)
	if err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("query PostgreSQL replay page: %w", err)
	}
	var inlineBytes int
	for rows.Next() {
		var operationID model.OperationID
		var revision model.Revision
		var acceptedBytes []byte
		var mapHash string
		var storageVersion = collabstore.LegacyTransactionVersion
		var digest *string
		var bodyBytes, changeCount *int64
		var ledgerHash *string
		if store.schemaVersion >= 4 {
			if err := rows.Scan(&operationID, &revision, &acceptedBytes, &mapHash, &storageVersion, &digest, &bodyBytes, &changeCount, &ledgerHash); err != nil {
				rows.Close()
				return collabstore.ReplayPage{}, fmt.Errorf("read PostgreSQL replay row: %w", err)
			}
		} else if err := rows.Scan(&operationID, &revision, &acceptedBytes, &mapHash, &ledgerHash); err != nil {
			rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("read legacy PostgreSQL replay row: %w", err)
		}
		mapHash = strings.TrimSpace(mapHash)
		if len(page.Entries) >= limit {
			page.HasMore = true
			break
		}
		if len(acceptedBytes) > collabstore.MaxReplayPageInlineBytes {
			rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("PostgreSQL replay row exceeds the bounded metadata page")
		}
		if inlineBytes+len(acceptedBytes) > collabstore.MaxReplayPageInlineBytes {
			page.HasMore = true
			break
		}
		if ledgerHash == nil || !collabstore.ValidReplayHash(mapHash) || strings.TrimSpace(*ledgerHash) != mapHash {
			rows.Close()
			return collabstore.ReplayPage{}, fmt.Errorf("PostgreSQL replay row hash differs from its revision ledger")
		}
		entry, err := postgresReplayEntry(documentID, operationID, revision, mapHash, acceptedBytes, storageVersion, digest, bodyBytes, changeCount, store.schemaVersion)
		if err != nil {
			rows.Close()
			return collabstore.ReplayPage{}, err
		}
		page.Entries = append(page.Entries, entry)
		inlineBytes += len(acceptedBytes)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("iterate PostgreSQL replay page: %w", rowsErr)
	}
	if err := collabstore.ValidateReplayPageSequence(page.Entries, after, through, page.HasMore); err != nil {
		return collabstore.ReplayPage{}, err
	}
	for _, entry := range page.Entries {
		if entry.StorageVersion != collabstore.BulkTransactionVersion {
			continue
		}
		var storedBytes int64
		if err := tx.QueryRow(ctx, "SELECT COALESCE(SUM(octet_length(data)), 0) FROM collaboration_transaction_chunks WHERE operation_id = $1", entry.Accepted.OperationID).Scan(&storedBytes); err != nil {
			return collabstore.ReplayPage{}, fmt.Errorf("read PostgreSQL replay transaction size: %w", err)
		}
		if storedBytes != entry.BodyBytes {
			return collabstore.ReplayPage{}, fmt.Errorf("PostgreSQL replay transaction chunk size differs from indexed byte length")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return collabstore.ReplayPage{}, fmt.Errorf("finish PostgreSQL replay page: %w", err)
	}
	return page, nil
}

func postgresReplayEntry(documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, acceptedBytes []byte, storageVersion int, digest *string, bodyBytes, changeCount *int64, schemaVersion int) (collabstore.ReplayEntry, error) {
	if schemaVersion < 4 {
		accepted, err := decodeAccepted(acceptedBytes)
		if err != nil {
			return collabstore.ReplayEntry{}, fmt.Errorf("decode legacy PostgreSQL replay row: %w", err)
		}
		return collabstore.ValidateLegacyReplayEntry(documentID, operationID, revision, mapHash, len(acceptedBytes), accepted)
	}
	switch storageVersion {
	case collabstore.LegacyTransactionVersion:
		if digest != nil || bodyBytes != nil || changeCount != nil {
			return collabstore.ReplayEntry{}, fmt.Errorf("PostgreSQL legacy replay row contains V2 body metadata")
		}
		accepted, err := decodeAccepted(acceptedBytes)
		if err != nil {
			return collabstore.ReplayEntry{}, fmt.Errorf("decode PostgreSQL legacy replay row: %w", err)
		}
		return collabstore.ValidateLegacyReplayEntry(documentID, operationID, revision, mapHash, len(acceptedBytes), accepted)
	case collabstore.BulkTransactionVersion:
		if digest == nil || bodyBytes == nil || changeCount == nil {
			return collabstore.ReplayEntry{}, fmt.Errorf("PostgreSQL versioned replay row is missing body metadata")
		}
		return collabstore.ValidateVersionedReplayEntry(documentID, operationID, revision, mapHash, acceptedBytes, strings.TrimSpace(*digest), *bodyBytes, *changeCount)
	default:
		return collabstore.ReplayEntry{}, collabstore.ErrUnsupportedTransactionVersion
	}
}
