package bulktransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/transaction"
)

type frameReader struct{ frames [][]byte }

func (r *frameReader) Read(context.Context) (websocket.MessageType, []byte, error) {
	frame := r.frames[0]
	r.frames = r.frames[1:]
	return websocket.MessageBinary, frame, nil
}

func TestWorkingAdmissionRetainedUntilConsumerFinishes(t *testing.T) {
	var body bytes.Buffer
	if err := transaction.Encode(context.Background(), &body, transaction.Header{Version: 2, Count: 1}, []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}}}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body.Bytes())
	d, _ := json.Marshal(descriptor{Size: int64(body.Len()), Digest: fmt.Sprintf("%x", digest)})
	first := append([]byte{begin}, d...)
	frame := make([]byte, 9)
	frame[0] = chunk
	binary.BigEndian.PutUint64(frame[1:], 0)
	frame = append(frame, body.Bytes()...)
	read := func(c *Codec) (func(), error) {
		_, _, release, err := c.ReadAdmitted(context.Background(), &frameReader{frames: [][]byte{frame, {end}}}, first, time.Second)
		return release, err
	}
	c := NewWithWorkingBudget(t.TempDir(), int64(body.Len()*2), int64(body.Len())*WorkingSetFactor)
	release, err := read(c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Used() != 0 {
		t.Fatal("disk spool not released after decoding")
	}
	if other, err := read(c); err == nil {
		other()
		t.Fatal("concurrent decoded edit escaped aggregate memory admission")
	}
	release()
	release()
	if c.working.Used() != 0 {
		t.Fatal("consumer lease leaked")
	}
	release, err = read(c)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestWholeLevelTransferAndInterruptedUploadCleanup(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole-level", true: "interrupted"}[interrupted], func(t *testing.T) {
			codec := New(t.TempDir(), 32<<20)
			result := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					result <- err
					return
				}
				defer func() { _ = c.CloseNow() }()
				c.SetReadLimit(ChunkBytes + 9)
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				_, first, err := c.Read(ctx)
				if err != nil {
					result <- err
					return
				}
				h, op, err := codec.Read(ctx, c, first, time.Second)
				if err == nil && (h.Count != 65536 || len(op.Changes) != 65536 || op.Changes[65535].Coord.X != 256) {
					t.Error("body lost changes")
				}
				result <- err
			}))
			defer server.Close()
			c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.CloseNow() }()
			if interrupted {
				if err := c.Write(context.Background(), websocket.MessageBinary, []byte(``+"\x01"+`{"size":100,"digest":"`+strings.Repeat("0", 64)+`"}`)); err != nil {
					t.Fatal(err)
				}
				_ = c.CloseNow()
			} else {
				changes := make([]model.TileChange, 65536)
				for i := range changes {
					changes[i] = model.TileChange{Coord: model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}}
				}
				err = codec.Write(context.Background(), c, transaction.Header{Version: 2, Count: 65536, Kind: "operation_submit"}, changes, time.Second)
				if err != nil {
					t.Fatal(err)
				}
			}
			err = <-result
			if (err != nil) != interrupted {
				t.Fatalf("transfer: %v", err)
			}
			if codec.Used() != 0 {
				t.Fatalf("admission leaked: %d", codec.Used())
			}
		})
	}
}
