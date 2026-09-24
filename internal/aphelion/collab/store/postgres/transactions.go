package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
	"sdmm/internal/aphelion/collab/transaction"
)

func (store *Store) ConfigureTransactions(ctx context.Context, documentID model.DocumentID, version int) error {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if version != collabstore.LegacyTransactionVersion && version != collabstore.BulkTransactionVersion {
		return collabstore.ErrUnsupportedTransactionVersion
	}
	if store.schemaVersion < 4 {
		if version == collabstore.LegacyTransactionVersion {
			_, err := readTransactionVersion(ctx, store.pool, store.schemaVersion, documentID, false)
			return err
		}
		return collabstore.ErrTransactionUpgradeRequired
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin PostgreSQL transaction version update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	current, err := readTransactionVersion(ctx, tx, store.schemaVersion, documentID, true)
	if err != nil {
		return err
	}
	if version < current {
		return collabstore.ErrTransactionDowngrade
	}
	if version == current {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE collaboration_documents SET transaction_version = $2 WHERE document_id = $1`, documentID, version); err != nil {
		return fmt.Errorf("update PostgreSQL transaction version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit PostgreSQL transaction version update: %w", err)
	}
	return nil
}

func (store *Store) TransactionVersion(ctx context.Context, documentID model.DocumentID) (int, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return 0, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return readTransactionVersion(ctx, store.pool, store.schemaVersion, documentID, false)
}

func readTransactionVersion(ctx context.Context, database queryer, schemaVersion int, documentID model.DocumentID, lock bool) (int, error) {
	query := `SELECT transaction_version FROM collaboration_documents WHERE document_id = $1`
	if schemaVersion < 4 {
		query = `SELECT 1 FROM collaboration_documents WHERE document_id = $1`
	} else if lock {
		query += ` FOR UPDATE`
	}
	var version int
	if err := database.QueryRow(ctx, query, documentID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return 0, collabstore.ErrSessionMissing
	} else if err != nil {
		return 0, fmt.Errorf("query PostgreSQL transaction version: %w", err)
	}
	if schemaVersion < 4 {
		return collabstore.LegacyTransactionVersion, nil
	}
	if version != collabstore.LegacyTransactionVersion && version != collabstore.BulkTransactionVersion {
		return 0, collabstore.ErrUnsupportedTransactionVersion
	}
	return version, nil
}

func (store *Store) AppendTransaction(ctx context.Context, body *transaction.Body) error {
	header, operation, err := collabstore.MaterializeTransactionBody(ctx, body)
	if err != nil {
		return err
	}
	if header.Version != collabstore.BulkTransactionVersion || (header.Kind != "" && header.Kind != "accepted") {
		return collabstore.ErrUnsupportedTransactionVersion
	}
	version, err := store.TransactionVersion(ctx, operation.DocumentID)
	if err != nil {
		return err
	}
	if version != collabstore.BulkTransactionVersion {
		return fmt.Errorf("document transaction version is %d; V2 body cannot be appended", version)
	}
	accepted := model.AcceptedOperation{Operation: operation, Revision: header.Revision, AcceptedAt: header.AcceptedAt}
	return store.append(ctx, accepted, &header.MapHash, true)
}

func lookupStoredOperation(ctx context.Context, database queryer, schemaVersion int, documentID model.DocumentID, operationID model.OperationID) (model.AcceptedOperation, string, bool, error) {
	var data []byte
	if schemaVersion < 4 {
		err := database.QueryRow(ctx, `SELECT accepted FROM collaboration_operations WHERE document_id = $1 AND operation_id = $2`, documentID, operationID).Scan(&data)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.AcceptedOperation{}, "", false, nil
		}
		if err != nil {
			return model.AcceptedOperation{}, "", false, fmt.Errorf("lookup PostgreSQL operation: %w", err)
		}
		accepted, err := decodeAccepted(data)
		return accepted, "", true, err
	}
	var revision model.Revision
	var mapHash string
	var storageVersion int
	var digest *string
	var bodyBytes, changeCount *int64
	err := database.QueryRow(ctx, `SELECT accepted, revision, map_hash, storage_version, body_digest, body_bytes, change_count FROM collaboration_operations WHERE document_id = $1 AND operation_id = $2`, documentID, operationID).Scan(&data, &revision, &mapHash, &storageVersion, &digest, &bodyBytes, &changeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AcceptedOperation{}, "", false, nil
	}
	if err != nil {
		return model.AcceptedOperation{}, "", false, fmt.Errorf("lookup PostgreSQL operation: %w", err)
	}
	mapHash = strings.TrimSpace(mapHash)
	accepted, err := readStoredAccepted(ctx, database, documentID, operationID, revision, mapHash, data, storageVersion, digest, bodyBytes, changeCount)
	return accepted, mapHash, true, err
}

func readStoredAccepted(ctx context.Context, database queryer, documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, data []byte, storageVersion int, digest *string, bodyBytes, changeCount *int64) (model.AcceptedOperation, error) {
	if storageVersion == collabstore.LegacyTransactionVersion {
		if digest != nil || bodyBytes != nil || changeCount != nil {
			return model.AcceptedOperation{}, fmt.Errorf("stored inline transaction has bulk metadata")
		}
		return decodeAccepted(data)
	}
	if storageVersion != collabstore.BulkTransactionVersion || digest == nil || bodyBytes == nil || changeCount == nil || *bodyBytes < 1 || *changeCount < 1 {
		return model.AcceptedOperation{}, fmt.Errorf("stored PostgreSQL transaction metadata is incomplete")
	}
	rows, err := database.Query(ctx, `SELECT chunk_index, data, chunk_digest FROM collaboration_transaction_chunks WHERE operation_id = $1 ORDER BY chunk_index`, operationID)
	if err != nil {
		return model.AcceptedOperation{}, fmt.Errorf("query PostgreSQL transaction chunks: %w", err)
	}
	accepted, err := collabstore.DecodeStoredTransaction(ctx, data, &postgresChunkReader{rows: rows, expectedDigest: strings.TrimSpace(*digest), expectedSize: *bodyBytes}, strings.TrimSpace(*digest), *bodyBytes, documentID, operationID, revision, mapHash)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	if int64(len(accepted.Changes)) != *changeCount {
		return model.AcceptedOperation{}, fmt.Errorf("stored PostgreSQL transaction change count differs from row metadata")
	}
	return accepted, nil
}

func (store *Store) OpenTransaction(ctx context.Context, documentID model.DocumentID, operationID model.OperationID) (io.ReadCloser, bool, error) {
	store.mutex.RLock()
	if store.closed {
		store.mutex.RUnlock()
		return nil, false, collabstore.ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		store.mutex.RUnlock()
		return nil, false, err
	}
	if store.schemaVersion < 4 {
		_, err := readTransactionVersion(ctx, store.pool, store.schemaVersion, documentID, false)
		store.mutex.RUnlock()
		return nil, false, err
	}
	var data []byte
	var revision model.Revision
	var mapHash string
	var storageVersion int
	var digest *string
	var bodyBytes, changeCount *int64
	err := store.pool.QueryRow(ctx, `SELECT accepted, revision, map_hash, storage_version, body_digest, body_bytes, change_count FROM collaboration_operations WHERE document_id = $1 AND operation_id = $2`, documentID, operationID).Scan(&data, &revision, &mapHash, &storageVersion, &digest, &bodyBytes, &changeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := documentExists(ctx, store.pool, documentID)
		store.mutex.RUnlock()
		if existsErr != nil {
			return nil, false, existsErr
		}
		if !exists {
			return nil, false, collabstore.ErrSessionMissing
		}
		return nil, false, nil
	}
	if err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("query PostgreSQL stored transaction: %w", err)
	}
	if storageVersion == collabstore.LegacyTransactionVersion {
		store.mutex.RUnlock()
		return nil, false, nil
	}
	if storageVersion != collabstore.BulkTransactionVersion || digest == nil || bodyBytes == nil || changeCount == nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("stored PostgreSQL transaction metadata is incomplete")
	}
	mapHash = strings.TrimSpace(mapHash)
	var header transaction.Header
	if err := json.Unmarshal(data, &header); err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("decode PostgreSQL stored transaction header: %w", err)
	}
	if header.Version != transaction.Version || header.Kind != "accepted" || header.Count != *changeCount || header.Revision != revision || header.MapHash != mapHash || header.Operation.DocumentID != documentID || header.Operation.OperationID != operationID || len(header.Operation.Changes) != 0 || header.SessionID != "" || header.MessageID != "" {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("stored PostgreSQL transaction header differs from indexed metadata")
	}
	rows, err := store.pool.Query(ctx, `SELECT chunk_index, data, chunk_digest FROM collaboration_transaction_chunks WHERE operation_id = $1 ORDER BY chunk_index`, operationID)
	if err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("query PostgreSQL stored transaction chunks: %w", err)
	}
	return &postgresChunkReader{rows: rows, expectedDigest: strings.TrimSpace(*digest), expectedSize: *bodyBytes, unlock: store.mutex.RUnlock}, true, nil
}

type postgresChunkReader struct {
	rows           pgx.Rows
	expectedDigest string
	expectedSize   int64
	unlock         func()
	data           []byte
	offset         int
	nextIndex      int64
	total          int64
	hash           hash.Hash
	ended          bool
	closed         bool
	closeErr       error
	readErr        error
}

func (reader *postgresChunkReader) Read(buffer []byte) (int, error) {
	if reader.closed {
		if reader.readErr != nil {
			return 0, reader.readErr
		}
		return 0, io.ErrClosedPipe
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if reader.hash == nil {
		reader.hash = sha256.New()
	}
	for reader.offset >= len(reader.data) {
		if !reader.rows.Next() {
			if err := reader.rows.Err(); err != nil {
				return 0, reader.finish(err)
			}
			if reader.total != reader.expectedSize || hex.EncodeToString(reader.hash.Sum(nil)) != reader.expectedDigest {
				return 0, reader.finish(fmt.Errorf("stored PostgreSQL transaction body digest or byte length differs"))
			}
			reader.ended = true
			if err := reader.finish(nil); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		var index int64
		var chunk []byte
		var digest string
		if err := reader.rows.Scan(&index, &chunk, &digest); err != nil {
			return 0, reader.finish(fmt.Errorf("read PostgreSQL transaction chunk: %w", err))
		}
		if index != reader.nextIndex || len(chunk) == 0 || len(chunk) > collabstore.MaxStoredTransactionChunkBytes {
			return 0, reader.finish(fmt.Errorf("stored PostgreSQL transaction chunk sequence or size is invalid"))
		}
		sum := sha256.Sum256(chunk)
		if hex.EncodeToString(sum[:]) != strings.TrimSpace(digest) {
			return 0, reader.finish(fmt.Errorf("stored PostgreSQL transaction chunk digest differs"))
		}
		_, _ = reader.hash.Write(chunk)
		reader.total += int64(len(chunk))
		reader.nextIndex++
		reader.data = chunk
		reader.offset = 0
	}
	count := copy(buffer, reader.data[reader.offset:])
	reader.offset += count
	return count, nil
}

func (reader *postgresChunkReader) Close() error {
	if reader.closed {
		return reader.closeErr
	}
	if !reader.ended && reader.readErr == nil {
		reader.readErr = fmt.Errorf("stored PostgreSQL transaction stream closed before verification")
	}
	return reader.finish(reader.readErr)
}

func (reader *postgresChunkReader) finish(err error) error {
	if reader.closed {
		if err != nil {
			return err
		}
		return reader.closeErr
	}
	reader.closed = true
	reader.readErr = err
	reader.rows.Close()
	if reader.unlock != nil {
		reader.unlock()
		reader.unlock = nil
	}
	reader.closeErr = err
	return err
}
