package client

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestVisibleTilesKeepsPendingBatchAtomicAndDetached(t *testing.T) {
	snapshot := projectionSnapshot(t)
	for x := 1; x <= 2; x++ {
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{StableID: mustStableID(t), Path: "/turf/original", Vars: map[string]string{"value": "1"}}}}})
	}
	operation := projectionOperation(t, snapshot, 1)
	second := projectionOperation(t, snapshot, 2)
	operation.Changes = append(operation.Changes, second.Changes...)
	coord := operation.Changes[0].Coord
	contains := func(c model.Coord) bool { return c == coord }
	for _, pending := range []bool{false, true} {
		for _, conflict := range []bool{false, true} {
			projection := NewProjection(snapshot)
			if pending {
				projection.Pending = []model.Operation{model.CloneOperation(operation)}
			}
			if conflict {
				projection.Acknowledged.Tiles[1].State.Prefabs[0].Path = "/obj/remote"
			}
			capture := ProjectionCapture{projection: projection}
			full, err := capture.VisibleSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			tiles, err := capture.VisibleTiles(contains)
			if err != nil {
				t.Fatal(err)
			}
			for _, tile := range full.Tiles {
				if contains(tile.Coord) && !tile.State.Equal(tiles[tile.Coord]) {
					t.Fatal("sparse capture differs from complete pending reconciliation")
				}
			}
			if len(tiles) != 1 {
				t.Fatal("capture included unrelated tiles")
			}
			tiles[coord].Prefabs[0].Path = "/obj/caller"
			after, err := capture.VisibleSnapshot()
			if err != nil || !reflect.DeepEqual(after, full) {
				t.Fatal("caller mutation changed pinned capture", err)
			}
		}
	}
}

func BenchmarkMoveFootprintCapture(b *testing.B) {
	network, _ := wholeLevelNetwork(b)
	capture, err := network.CaptureProjection(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	contains := func(c model.Coord) bool { return c.X <= 96 && c.Y <= 96 }
	b.Run("whole_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			snapshot, err := capture.VisibleSnapshot()
			if err != nil {
				b.Fatal(err)
			}
			tiles := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
			for _, tile := range snapshot.Tiles {
				tiles[tile.Coord] = tile.State
			}
			if len(tiles) != 65536 {
				b.Fatal("lost tiles")
			}
		}
	})
	b.Run("selected_footprint", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			tiles, err := capture.VisibleTiles(contains)
			if err != nil || len(tiles) != 9216 {
				b.Fatal("lost selected tiles", err)
			}
		}
	})
}
