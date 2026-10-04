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

func TestReleasedScopedTileCaptureAvoidsTableCopy(t *testing.T) {
	doc, change := capturedLocalEditFixture(t, 10000)
	capture := doc.CaptureScopedTiles()
	if capture.DocumentID() != doc.snapshot.DocumentID || capture.Revision() != doc.snapshot.Revision {
		t.Fatal("scoped capture lost its revision")
	}
	state, ok := capture.Tile(change.Coord)
	if !ok || !state.Equal(change.Before) {
		t.Fatal("scoped source differs")
	}
	state.Prefabs[0].Vars["dir"] = "999"
	state, _ = capture.Tile(change.Coord)
	if !state.Equal(change.Before) {
		t.Fatal("caller mutated scoped source")
	}
	alias := capture
	capture.Release()
	alias.Release()
	if _, ok := capture.Tile(change.Coord); ok {
		t.Fatal("released source remains readable")
	}
	if _, ok := capture.EstimatedTileBytes(change.Coord); ok {
		t.Fatal("released source remains retained")
	}
	before := &doc.snapshot.Tiles[0]
	if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{change}}); err != nil {
		t.Fatal(err)
	}
	if &doc.snapshot.Tiles[0] != before {
		t.Fatal("released selection source forced a whole tile-table copy")
	}
}

func TestScopedTileCapturePinsIndependentTables(t *testing.T) {
	doc, change := capturedLocalEditFixture(t, 10)
	apply := func() {
		t.Helper()
		if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{change}}); err != nil {
			t.Fatal(err)
		}
		change.Before, change.After = change.After, change.Before
	}
	first, second := doc.CaptureScopedTiles(), doc.CaptureScopedTiles()
	first.Release()
	before := &doc.snapshot.Tiles[0]
	apply()
	if &doc.snapshot.Tiles[0] == before {
		t.Fatal("one release unpinned another active reader")
	}
	state, ok := second.Tile(change.Coord)
	if !ok || !state.Equal(change.After) {
		t.Fatal("old reader followed the edit")
	}
	current := doc.CaptureScopedTiles()
	second.Release()
	before = &doc.snapshot.Tiles[0]
	apply()
	if &doc.snapshot.Tiles[0] == before {
		t.Fatal("old table release unpinned a current reader")
	}
	state, ok = current.Tile(change.Coord)
	if !ok || !state.Equal(change.After) {
		t.Fatal("current reader followed the next edit")
	}
	current.Release()
	before = &doc.snapshot.Tiles[0]
	apply()
	if &doc.snapshot.Tiles[0] != before {
		t.Fatal("detached readers forced another tile-table copy")
	}
}

func TestScopedReleasePreservesPermanentPins(t *testing.T) {
	for _, kind := range []string{"snapshot", "branch"} {
		t.Run(kind, func(t *testing.T) {
			doc, change := capturedLocalEditFixture(t, 10)
			capture := doc.CaptureScopedTiles()
			var retained func() model.Snapshot
			if kind == "snapshot" {
				saved := doc.CaptureSnapshot()
				retained = saved.Snapshot
			} else {
				branch := doc.Clone()
				retained = branch.Snapshot
			}
			capture.Release()
			before := &doc.snapshot.Tiles[0]
			if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{change}}); err != nil {
				t.Fatal(err)
			}
			if &doc.snapshot.Tiles[0] == before {
				t.Fatal("scoped release removed a permanent pin")
			}
			if !retained().Tiles[0].State.Equal(change.Before) {
				t.Fatal("permanent capture changed")
			}
		})
	}
}

func TestScopedTileCaptureKeepsIndexAcrossTableReplacementAndInsertion(t *testing.T) {
	source := initialSnapshot()
	source.MaxX = 3
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	capture := doc.CaptureScopedTiles()
	defer capture.Release()
	first := source.Tiles[0]
	destination := model.Coord{X: 3, Y: 1, Z: 1}
	for _, change := range []model.TileChange{
		{Coord: first.Coord, Before: first.State},
		{Coord: destination, After: first.State},
	} {
		if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{change}}); err != nil {
			t.Fatal(err)
		}
	}
	state, ok := capture.Tile(first.Coord)
	if !ok || !state.Equal(first.State) {
		t.Fatal("scoped capture lost its original tile")
	}
	if _, ok := capture.Tile(destination); ok {
		t.Fatal("scoped capture gained a later coordinate")
	}
	if _, ok := capture.EstimatedTileBytes(destination); ok {
		t.Fatal("scoped estimate gained a later coordinate")
	}
}

func TestScopedTileCaptureReadDuringRelease(t *testing.T) {
	doc, change := capturedLocalEditFixture(t, 10)
	capture := doc.CaptureScopedTiles()
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(ready)
		for range 1000 {
			if state, ok := capture.Tile(change.Coord); ok && !state.Equal(change.Before) {
				done <- fmt.Errorf("reader saw mutable authority")
				return
			}
			capture.EstimatedTileBytes(change.Coord)
			capture.DocumentID()
			capture.Revision()
		}
		done <- nil
	}()
	<-ready
	capture.Release()
	before := &doc.snapshot.Tiles[0]
	if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{change}}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if &doc.snapshot.Tiles[0] != before {
		t.Fatal("released concurrent reader forced a copy")
	}
}
