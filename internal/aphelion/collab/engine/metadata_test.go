package engine

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestDocumentSnapshotComparisonPreservesCloneSemantics(t *testing.T) {
	empty := initialSnapshot()
	empty.Tiles = nil
	for _, fixture := range []model.Snapshot{initialSnapshot(), empty} {
		document, err := NewDocument(fixture)
		if err != nil {
			t.Fatal(err)
		}
		variants := []model.Snapshot{fixture, model.CloneSnapshot(fixture)}
		changed := model.CloneSnapshot(fixture)
		changed.Revision++
		variants = append(variants, changed)
		if len(fixture.Tiles) != 0 {
			changed = model.CloneSnapshot(fixture)
			changed.Tiles[0].State.Prefabs[0].Vars["dir"] = "99"
			variants = append(variants, changed)
		}
		for index, candidate := range variants {
			want := index < 2
			if got := document.EqualSnapshot(candidate); got != want {
				t.Fatalf("snapshot equality=%v, want %v", got, want)
			}
		}
	}
}

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
