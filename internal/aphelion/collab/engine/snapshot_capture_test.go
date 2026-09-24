package engine

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"testing"
)

func TestSnapshotCaptureRemainsAtRevisionAcrossLocalEdit(t *testing.T) {
	source := initialSnapshot()
	source.Tiles[0], source.Tiles[1] = source.Tiles[1], source.Tiles[0]
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	capture := doc.CaptureSnapshot()
	tiles := doc.CaptureTiles()
	if allocs := testing.AllocsPerRun(100, func() { _ = doc.CaptureSnapshot() }); allocs != 0 {
		t.Fatalf("capture allocated %v", allocs)
	}
	if estimated := capture.EstimatedBytes(); estimated <= 1<<20 {
		t.Fatalf("snapshot estimate = %d, want fixed allowance plus payload", estimated)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = capture.EstimatedBytes() }); allocs != 0 {
		t.Fatalf("snapshot estimate allocated %v", allocs)
	}
	first := source.Tiles[1]
	accepted, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: first.Coord, Before: first.State, After: model.TileState{}}}})
	if err != nil {
		t.Fatal(err)
	}
	old := capture.Snapshot()
	state, exists := tiles.Tile(first.Coord)
	if !exists || !state.Equal(first.State) {
		t.Fatal("sparse capture moved with authority")
	}
	state.Prefabs[0].Path = "/obj/mutated"
	state, _ = tiles.Tile(first.Coord)
	if state.Prefabs[0].Path == "/obj/mutated" {
		t.Fatal("sparse caller mutated capture")
	}
	if capture.DocumentID() != source.DocumentID || capture.Revision() != source.Revision || !old.Tiles[1].State.Equal(first.State) || accepted.Revision == old.Revision {
		t.Fatal("capture moved with authority")
	}
	old.Tiles[1].State.Prefabs[0].Path = "/obj/mutated"
	if capture.Snapshot().Tiles[1].State.Prefabs[0].Path == "/obj/mutated" {
		t.Fatal("caller mutated captured authority")
	}
}
