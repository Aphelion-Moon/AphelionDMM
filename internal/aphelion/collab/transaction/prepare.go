package transaction

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"

	"sdmm/internal/aphelion/collab/model"
)

type admittedWriter struct {
	writer  io.Writer
	budget  *Budget
	written int64
}

func (w *admittedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w.budget.acquire(int64(len(p))); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	w.written += int64(n)
	if n < len(p) {
		w.budget.release(int64(len(p) - n))
	}
	return n, err
}

// Prepare stages an outgoing immutable body once. Resource admission is based
// on bytes actually produced, and is released when the body owner closes it.
func Prepare(ctx context.Context, directory string, budget *Budget, h Header, changes []model.TileChange) (*Body, error) {
	return prepare(ctx, directory, budget, func(w io.Writer) error { return Encode(ctx, w, h, changes) })
}

// Reframe validates stored metadata and streams one change at a time into a
// wire body. Database readers can be closed before network transmission starts.
func Reframe(ctx context.Context, directory string, budget *Budget, source io.Reader, expected Header, sessionID, messageID string) (*Body, error) {
	reader, err := OpenReader(ctx, source)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(reader.Header, expected) || expected.Kind != "accepted" {
		return nil, fmt.Errorf("stored replay metadata differs from its index")
	}
	header := expected
	header.Kind = "operation_accepted"
	header.SessionID = sessionID
	header.MessageID = messageID
	return prepare(ctx, directory, budget, func(w io.Writer) error { return encodeRecords(ctx, w, header, reader.Next) })
}

func prepare(ctx context.Context, directory string, budget *Budget, encode func(io.Writer) error) (*Body, error) {
	if budget == nil {
		return nil, fmt.Errorf("transaction admission budget is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(directory, "aphelion-transaction-*")
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	w := &admittedWriter{writer: io.MultiWriter(file, digest), budget: budget}
	buffer := bufio.NewWriterSize(w, 64<<10)
	err = encode(buffer)
	if err == nil {
		err = buffer.Flush()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(file.Name())
		budget.release(w.written)
		return nil, err
	}
	return &Body{path: file.Name(), Size: w.written, Digest: hex.EncodeToString(digest.Sum(nil)), budget: budget}, nil
}
