package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"

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
			if _, err := readTransactionVersion(ctx, store.database, store.schemaVersion, documentID); err != nil {
				return err
			}
			return nil
		}
		return collabstore.ErrTransactionUpgradeRequired
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction version update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	current, err := readTransactionVersion(ctx, transaction, store.schemaVersion, documentID)
	if err != nil {
		return err
	}
	if version < current {
		return collabstore.ErrTransactionDowngrade
	}
	if version == current {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE documents SET transaction_version = ? WHERE document_id = ?", version, documentID); err != nil {
		return fmt.Errorf("update transaction version: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit transaction version update: %w", err)
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
	return readTransactionVersion(ctx, store.database, store.schemaVersion, documentID)
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
		if _, err := readTransactionVersion(ctx, store.database, store.schemaVersion, documentID); err != nil {
			store.mutex.RUnlock()
			return nil, false, err
		}
		store.mutex.RUnlock()
		return nil, false, nil
	}
	var data []byte
	var revision model.Revision
	var mapHash string
	var version int
	var digest sql.NullString
	var bodyBytes, changeCount sql.NullInt64
	err := store.database.QueryRowContext(ctx, `SELECT accepted, revision, map_hash, storage_version, body_digest, body_bytes, change_count FROM operations WHERE document_id = ? AND operation_id = ?`, documentID, operationID).Scan(&data, &revision, &mapHash, &version, &digest, &bodyBytes, &changeCount)
	if errors.Is(err, sql.ErrNoRows) {
		var present int
		err = store.database.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", documentID).Scan(&present)
		store.mutex.RUnlock()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, collabstore.ErrSessionMissing
		}
		if err != nil {
			return nil, false, fmt.Errorf("query transaction document: %w", err)
		}
		return nil, false, nil
	}
	if err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("query stored transaction: %w", err)
	}
	if version == collabstore.LegacyTransactionVersion {
		store.mutex.RUnlock()
		return nil, false, nil
	}
	if version != collabstore.BulkTransactionVersion || !digest.Valid || !bodyBytes.Valid || !changeCount.Valid {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("stored transaction metadata is incomplete")
	}
	var header transaction.Header
	if err := json.Unmarshal(data, &header); err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("decode stored transaction header: %w", err)
	}
	if header.Version != transaction.Version || header.Kind != "accepted" || header.Count != changeCount.Int64 || header.Revision != revision || header.MapHash != mapHash || header.Operation.DocumentID != documentID || header.Operation.OperationID != operationID || header.Operation.Changes != nil || header.SessionID != "" || header.MessageID != "" {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("stored transaction header differs from row metadata")
	}
	rows, err := store.database.QueryContext(ctx, "SELECT chunk_index, data, chunk_digest FROM transaction_chunks WHERE document_id = ? AND operation_id = ? ORDER BY chunk_index", documentID, operationID)
	if err != nil {
		store.mutex.RUnlock()
		return nil, false, fmt.Errorf("query stored transaction chunks: %w", err)
	}
	return &sqliteChunkReader{rows: rows, expectedDigest: digest.String, expectedSize: bodyBytes.Int64, unlock: store.mutex.RUnlock}, true, nil
}

func readTransactionVersion(ctx context.Context, database queryer, schemaVersion int, documentID model.DocumentID) (int, error) {
	if schemaVersion < 4 {
		var present int
		if err := database.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE document_id = ?", documentID).Scan(&present); errors.Is(err, sql.ErrNoRows) {
			return 0, collabstore.ErrSessionMissing
		} else if err != nil {
			return 0, fmt.Errorf("query legacy transaction document: %w", err)
		}
		return collabstore.LegacyTransactionVersion, nil
	}
	var version int
	if err := database.QueryRowContext(ctx, "SELECT transaction_version FROM documents WHERE document_id = ?", documentID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return 0, collabstore.ErrSessionMissing
	} else if err != nil {
		return 0, fmt.Errorf("query transaction version: %w", err)
	}
	if version != collabstore.LegacyTransactionVersion && version != collabstore.BulkTransactionVersion {
		return 0, collabstore.ErrUnsupportedTransactionVersion
	}
	return version, nil
}

func readStoredAccepted(ctx context.Context, database queryer, documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, data []byte, version int, digest sql.NullString, bodyBytes, changeCount sql.NullInt64) (model.AcceptedOperation, error) {
	if version == collabstore.LegacyTransactionVersion {
		if digest.Valid || bodyBytes.Valid || changeCount.Valid {
			return model.AcceptedOperation{}, fmt.Errorf("stored inline transaction has bulk metadata")
		}
		return decodeAccepted(data)
	}
	if version != collabstore.BulkTransactionVersion || !digest.Valid || !bodyBytes.Valid || !changeCount.Valid {
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction metadata is incomplete")
	}
	rows, err := database.QueryContext(ctx, "SELECT chunk_index, data, chunk_digest FROM transaction_chunks WHERE document_id = ? AND operation_id = ? ORDER BY chunk_index", documentID, operationID)
	if err != nil {
		return model.AcceptedOperation{}, fmt.Errorf("query stored transaction chunks: %w", err)
	}
	headerData := data
	accepted, err := collabstore.DecodeStoredTransaction(ctx, headerData, &sqliteChunkReader{rows: rows, expectedDigest: digest.String, expectedSize: bodyBytes.Int64}, digest.String, bodyBytes.Int64, documentID, operationID, revision, mapHash)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	if int64(len(accepted.Changes)) != changeCount.Int64 {
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction change count differs from row metadata")
	}
	return accepted, nil
}

type sqliteRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

type sqliteChunkReader struct {
	rows           sqliteRows
	expectedDigest string
	expectedSize   int64
	unlock         func()
	data           []byte
	offset         int
	nextIndex      uint64
	total          int64
	hash           hash.Hash
	ended          bool
	closed         bool
	closeErr       error
	readErr        error
}

func (reader *sqliteChunkReader) Read(buffer []byte) (int, error) {
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
				return 0, reader.finish(fmt.Errorf("stored transaction body digest or byte length differs"))
			}
			reader.ended = true
			if err := reader.finish(nil); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		var index uint64
		var chunk []byte
		var chunkDigest string
		if err := reader.rows.Scan(&index, &chunk, &chunkDigest); err != nil {
			return 0, reader.finish(fmt.Errorf("read transaction chunk: %w", err))
		}
		if index != reader.nextIndex || len(chunk) == 0 || len(chunk) > collabstore.MaxStoredTransactionChunkBytes {
			return 0, reader.finish(fmt.Errorf("stored transaction chunk sequence or size is invalid"))
		}
		sum := sha256.Sum256(chunk)
		if hex.EncodeToString(sum[:]) != chunkDigest {
			return 0, reader.finish(fmt.Errorf("stored transaction chunk digest differs"))
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

func (reader *sqliteChunkReader) Close() error {
	if reader.closed {
		return reader.closeErr
	}
	if !reader.ended && reader.readErr == nil {
		reader.readErr = fmt.Errorf("stored transaction stream closed before verification")
	}
	return reader.finish(reader.readErr)
}

func (reader *sqliteChunkReader) finish(err error) error {
	if reader.closed {
		if err != nil {
			return err
		}
		return reader.closeErr
	}
	reader.closed = true
	reader.readErr = err
	reader.closeErr = reader.rows.Close()
	if reader.unlock != nil {
		reader.unlock()
		reader.unlock = nil
	}
	return errors.Join(err, reader.closeErr)
}
