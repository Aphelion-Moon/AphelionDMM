package engine

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestDocumentMetadataTracksLocalHashInvalidation(t *testing.T) {
	document, err := NewUnsharedDocument(initialSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	before, err := document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	state := tileOneBefore()
	state.Prefabs[0].Vars["dir"] = "4"
	_, err = document.ApplyLocal(context.Background(), LocalRequest{Version: document.LocalVersion(), Changes: []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: tileOneBefore(), After: state}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := document.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := document.Snapshot().Hash()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || after.MapHash == before.MapHash || after.MapHash != hash || document.Revision() != after.Revision {
		t.Fatalf("metadata did not advance coherently: before=%+v after=%+v hash=%s", before, after, hash)
	}
}

func TestDocumentMetadataDoesNotCopyTiles(t *testing.T) {
	document, err := NewDocument(copyFixture(10000, 1))
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(10, func() {
		if _, err := document.Metadata(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("metadata read allocated %.0f times", allocs)
	}
}
