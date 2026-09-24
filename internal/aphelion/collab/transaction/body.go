// Package transaction encodes versioned edit bodies independently of transport
// chunks and storage pages. A body describes one operation, never partial edits.
package transaction

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
)

const Version = 2

// Metadata contains identifiers and hashes only; tile bodies are separate
// records. This bounds hostile inline header allocations independently of edits.
const MaxHeaderBytes = 16 << 10

type Header struct {
	Version    int             `json:"version"`
	Count      int64           `json:"count"`
	Kind       string          `json:"kind"`
	SessionID  string          `json:"session_id"`
	MessageID  string          `json:"message_id"`
	Operation  model.Operation `json:"operation"`
	Revision   model.Revision  `json:"revision,omitempty"`
	AcceptedAt time.Time       `json:"accepted_at,omitempty"`
	MapHash    string          `json:"map_hash,omitempty"`
}

// Encode writes one bounded record at a time, avoiding a JSON buffer for the
// entire operation. The caller owns changes and must keep them immutable.
func Encode(ctx context.Context, w io.Writer, h Header, changes []model.TileChange) error {
	if h.Count != int64(len(changes)) {
		return fmt.Errorf("invalid transaction count")
	}
	index := 0
	return encodeRecords(ctx, w, h, func() (model.TileChange, error) {
		if index == len(changes) {
			return model.TileChange{}, io.EOF
		}
		change := changes[index]
		index++
		return change, nil
	})
}

func encodeRecords(ctx context.Context, w io.Writer, h Header, next func() (model.TileChange, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.Version != Version || h.Count < 1 || h.Count > model.MaxMapCells {
		return fmt.Errorf("invalid transaction version or count")
	}
	if len(h.Operation.Changes) != 0 {
		return fmt.Errorf("transaction header contains inline changes")
	}
	op := h.Operation
	for _, value := range []string{h.Kind, h.SessionID, h.MessageID, h.MapHash, string(op.DocumentID), string(op.ActorID), string(op.OperationID), op.EnvironmentHash, op.BaseMapHash, string(op.Kind)} {
		if !utf8.ValidString(value) {
			return fmt.Errorf("transaction metadata contains invalid UTF-8")
		}
	}
	if op.InverseOf != nil && !utf8.ValidString(string(*op.InverseOf)) {
		return fmt.Errorf("transaction inverse identifier contains invalid UTF-8")
	}
	header, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if len(header)+1 > MaxHeaderBytes {
		return fmt.Errorf("transaction metadata exceeds %d bytes", MaxHeaderBytes)
	}
	if _, err := w.Write(append(header, '\n')); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	if err := ctx.Err(); err != nil {
		return err
	}
	for index := int64(0); index < h.Count; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		change, err := next()
		if err != nil {
			return fmt.Errorf("read transaction record %d: %w", index, err)
		}
		if !validTileText(change.Before) || !validTileText(change.After) {
			return fmt.Errorf("transaction tile contains invalid UTF-8")
		}
		if err := encoder.Encode(change); err != nil {
			return err
		}
	}
	if _, err := next(); err != io.EOF {
		return fmt.Errorf("transaction has excess records or invalid ending: %v", err)
	}
	return nil
}

func validTileText(tile model.TileState) bool {
	for _, prefab := range tile.Prefabs {
		if !utf8.ValidString(string(prefab.StableID)) || !utf8.ValidString(prefab.Path) {
			return false
		}
		for name, value := range prefab.Vars {
			if !utf8.ValidString(name) || !utf8.ValidString(value) {
				return false
			}
		}
	}
	return true
}

type Reader struct {
	Header    Header
	ctx       context.Context
	decoder   *json.Decoder
	remaining int64
	ended     bool
}

func OpenReader(ctx context.Context, r io.Reader) (*Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer := bufio.NewReaderSize(&utf8Reader{source: r}, MaxHeaderBytes)
	header, err := buffer.ReadSlice('\n')
	if err != nil {
		return nil, fmt.Errorf("read bounded transaction header: %w", err)
	}
	metadata := json.NewDecoder(bytes.NewReader(header))
	metadata.DisallowUnknownFields()
	var h Header
	if err := metadata.Decode(&h); err != nil {
		return nil, fmt.Errorf("read transaction header: %w", err)
	}
	var extra any
	if err := metadata.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("transaction header has trailing data")
	}
	if h.Version != Version || h.Count < 1 || h.Count > model.MaxMapCells || len(h.Operation.Changes) != 0 {
		return nil, fmt.Errorf("invalid transaction header")
	}
	decoder := json.NewDecoder(buffer)
	decoder.DisallowUnknownFields()
	return &Reader{Header: h, ctx: ctx, decoder: decoder, remaining: h.Count}, nil
}

func (r *Reader) Next() (model.TileChange, error) {
	if err := r.ctx.Err(); err != nil {
		return model.TileChange{}, err
	}
	if r.remaining == 0 {
		if !r.ended {
			var extra any
			if err := r.decoder.Decode(&extra); err != io.EOF {
				return model.TileChange{}, fmt.Errorf("transaction has trailing data: %v", err)
			}
			r.ended = true
		}
		return model.TileChange{}, io.EOF
	}
	var change model.TileChange
	if err := r.decoder.Decode(&change); err != nil {
		return change, fmt.Errorf("read transaction change: %w", err)
	}
	r.remaining--
	return change, nil
}

// Materialize bridges a resource-admitted body into the existing atomic engine.
// It grows from actual records, never allocating a hostile declared count first.
func (r *Reader) Materialize() (model.Operation, error) {
	op := r.Header.Operation
	for {
		change, err := r.Next()
		if err == io.EOF {
			return op, nil
		}
		if err != nil {
			return model.Operation{}, err
		}
		op.Changes = append(op.Changes, change)
	}
}
