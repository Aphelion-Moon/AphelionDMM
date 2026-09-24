package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"reflect"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/transaction"
)

const (
	MaxStoredTransactionChunkBytes   = 256 << 10
	legacyInlineChangeLimit          = 64
	legacyInlineEnvelopeReserveBytes = 2048
)

// MarshalAcceptedInline keeps ordinary protocol-sized edits in the legacy
// row format even when a document also supports streamed transactions. The
// metadata reserve ensures the accepted operation can still fit inside the
// enclosing legacy protocol envelope.
func MarshalAcceptedInline(accepted model.AcceptedOperation) ([]byte, bool, error) {
	if len(accepted.Changes) > legacyInlineChangeLimit {
		return nil, false, nil
	}
	data, err := json.Marshal(accepted)
	if err != nil {
		return nil, false, fmt.Errorf("encode inline accepted operation: %w", err)
	}
	if len(data) > protocol.MaxMessageBytes-legacyInlineEnvelopeReserveBytes {
		return nil, false, nil
	}
	return data, true, nil
}

func AcceptedTransactionHeader(accepted model.AcceptedOperation, mapHash string) transaction.Header {
	operation := accepted.Operation
	if operation.InverseOf != nil {
		inverseOf := *operation.InverseOf
		operation.InverseOf = &inverseOf
	}
	count := int64(len(operation.Changes))
	operation.Changes = nil
	return transaction.Header{
		Version: transaction.Version, Count: count, Kind: "accepted", Operation: operation,
		Revision: accepted.Revision, AcceptedAt: accepted.AcceptedAt, MapHash: mapHash,
	}
}

func MarshalAcceptedTransactionHeader(accepted model.AcceptedOperation, mapHash string) (transaction.Header, []byte, error) {
	header := AcceptedTransactionHeader(accepted, mapHash)
	data, err := json.Marshal(header)
	if err != nil {
		return transaction.Header{}, nil, fmt.Errorf("encode transaction header: %w", err)
	}
	return header, data, nil
}

type StoredChunkWriter struct {
	ctx    context.Context
	write  func(index uint64, data []byte, digest string) error
	buffer []byte
	hash   hash.Hash
	size   int64
	index  uint64
}

func NewStoredChunkWriter(ctx context.Context, write func(index uint64, data []byte, digest string) error) *StoredChunkWriter {
	return &StoredChunkWriter{
		ctx: ctx, write: write, buffer: make([]byte, 0, MaxStoredTransactionChunkBytes), hash: sha256.New(),
	}
}

func (writer *StoredChunkWriter) Write(data []byte) (int, error) {
	if writer.write == nil {
		return 0, fmt.Errorf("transaction chunk writer is not configured")
	}
	written := 0
	for len(data) != 0 {
		if err := writer.ctx.Err(); err != nil {
			return written, err
		}
		space := MaxStoredTransactionChunkBytes - len(writer.buffer)
		count := min(space, len(data))
		piece := data[:count]
		writer.buffer = append(writer.buffer, piece...)
		_, _ = writer.hash.Write(piece)
		writer.size += int64(count)
		written += count
		data = data[count:]
		if len(writer.buffer) == MaxStoredTransactionChunkBytes {
			if err := writer.flush(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (writer *StoredChunkWriter) Flush() error { return writer.flush() }

func (writer *StoredChunkWriter) Digest() string { return hex.EncodeToString(writer.hash.Sum(nil)) }

func (writer *StoredChunkWriter) Size() int64 { return writer.size }

func (writer *StoredChunkWriter) flush() error {
	if len(writer.buffer) == 0 {
		return nil
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	chunk := append([]byte(nil), writer.buffer...)
	digest := sha256.Sum256(chunk)
	if err := writer.write(writer.index, chunk, hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	writer.index++
	writer.buffer = writer.buffer[:0]
	return nil
}

func WriteAcceptedTransaction(ctx context.Context, accepted model.AcceptedOperation, mapHash string, write func(index uint64, data []byte, digest string) error) ([]byte, string, int64, error) {
	header, headerData, err := MarshalAcceptedTransactionHeader(accepted, mapHash)
	if err != nil {
		return nil, "", 0, fmt.Errorf("encode transaction header: %w", err)
	}
	digest, size, err := EncodeStoredTransaction(ctx, header, accepted.Changes, write)
	if err != nil {
		return nil, "", 0, err
	}
	return headerData, digest, size, nil
}

func EncodeStoredTransaction(ctx context.Context, header transaction.Header, changes []model.TileChange, write func(index uint64, data []byte, digest string) error) (string, int64, error) {
	writer := NewStoredChunkWriter(ctx, write)
	if err := transaction.Encode(ctx, writer, header, changes); err != nil {
		return "", 0, fmt.Errorf("encode accepted transaction: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return "", 0, fmt.Errorf("write final transaction chunk: %w", err)
	}
	return writer.Digest(), writer.Size(), nil
}

func DecodeStoredTransaction(ctx context.Context, headerData []byte, body io.ReadCloser, digest string, size int64, documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string) (model.AcceptedOperation, error) {
	var storedHeader transaction.Header
	if err := json.Unmarshal(headerData, &storedHeader); err != nil {
		_ = body.Close()
		return model.AcceptedOperation{}, fmt.Errorf("decode stored transaction header: %w", err)
	}
	if storedHeader.Version != transaction.Version || storedHeader.Kind != "accepted" || storedHeader.Count < 1 || storedHeader.Revision != revision || storedHeader.MapHash != mapHash || storedHeader.Operation.Changes != nil || storedHeader.Operation.DocumentID != documentID || storedHeader.Operation.OperationID != operationID || storedHeader.SessionID != "" || storedHeader.MessageID != "" {
		_ = body.Close()
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction header differs from indexed metadata")
	}
	verifiedBody := &verifiedTransactionBody{body: body, hash: sha256.New(), expectedDigest: digest, expectedSize: size}
	reader, err := transaction.OpenReader(ctx, verifiedBody)
	if err != nil {
		_ = body.Close()
		return model.AcceptedOperation{}, err
	}
	if !reflect.DeepEqual(reader.Header, storedHeader) {
		_ = body.Close()
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction body header differs from indexed header")
	}
	operation, err := reader.Materialize()
	if err != nil {
		_ = body.Close()
		return model.AcceptedOperation{}, err
	}
	if int64(len(operation.Changes)) != storedHeader.Count {
		_ = body.Close()
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction record count differs from header")
	}
	if !verifiedBody.verified {
		_ = body.Close()
		return model.AcceptedOperation{}, fmt.Errorf("stored transaction body ended without digest verification")
	}
	if err := body.Close(); err != nil {
		return model.AcceptedOperation{}, fmt.Errorf("close stored transaction body: %w", err)
	}
	return model.AcceptedOperation{Operation: operation, Revision: storedHeader.Revision, AcceptedAt: storedHeader.AcceptedAt}, nil
}

func MaterializeTransactionBody(ctx context.Context, body *transaction.Body) (transaction.Header, model.Operation, error) {
	if body == nil {
		return transaction.Header{}, model.Operation{}, fmt.Errorf("transaction body is nil")
	}
	raw, err := body.Open()
	if err != nil {
		return transaction.Header{}, model.Operation{}, err
	}
	verifiedBody := &verifiedTransactionBody{body: raw, hash: sha256.New(), expectedDigest: body.Digest, expectedSize: body.Size}
	reader, err := transaction.OpenReader(ctx, verifiedBody)
	if err != nil {
		_ = raw.Close()
		return transaction.Header{}, model.Operation{}, err
	}
	header := reader.Header
	operation, readErr := reader.Materialize()
	closeErr := raw.Close()
	if readErr != nil {
		return transaction.Header{}, model.Operation{}, readErr
	}
	if closeErr != nil {
		return transaction.Header{}, model.Operation{}, closeErr
	}
	if !verifiedBody.verified || int64(len(operation.Changes)) != header.Count {
		return transaction.Header{}, model.Operation{}, fmt.Errorf("transaction body count or digest differs")
	}
	return header, operation, nil
}

type verifiedTransactionBody struct {
	body           io.ReadCloser
	hash           hash.Hash
	expectedDigest string
	expectedSize   int64
	size           int64
	verified       bool
}

func (reader *verifiedTransactionBody) Read(buffer []byte) (int, error) {
	count, err := reader.body.Read(buffer)
	if count != 0 {
		_, _ = reader.hash.Write(buffer[:count])
		reader.size += int64(count)
	}
	if err == io.EOF {
		if reader.size != reader.expectedSize || hex.EncodeToString(reader.hash.Sum(nil)) != reader.expectedDigest {
			return count, fmt.Errorf("stored transaction body digest or byte length differs")
		}
		reader.verified = true
	}
	return count, err
}

func (reader *verifiedTransactionBody) Close() error { return reader.body.Close() }
