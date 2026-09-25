package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestShapeEraseModesPreserveDefaultsAndFenceRevision(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "one topmost target"
		if all {
			name = "all eligible targets"
		}
		t.Run(name, func(t *testing.T) {
			e := selectionEditor(t)
			tile := e.dmm.Tiles[0]
			oldArea, oldTurf := dmmap.BaseArea, dmmap.BaseTurf
			dmmap.BaseArea = tile.Instances()[0].Prefab()
			dmmap.BaseTurf = tile.Instances()[1].Prefab()
			t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = oldArea, oldTurf })
			tile.InstancesAdd(dmmprefab.New(dmmprefab.IdNone, "/obj/other", (&dmvars.MutableVariables{}).ToImmutable()))
			e.initializeCollaboration()

			before, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			areaID := before.Tiles[0].State.Prefabs[0].StableID
			turfID := before.Tiles[0].State.Prefabs[1].StableID
			topID := before.Tiles[0].State.Prefabs[2].StableID
			otherID := before.Tiles[0].State.Prefabs[3].StableID
			selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
			filter := e.app.PathsFilter().Copy()
			fence, err := e.BeginShapeDelete(1)
			if err != nil {
				t.Fatal(err)
			}
			stale := fence
			stale.ViewGeneration++
			display := e.dmm.Copy()
			targets := editing.ShapeDeleteTargets{string(topID): {X: 1, Y: 1, Z: 1}}
			if err := e.EraseShape(selection, all, filter, stale, targets); err == nil {
				t.Fatal("stale shape-delete assessment was accepted")
			}
			if !reflect.DeepEqual(display, e.dmm.Copy()) {
				t.Fatal("stale assessment changed the display")
			}

			fence, err = e.BeginShapeDelete(1)
			if err != nil {
				t.Fatal(err)
			}
			if all {
				targets = nil
			}
			if err := e.EraseShape(selection, all, filter, fence, targets); err != nil {
				t.Fatal(err)
			}
			after, err := e.SaveSnapshot(context.Background())
			if err != nil || after.Revision != before.Revision+1 {
				t.Fatalf("shape erase revision = %d, want %d: %v", after.Revision, before.Revision+1, err)
			}
			hasID := func(id model.StableID) bool {
				for _, prefab := range after.Tiles[0].State.Prefabs {
					if prefab.StableID == id {
						return true
					}
				}
				return false
			}
			if !hasID(areaID) || !hasID(turfID) {
				t.Fatal("shape erase changed a required default identity")
			}
			if all {
				if hasID(topID) || hasID(otherID) {
					t.Fatal("Alt shape erase retained an eligible object")
				}
			} else if hasID(topID) || !hasID(otherID) {
				t.Fatal("normal shape erase did not remove only the resolved topmost target")
			}
			e.app.CommandStorage().UndoV("test")
			undone, err := e.SaveSnapshot(context.Background())
			if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
				t.Fatal("shape erase undo did not restore exact identities", err)
			}
		})
	}
}

func TestShapeEraseRetainsTargetAdmissionThroughBulkCompletion(t *testing.T) {
	e := selectionEditor(t)
	e.workBudget = resources.NewFixedBudget(32 << 20)
	a := e.app.(*editorTestApp)
	a.runLater = make(chan func(), 8)
	oldArea, oldTurf := dmmap.BaseArea, dmmap.BaseTurf
	dmmap.BaseArea, dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[0].Prefab(), e.dmm.Tiles[0].Instances()[1].Prefab()
	t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = oldArea, oldTurf })
	for x := 2; x <= directLocalTiles+1; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
		e.dmm.Tiles = append(e.dmm.Tiles, tile)
	}
	e.dmm.MaxX = directLocalTiles + 1
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: float32(e.dmm.MaxX), Y2: 1}, 1)
	targets := make(editing.ShapeDeleteTargets)
	targetPath := e.dmm.Tiles[0].Instances()[2].Prefab().Path()
	for _, tile := range before.Tiles {
		for _, prefab := range tile.State.Prefabs {
			if prefab.Path == targetPath {
				targets[string(prefab.StableID)] = util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}
			}
		}
	}
	if len(targets) != selection.Len() {
		t.Fatalf("fixture targets=%d, selection=%d", len(targets), selection.Len())
	}
	admission, err := e.ShapeDeleteBudget().Reserve(1024 + uint64(len(targets))*160)
	if err != nil {
		t.Fatal(err)
	}
	fence, err := e.BeginShapeDelete(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.EraseShape(selection, false, e.BrushFilter(), fence, targets, admission); err != nil {
		t.Fatal(err)
	}
	if e.localWork == nil || e.workBudget.Used() < admission.Bytes() {
		t.Fatal("target admission was released before worker completion")
	}
	for e.localWork != nil {
		a.runScheduled(t)
	}
	if e.workBudget.Used() != 0 {
		t.Fatal("shape admission leaked after completion")
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil || after.Revision != before.Revision+1 {
		t.Fatal("shape was not one operation", err)
	}
	for _, tile := range after.Tiles {
		if len(tile.State.Prefabs) != 2 {
			t.Fatal("bulk shape missed a resolved target")
		}
	}
	a.commands.UndoV("test")
	for e.localWork != nil {
		a.runScheduled(t)
	}
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before.Tiles, undone.Tiles) {
		t.Fatal("bulk shape undo changed identities", err)
	}
}
