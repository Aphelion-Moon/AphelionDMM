package transaction

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestReframeStreamsRecordsAndRejectsTruncatedStoredBody(t *testing.T) {
	ctx := context.Background()
	h := Header{Version: 2, Count: 2, Kind: "accepted"}
	changes := []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}}, {Coord: model.Coord{X: 2, Y: 1, Z: 1}}}
	var source bytes.Buffer
	if err := Encode(ctx, &source, h, changes); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "truncated", "metadata", "quota"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			budget := NewBudget(1 << 20)
			input := source.Bytes()
			expected := h
			if mode == "truncated" {
				input = input[:len(input)-20]
			}
			if mode == "metadata" {
				expected.MapHash = "altered"
			}
			if mode == "quota" {
				budget = NewBudget(1)
			}
			body, err := Reframe(ctx, dir, budget, bytes.NewReader(input), expected, "session", "replay")
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				r, err := body.Open()
				if err != nil {
					t.Fatal(err)
				}
				reader, err := OpenReader(ctx, r)
				if err != nil {
					t.Fatal(err)
				}
				if reader.Header.Kind != "operation_accepted" || reader.Header.SessionID != "session" || reader.Header.Count != 2 {
					t.Fatal("wrong wire metadata")
				}
				for _, want := range changes {
					got, err := reader.Next()
					if err != nil || got.Coord != want.Coord {
						t.Fatalf("record lost: %v", err)
					}
				}
				if _, err := reader.Next(); err != io.EOF {
					t.Fatal(err)
				}
				_ = r.Close()
				if err := body.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				_ = body.Close()
				t.Fatal("invalid stored transaction accepted")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 || budget.Used() != 0 {
				t.Fatal("reframe leaked admission or spool")
			}
		})
	}
}
