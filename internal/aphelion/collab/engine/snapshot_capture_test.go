package engine

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"testing"
)

func TestSnapshotCaptureRemainsAtRevisionAcrossLocalEdit(t *testing.T) {
	source := initialSnapshot()
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	capture := doc.CaptureSnapshot()
	if allocs := testing.AllocsPerRun(100, func() { _ = doc.CaptureSnapshot() }); allocs != 0 {
		t.Fatalf("capture allocated %v", allocs)
	}
	first := source.Tiles[0]
	accepted, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: first.Coord, Before: first.State, After: model.TileState{}}}})
	if err != nil {
		t.Fatal(err)
	}
	old := capture.Snapshot()
	if capture.DocumentID() != source.DocumentID || capture.Revision() != source.Revision || !old.Tiles[0].State.Equal(first.State) || accepted.Revision == old.Revision {
		t.Fatal("capture moved with authority")
	}
	old.Tiles[0].State.Prefabs[0].Path = "/obj/mutated"
	if capture.Snapshot().Tiles[0].State.Prefabs[0].Path == "/obj/mutated" {
		t.Fatal("caller mutated captured authority")
	}
}
