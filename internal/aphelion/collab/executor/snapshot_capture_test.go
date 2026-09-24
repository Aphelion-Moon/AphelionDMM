package executor

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"sync"
	"testing"
)

func TestCapturedSnapshotCanMaterializeDuringLaterMutation(t *testing.T) {
	local := newTestLocal(t)
	ctx := context.Background()
	base, err := local.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := local.CaptureSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := local.CaptureTiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for range 30 {
			state, ok := tiles.Tile(base.Tiles[0].Coord)
			if !ok || !state.Equal(base.Tiles[0].State) {
				t.Error("sparse read crossed revision")
			}
			snapshot := capture.Snapshot()
			if snapshot.Revision != base.Revision || !snapshot.Tiles[0].State.Equal(base.Tiles[0].State) {
				t.Error("read crossed revision")
			}
		}
	}()
	operation := testOperation(t, base, "01890f3e-7b5c-7abc-8def-0123456789bb", model.Coord{X: 1, Y: 1, Z: 1}, testTile(), model.TileState{})
	if _, err := local.Execute(ctx, operation); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
}
