package editor

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
)

func TestAcceptedTileCaptureDoesNotCopyWholeSource(t *testing.T) {
	for _, shared := range []bool{false, true} {
		var small float64
		for _, cells := range []int{100, 10000} {
			e := selectionEditor(t)
			snapshot := e.authoritative
			snapshot.MaxX, snapshot.MaxY = 100, cells/100
			snapshot.Tiles = authoritativeCopyFixture(cells).Tiles
			for i := range snapshot.Tiles {
				snapshot.Tiles[i].Coord = model.Coord{X: i%100 + 1, Y: i/100 + 1, Z: 1}
				snapshot.Tiles[i].State.Prefabs[0].StableID, _ = model.NewStableID()
			}
			if shared {
				network, err := client.NewNetworkExecutor(newEditorNetworkTransport(), snapshot, e.actorID, "export")
				if err != nil {
					t.Fatal(err)
				}
				e.executor, e.sessionOwned = network, true
			} else {
				document, err := engine.NewUnsharedDocument(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				local, err := executor.NewLocal(document, e.actorID)
				if err != nil {
					t.Fatal(err)
				}
				e.executor = local
			}
			e.authoritative = snapshot
			allocs := testing.AllocsPerRun(5, func() {
				capture, _, err := e.CaptureAcceptedTiles(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(capture.Header().Tiles) != 0 {
					t.Fatal("capture materialized whole source")
				}
				state, exists := capture.Tile(model.Coord{X: 1, Y: 1, Z: 1})
				if !exists || state.Prefabs[0].Vars["dir"] != "2" {
					t.Fatal("selected tile missing")
				}
			})
			if cells == 100 {
				small = allocs
			} else if allocs > small+4 {
				t.Fatalf("shared=%t capture grew with whole map: %.0f -> %.0f allocations", shared, small, allocs)
			}
		}
	}
}

func TestAcceptedTileCapturePinsSessionAndRejectsUndisplayedHead(t *testing.T) {
	e, network, document, _ := deltaEditor(t)
	capture, version, err := e.CaptureAcceptedTiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	coord := model.Coord{X: 1, Y: 1, Z: 1}
	acceptDelta(t, network, document, coord, "/area/changed")
	if e.SaveCaptureReady(version) {
		t.Fatal("undisplayed executor revision remained exportable")
	}
	if _, _, err := e.CaptureAcceptedTiles(context.Background()); err == nil {
		t.Fatal("captured undisplayed authority")
	}
	e.ProcessCollaborationUpdates()
	state, exists := capture.Tile(coord)
	if !exists || state.Prefabs[0].Path != "/area/foo" {
		t.Fatal("worker capture changed with the displayed source")
	}
	state.Prefabs[0].Path = "/area/caller"
	again, _ := capture.Tile(coord)
	if again.Prefabs[0].Path != "/area/foo" {
		t.Fatal("caller changed captured tile")
	}
}
