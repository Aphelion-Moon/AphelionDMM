package transaction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestBodyRejectsUnboundedHeaderBeforeMaterializingInlineChanges(t *testing.T) {
	data := `{"version":2,"count":1,"operation":{"changes":[` + strings.Repeat(" ", 1<<20) + `]}}` + "\n"
	if _, err := OpenReader(context.Background(), strings.NewReader(data)); err == nil {
		t.Fatal("unbounded transaction metadata accepted")
	}
}

type shortReader struct {
	io.Reader
	size int
}

func (r shortReader) Read(p []byte) (int, error) { return r.Reader.Read(p[:min(len(p), r.size)]) }

func TestBodyUTF8AcrossChunksAndInvalidRawBytes(t *testing.T) {
	for _, size := range []int{1, 2, 3, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			changes := []model.TileChange{{After: model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/未知", Vars: map[string]string{"raw": "世界🙂"}}}}}}
			var encoded bytes.Buffer
			if err := Encode(context.Background(), &encoded, Header{Version: Version, Count: 1}, changes); err != nil {
				t.Fatal(err)
			}
			reader, err := OpenReader(context.Background(), shortReader{Reader: bytes.NewReader(encoded.Bytes()), size: size})
			if err != nil {
				t.Fatal(err)
			}
			operation, err := reader.Materialize()
			if err != nil || !operation.Changes[0].After.Equal(changes[0].After) {
				t.Fatalf("split UTF8: %v", err)
			}
			invalid := bytes.Replace(encoded.Bytes(), []byte("世界"), []byte{0xff}, 1)
			reader, err = OpenReader(context.Background(), shortReader{Reader: bytes.NewReader(invalid), size: size})
			if err == nil {
				_, err = reader.Materialize()
			}
			if err == nil {
				t.Fatal("invalid UTF8 silently replaced")
			}
		})
	}
}

func TestBodyRoundTripAndCancellation(t *testing.T) {
	changes := []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, After: model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/unknown", Vars: map[string]string{"raw": `list("a" = 12)`}}}}}}
	header := Header{Version: 2, Count: 1, Kind: "operation_submit", SessionID: "session", MessageID: "operation"}
	var output bytes.Buffer
	if err := Encode(context.Background(), &output, header, changes); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReader(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Next()
	if err != nil || !got.After.Equal(changes[0].After) {
		t.Fatalf("round trip: %#v %v", got, err)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Encode(ctx, io.Discard, header, changes); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}

func TestEncodeRejectsInvalidUTF8WithoutReplacingSource(t *testing.T) {
	for _, field := range []string{"header", "path", "key", "value"} {
		t.Run(field, func(t *testing.T) {
			h := Header{Version: Version, Count: 1}
			p := model.PrefabState{Path: "/obj/test", Vars: map[string]string{"raw": "value"}}
			invalid := string([]byte{0xff})
			switch field {
			case "header":
				h.SessionID = invalid
			case "path":
				p.Path = invalid
			case "key":
				p.Vars[invalid] = "value"
			case "value":
				p.Vars["raw"] = invalid
			}
			if err := Encode(context.Background(), io.Discard, h, []model.TileChange{{After: model.TileState{Prefabs: []model.PrefabState{p}}}}); err == nil {
				t.Fatal("invalid source text silently replaced")
			}
		})
	}
}

func TestPrepareAdmissionAndCancellationCleanup(t *testing.T) {
	changes := []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}}}
	for _, mode := range []string{"nil-budget", "exhausted", "cancelled", "valid"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			budget := NewBudget(4096)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "nil-budget" {
				budget = nil
			}
			if mode == "exhausted" {
				budget = NewBudget(1)
			}
			if mode == "cancelled" {
				cancel()
			}
			body, err := Prepare(ctx, dir, budget, Header{Version: Version, Count: 1}, changes)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("Prepare: %v", err)
			}
			if body != nil {
				_ = body.Close()
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatal("prepared body leaked")
			}
			if budget != nil && budget.Used() != 0 {
				t.Fatal("prepared admission leaked")
			}
		})
	}
}

func TestSpoolRejectsPartialReorderedAlteredAndTrailingData(t *testing.T) {
	data := []byte("verified transaction bytes")
	digest := sha256.Sum256(data)
	for _, mode := range []string{"partial", "reordered", "altered", "overflow", "valid"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			budget := NewBudget(1024)
			spool, err := NewSpool(dir, int64(len(data)), hex.EncodeToString(digest[:]), budget)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = spool.Close() }()
			chunk := append([]byte(nil), data...)
			index := uint64(0)
			switch mode {
			case "partial":
				chunk = chunk[:2]
			case "reordered":
				index = 1
			case "altered":
				chunk[0] = 'X'
			case "overflow":
				chunk = append(chunk, 'x')
			}
			err = spool.Append(context.Background(), index, chunk)
			if mode == "reordered" || mode == "overflow" {
				if err == nil {
					t.Fatal("invalid chunk accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				body, finishErr := spool.Finish()
				if mode != "valid" {
					if finishErr == nil {
						_ = body.Close()
						t.Fatal("invalid body accepted")
					}
				} else {
					if finishErr != nil {
						t.Fatal(finishErr)
					}
					r, err := body.Open()
					if err != nil {
						t.Fatal(err)
					}
					got, err := io.ReadAll(r)
					_ = r.Close()
					if err != nil || !bytes.Equal(got, data) {
						t.Fatal("body differs")
					}
					_ = body.Close()
				}
			}
			_ = spool.Close()
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatal("spool leaked file")
			}
			if budget.Used() != 0 {
				t.Fatal("spool leaked resource admission")
			}
		})
	}
}

func TestSpoolSharesByteAdmissionAcrossConnections(t *testing.T) {
	budget := NewBudget(10)
	sum := sha256.Sum256([]byte("123456"))
	digest := hex.EncodeToString(sum[:])
	first, err := NewSpool(t.TempDir(), 6, digest, budget)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := NewSpool(t.TempDir(), 6, digest, budget); err == nil {
		_ = other.Close()
		t.Fatal("aggregate quota bypassed")
	}
	_ = first.Close()
	second, err := NewSpool(t.TempDir(), 6, digest, budget)
	if err != nil {
		t.Fatal("quota not released")
	}
	_ = second.Close()
}

func TestBudgetReportsResourceRequirementAndIdempotentRelease(t *testing.T) {
	budget := NewBudget(100)
	release, err := budget.Reserve(70)
	if err != nil {
		t.Fatal(err)
	}
	_, err = budget.Reserve(50)
	var admission *AdmissionError
	if !errors.As(err, &admission) || admission.Required != 50 || admission.Available != 30 {
		t.Fatalf("missing actionable admission error: %v", err)
	}
	release()
	release()
	if budget.Used() != 0 {
		t.Fatal("reservation leaked or refunded twice")
	}
}
