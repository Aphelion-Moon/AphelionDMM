package editor

import (
	"context"
	"reflect"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestSelectionFillAndReplaceKeepHolesAndUndoExactly(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "fill", true: "replace"}[replace], func(t *testing.T) {
			e := selectionEditor(t)
			for x := 2; x <= 3; x++ {
				tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
				tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
				e.dmm.Tiles = append(e.dmm.Tiles, tile)
			}
			e.dmm.MaxX = 3
			e.initializeCollaboration()
			e.pMap.Snapshot().Sync()
			a := e.app.(*editorTestApp)
			a.runLater = make(chan func(), 8)
			before, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			mask, _ := editing.MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
			prefab := dmmprefab.New(0, "/obj/new", (&dmvars.MutableVariables{}).ToImmutable())
			if err := e.FillSelection(mask, prefab, replace); err != nil {
				t.Fatal(err)
			}
			for e.localWork != nil {
				a.runScheduled(t)
			}
			after, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Tiles[1], after.Tiles[1]) {
				t.Fatal("fill changed mask hole")
			}
			for _, index := range []int{0, 2} {
				want := len(before.Tiles[index].State.Prefabs)
				if !replace {
					want++
				}
				if len(after.Tiles[index].State.Prefabs) != want {
					t.Fatal("wrong collection composition")
				}
			}
			a.commands.UndoV("test")
			for e.localWork != nil {
				a.runScheduled(t)
			}
			undone, err := e.SaveSnapshot(context.Background())
			if err != nil || !reflect.DeepEqual(undone.Tiles, before.Tiles) {
				t.Fatal("fill undo differs", err)
			}
		})
	}
}
func TestSelectionFillBudgetRefusesBeforeMutation(t *testing.T) {
	e := selectionEditor(t)
	e.workBudget = resources.NewFixedBudget(1)
	before := e.dmm.Copy()
	s := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	if err := e.FillSelection(s, e.dmm.Tiles[0].Instances()[2].Prefab(), false); err == nil {
		t.Fatal("admitted insufficient budget")
	}
	if !reflect.DeepEqual(before, e.dmm.Copy()) || len(e.pendingChanges) != 0 {
		t.Fatal("failed admission mutated map")
	}
	mask, _ := editing.MaskSelection(s.Coordinates())
	if _, err := e.RotateSelectionMask(mask, true); err == nil {
		t.Fatal("rotation bypassed budget")
	}
	if !reflect.DeepEqual(before, e.dmm.Copy()) || len(e.pendingChanges) != 0 {
		t.Fatal("rotation admission mutated map")
	}
}
