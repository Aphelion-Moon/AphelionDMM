package editing

import (
	"context"
	"reflect"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"testing"
)

func TestPayloadMaskNeverWritesBoundingHoles(t *testing.T) {
	m := rotationMap()
	source := []dmmap.Tile{m.Tiles[0].Copy(), m.Tiles[len(m.Tiles)-1].Copy()}
	p, err := CompilePlacementPayload(context.Background(), source, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	lookups := 0
	changes, err := p.BuildPlacementChanges(context.Background(), util.Point{X: 10, Y: 20, Z: 1}, PastePolicy{Mode: ReplaceIncludingBlanks, Channels: AllChannels}, func(string) bool { return true }, func(model.Coord) (model.TileState, bool) { lookups++; return model.TileState{}, true })
	if err != nil || lookups != 2 || len(changes) != 2 {
		t.Fatalf("sparse footprint expanded: lookups=%d changes=%d err=%v", lookups, len(changes), err)
	}
}

func TestNormalizedOrientationRestoresOriginalInheritedValues(t *testing.T) {
	m := rotationMap()
	source := []dmmap.Tile{m.Tiles[0].Copy(), m.Tiles[1].Copy()}
	orientation := IdentityOrientation()
	for i := 0; i < 400; i++ {
		orientation = orientation.Transform(PlacementRotateRight)
	}
	result, err := orientation.Prepare(context.Background(), source)
	if err != nil || !reflect.DeepEqual(result, source) {
		t.Fatal("four-turn groups accumulated overrides", err)
	}
	if orientation.Transform(PlacementMirrorHorizontal).Transform(PlacementMirrorHorizontal) != orientation {
		t.Fatal("reflection failed to cancel")
	}
	if orientation.Transform(PlacementRotateLeft).Transform(PlacementRotateRight) != orientation {
		t.Fatal("opposite turns failed to cancel")
	}
}
