package editing

import (
	"context"
	"fmt"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
)

func TestMovePayloadRotationKeepsSourceAndSparseDestinationSeparate(t *testing.T) {
	family, parents := directionalFixture()
	selection, _ := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 1, Y: 3, Z: 1}})
	base := map[model.Coord]model.TileState{}
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 4; x++ {
			c := model.Coord{X: x, Y: y, Z: 1}
			base[c] = model.TileState{Prefabs: []model.PrefabState{{Path: family + "/north", StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", y*4+x))}}}
		}
	}
	c := model.Coord{X: 1, Y: 1, Z: 1}
	base[c] = model.TileState{Prefabs: append(base[c].Prefabs, model.PrefabState{Path: "/obj/hidden", StableID: "hidden"})}
	lookup := func(c model.Coord) (model.TileState, bool) { s, ok := base[c]; return s, ok }
	visible := func(path string) bool { return path != "/obj/hidden" }
	original, err := CompileMovePayload(context.Background(), selection, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := original.Rotated(context.Background(), 1, parents)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotated.ValidateSource(visible, lookup); err != nil {
		t.Fatal("rotation changed source validation", err)
	}
	changes, err := rotated.BuildMoveChanges(context.Background(), util.Point{}, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	after := changedStates(changes)
	for src, dst := range map[model.Coord]model.Coord{
		{X: 1, Y: 1, Z: 1}: {X: 1, Y: 2, Z: 1},
		{X: 2, Y: 1, Z: 1}: {X: 1, Y: 1, Z: 1},
		{X: 1, Y: 3, Z: 1}: {X: 3, Y: 2, Z: 1},
	} {
		found := false
		for _, p := range after[dst].Prefabs {
			if p.StableID == base[src].Prefabs[0].StableID {
				found = true
				if p.Path != family+"/east" {
					t.Fatal("rotated move lost variant path")
				}
			}
		}
		if !found {
			t.Fatalf("source %v missing from destination %v", src, dst)
		}
	}
	if after[model.Coord{X: 1, Y: 1, Z: 1}].Prefabs[0].Path != "/obj/hidden" {
		t.Fatal("hidden overlap content changed")
	}
	if _, changed := after[model.Coord{X: 2, Y: 2, Z: 1}]; changed {
		t.Fatal("rotation wrote a sparse hole")
	}
	if !rotated.DestinationContains(util.Point{X: 3, Y: 2, Z: 1}, util.Point{}) ||
		rotated.Suppresses(util.Point{X: 2, Y: 2, Z: 1}, util.Point{}) {
		t.Fatal("rotation suppression used source shape as destination")
	}
	if got, err := original.Rotated(context.Background(), 0, parents); err != nil || got != original {
		t.Fatal("identity rotation did not retain original")
	}
	if err := original.ValidateSource(visible, lookup); err != nil {
		t.Fatal("original payload changed", err)
	}
	base[c] = model.TileState{}
	if err := rotated.ValidateSource(visible, lookup); err == nil {
		t.Fatal("rotated payload accepted changed source")
	}
}

func TestSelectionMoveRotationValidatesBoundsAndRestoresPose(t *testing.T) {
	source := RectangleSelection(util.Bounds{X1: 2, Y1: 1, X2: 3, Y2: 3}, 1)
	move, err := NewSelectionMove(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := move.Rotate(true, 3, 3, 1); err == nil || move.Turns() != 0 || move.Bounds() != source.Bounds() {
		t.Fatal("invalid turn changed pose")
	}
	if _, err := move.Rotate(true, 4, 3, 1); err != nil {
		t.Fatal(err)
	}
	if move.Bounds() != (util.Bounds{X1: 2, Y1: 1, X2: 4, Y2: 2}) {
		t.Fatal("rotation changed bottom-left anchor")
	}
	if _, _, err := move.Update(util.Point{X: 1}, 4, 3, 1); err == nil {
		t.Fatal("translation ignored rotated width")
	}
	for range 3 {
		if _, err := move.Rotate(true, 4, 3, 1); err != nil {
			t.Fatal(err)
		}
	}
	if move.Turns() != 0 || move.Bounds() != source.Bounds() || move.DestinationSelection().Bounds() != source.Bounds() {
		t.Fatal("four turns drifted")
	}
	move.Finish()
	if _, err := move.Rotate(true, 4, 3, 1); err == nil {
		t.Fatal("closed pose rotated")
	}
}
