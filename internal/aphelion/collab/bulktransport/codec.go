// Package bulktransport carries one atomic transaction in bounded WebSocket
// frames. Only authenticated, capability-negotiated connections may call Read.
package bulktransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/transaction"
)

const ChunkBytes = 64 << 10
const DefaultSpoolBytes int64 = 512 << 20
const DefaultWorkingBytes int64 = 2 << 30

// Includes decoded maps, validation candidates and detached public results.
// This conservative wire-byte charge is admission accounting, not a heap meter.
const WorkingSetFactor int64 = 16
const (
	begin byte = 1
	chunk byte = 2
	end   byte = 3
)

type Codec struct {
	directory string
	budget    *transaction.Budget
	working   *transaction.Budget
}

func New(directory string, bytes int64) *Codec {
	return NewWithWorkingBudget(directory, bytes, DefaultWorkingBytes)
}
func NewWithWorkingBudget(directory string, spoolBytes, workingBytes int64) *Codec {
	return &Codec{directory: directory, budget: transaction.NewBudget(spoolBytes), working: transaction.NewBudget(workingBytes)}
}
func (c *Codec) Used() int64 { return c.budget.Used() }

// ReserveWorking admits server-generated work, such as an inverse, against the
// same aggregate budget as incoming materialized transaction bodies.
func (c *Codec) ReserveWorking(bytes int64) (func(), error) { return c.working.Reserve(bytes) }

func (c *Codec) Prepare(ctx context.Context, h transaction.Header, changes []model.TileChange) (*transaction.Body, error) {
	return transaction.Prepare(ctx, c.directory, c.budget, h, changes)
}

func (c *Codec) PrepareReplay(ctx context.Context, source io.Reader, header transaction.Header, sourceBytes int64, sessionID, messageID string) (*transaction.Body, error) {
	if sourceBytes < 1 || sourceBytes > int64(^uint64(0)>>1)/WorkingSetFactor {
		return nil, fmt.Errorf("invalid replay working-set charge")
	}
	release, err := c.working.Reserve(sourceBytes * WorkingSetFactor)
	if err != nil {
		return nil, err
	}
	defer release()
	return transaction.Reframe(ctx, c.directory, c.budget, source, header, sessionID, messageID)
}

type descriptor struct {
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// Write serializes the body to private storage before beginning transmission.
// The timeout applies to each frame, not the duration of a gigantic edit.
func (c *Codec) Write(ctx context.Context, connection *websocket.Conn, h transaction.Header, changes []model.TileChange, timeout time.Duration) error {
	body, err := c.Prepare(ctx, h, changes)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	return c.WriteBody(ctx, connection, body, timeout)
}

func (c *Codec) WriteBody(ctx context.Context, connection *websocket.Conn, body *transaction.Body, timeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := body.Open()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	d, err := json.Marshal(descriptor{Size: body.Size, Digest: body.Digest})
	if err != nil {
		return err
	}
	write := func(data []byte) error {
		frameCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return connection.Write(frameCtx, websocket.MessageBinary, data)
	}
	if err := write(append([]byte{begin}, d...)); err != nil {
		return err
	}
	frame := make([]byte, 9+ChunkBytes)
	frame[0] = chunk
	for index := uint64(0); ; index++ {
		n, err := r.Read(frame[9:])
		if err != nil && err != io.EOF {
			return err
		}
		if n > 0 {
			binary.BigEndian.PutUint64(frame[1:9], index)
			if err := write(frame[:9+n]); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
	}
	return write([]byte{end})
}

// Read admits declared bytes before opening a spool and owns cleanup on every
// exit. No operation is visible until all bytes and their digest are verified.
type Reader interface {
	Read(context.Context) (websocket.MessageType, []byte, error)
}

func (c *Codec) Read(ctx context.Context, connection Reader, first []byte, timeout time.Duration) (transaction.Header, model.Operation, error) {
	h, op, release, err := c.ReadAdmitted(ctx, connection, first, timeout)
	if release != nil {
		defer release()
	}
	if err != nil {
		return transaction.Header{}, model.Operation{}, err
	}
	return h, op, nil
}

// ReadAdmitted retains a working-set lease through validation/commit or client
// projection. The owner must release it after consuming the returned operation.
func (c *Codec) ReadAdmitted(ctx context.Context, connection Reader, first []byte, timeout time.Duration) (transaction.Header, model.Operation, func(), error) {
	var release func()
	h, op, err := c.read(ctx, connection, first, timeout, &release)
	if err != nil {
		if release != nil {
			release()
		}
		return transaction.Header{}, model.Operation{}, nil, err
	}
	return h, op, release, nil
}
func (c *Codec) read(ctx context.Context, connection Reader, first []byte, timeout time.Duration, release *func()) (transaction.Header, model.Operation, error) {
	var h transaction.Header
	var op model.Operation
	if len(first) < 2 || first[0] != begin || len(first) > 1024 {
		return h, op, fmt.Errorf("bulk begin required")
	}
	decoder := json.NewDecoder(bytes.NewReader(first[1:]))
	decoder.DisallowUnknownFields()
	var d descriptor
	if err := decoder.Decode(&d); err != nil {
		return h, op, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return h, op, fmt.Errorf("trailing bulk descriptor")
	}
	spool, err := transaction.NewSpool(c.directory, d.Size, d.Digest, c.budget)
	if err != nil {
		return h, op, err
	}
	defer func() { _ = spool.Close() }()
	for {
		frameCtx, cancel := context.WithTimeout(ctx, timeout)
		kind, data, err := connection.Read(frameCtx)
		cancel()
		if err != nil {
			return h, op, err
		}
		if kind != websocket.MessageBinary || len(data) == 0 {
			return h, op, fmt.Errorf("bulk frame required")
		}
		if data[0] == end {
			if len(data) != 1 {
				return h, op, fmt.Errorf("invalid bulk end")
			}
			break
		}
		if data[0] != chunk || len(data) <= 9 || len(data) > 9+ChunkBytes {
			return h, op, fmt.Errorf("invalid bulk chunk")
		}
		if err := spool.Append(ctx, binary.BigEndian.Uint64(data[1:9]), data[9:]); err != nil {
			return h, op, err
		}
	}
	body, err := spool.Finish()
	if err != nil {
		return h, op, err
	}
	defer func() { _ = body.Close() }()
	if body.Size > int64(^uint64(0)>>1)/WorkingSetFactor {
		return h, op, fmt.Errorf("transaction working-set charge overflows")
	}
	*release, err = c.working.Reserve(body.Size * WorkingSetFactor)
	if err != nil {
		return h, op, fmt.Errorf("bulk working memory: %w", err)
	}
	r, err := body.Open()
	if err != nil {
		return h, op, err
	}
	defer func() { _ = r.Close() }()
	reader, err := transaction.OpenReader(ctx, r)
	if err != nil {
		return h, op, err
	}
	op, err = reader.Materialize()
	return reader.Header, op, err
}
