package executor

import (
	"context"
	"errors"
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

func TestLocalMetadataTracksUnsharedEditsAndOwnedSnapshot(t *testing.T) {
	document, err := engine.NewUnsharedDocument(testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	local, err := NewLocal(document, testActorID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before, err := local.Metadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(10, func() { _, _ = local.Metadata(ctx) }); allocs != 0 {
		t.Fatalf("cached metadata allocated %.0f times", allocs)
	}
	version, err := local.LocalVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := local.ApplyLocal(ctx, engine.LocalRequest{Version: version, Changes: []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: testTile()}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, after, err := local.SnapshotWithMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := snapshot.Hash()
	if err != nil || hash != after.MapHash || after.MapHash == before.MapHash || after.Revision != accepted.Revision {
		t.Fatalf("metadata missed local edit or crossed snapshot revision: %v", err)
	}
	snapshot.Tiles[0].State = testTile()
	current, err := local.Metadata(ctx)
	if err != nil || current != after {
		t.Fatal("caller snapshot mutation changed authority")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := local.Metadata(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := local.SnapshotWithMetadata(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
