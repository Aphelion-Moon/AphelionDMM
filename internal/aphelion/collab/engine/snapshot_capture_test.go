package engine

import (
	"context"
	"fmt"
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

func TestTileCaptureRemainsSparseDuringConcurrentEdits(t *testing.T) {
	source := initialSnapshot()
	source.MaxX = 3
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	capture := doc.CaptureTiles()
	first := source.Tiles[0]
	destination := model.Coord{X: 3, Y: 1, Z: 1}
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(ready)
		for range 1000 {
			state, exists := capture.Tile(first.Coord)
			if !exists || !state.Equal(first.State) {
				done <- fmt.Errorf("capture changed an existing tile")
				return
			}
			if _, exists := capture.Tile(destination); exists {
				done <- fmt.Errorf("capture gained a newly inserted tile")
				return
			}
			if _, exists := capture.EstimatedTileBytes(destination); exists {
				done <- fmt.Errorf("capture gained a newly inserted tile estimate")
				return
			}
		}
		done <- nil
	}()
	<-ready
	// Both changed entries already exist on the inverse; only the forward edit
	// introduces a coordinate. The pinned capture must remain sparse throughout.
	forward := []model.TileChange{{Coord: first.Coord, Before: first.State}, {Coord: destination, After: first.State}}
	backward := []model.TileChange{{Coord: first.Coord, After: first.State}, {Coord: destination, Before: first.State}}
	for _, changes := range [][]model.TileChange{forward, backward} {
		if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: changes}); err != nil {
			t.Error(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state, exists := capture.Tile(first.Coord); !exists || !state.Equal(first.State) {
		t.Fatal("later edit changed captured payload")
	}
	if _, exists := capture.Tile(destination); exists {
		t.Fatal("later edit changed captured coordinate membership")
	}
}

func capturedLocalEditFixture(tb testing.TB, cells int) (*Document, model.TileChange) {
	tb.Helper()
	source := copyFixture(cells, 1)
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		tb.Fatal(err)
	}
	tile := source.Tiles[0]
	after := model.CloneTileState(tile.State)
	after.Prefabs[0].Vars["dir"] = "4"
	return doc, model.TileChange{Coord: tile.Coord, Before: tile.State, After: after}
}

func applyCapturedLocalEdit(tb testing.TB, doc *Document, change *model.TileChange) {
	tb.Helper()
	capture := doc.CaptureTiles()
	if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{*change}}); err != nil {
		tb.Fatal(err)
	}
	if state, exists := capture.Tile(change.Coord); !exists || !state.Equal(change.Before) {
		tb.Fatal("edit changed the pinned tile capture")
	}
	change.Before, change.After = change.After, change.Before
}

func TestCapturedLocalEditAllocationsDoNotScaleWithUntouchedIndexes(t *testing.T) {
	var small float64
	for _, cells := range []int{100, 10000} {
		doc, change := capturedLocalEditFixture(t, cells)
		allocs := testing.AllocsPerRun(10, func() { applyCapturedLocalEdit(t, doc, &change) })
		t.Logf("cells=%d captured edit allocations=%g", cells, allocs)
		if small == 0 {
			small = allocs
		} else if allocs > small+16 {
			t.Fatalf("captured sparse edit copies untouched indexes: small=%g large=%g", small, allocs)
		}
	}
}

func BenchmarkCapturedLocalEdit(b *testing.B) {
	for _, cells := range []int{100, 10000} {
		b.Run(fmt.Sprint(cells), func(b *testing.B) {
			doc, change := capturedLocalEditFixture(b, cells)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				applyCapturedLocalEdit(b, doc, &change)
			}
		})
	}
}
