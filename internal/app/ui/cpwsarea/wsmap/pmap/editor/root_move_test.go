// APHELION EDIT ADDITION START - COMPOSITION ANCHORS
package editor

import (
	"context"
	"reflect"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"testing"

	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

type compositionLockedApp struct {
	app
	point util.Point
}

func (a *compositionLockedApp) CompositionTileLocked(_ string, p util.Point) bool {
	return p == a.point
}
func (a *compositionLockedApp) CompositionEditFence(string) func(util.Point) bool {
	point := a.point
	return func(p util.Point) bool { return p == point }
}

func TestCompositionLocksCaptureAndAsynchronousBulkBeforeAcceptance(t *testing.T) {
	e := selectionEditor(t)
	a := e.app.(*editorTestApp)
	a.runLater = make(chan func(), 8)
	point := util.Point{X: 1, Y: 1, Z: 1}
	e.app = &compositionLockedApp{app: e.app, point: point}
	if e.TryBeginTileChange(point) {
		t.Fatal("captured a locked contribution")
	}
	before := e.authoritative.Tiles[0].State
	after := model.CloneTileState(before)
	after.Prefabs[2].Vars["dir"] = "4"
	done := false
	err := e.startLocalWork(e.executor.(localEditExecutor), true, 4096, func(context.Context, *resources.Reservation) ([]model.TileChange, error) {
		return []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: before, After: after}}, nil
	}, func(_ engine.LocalAcceptance, _ []model.TileChange, err error) {
		if err == nil {
			t.Error("bulk edit ignored locked footprint")
		}
		done = true
	})
	if err != nil {
		t.Fatal(err)
	}
	for !done {
		a.runScheduled(t)
	}
	if e.authoritative.Revision != 0 || e.editWorkBudget().Used() != 0 {
		t.Fatal("rejected bulk edit advanced authority or leaked admission")
	}
	assertEditorDirection(t, e.dmm, "2")
}

func TestCompositionRootMoveIsExactAndUndoable(t *testing.T) {
	e := captureEditor(t)
	i := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(i, dmmprefab.New(dmmprefab.IdNone, "/obj/modular_map_root", i.Prefab().Vars()))
	e.CommitOperation("Root fixture")
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := e.SaveVersion()
	root := mapping.Root{StableID: i.StableID(), Local: i.Coord(), Source: mapping.Identity{DocumentID: string(before.DocumentID), Generation: generation, Revision: uint64(before.Revision)}}
	to := util.Point{X: 2, Y: 1, Z: 1}
	if err := e.MoveCompositionRoot(root, to); err != nil {
		t.Fatal(err)
	}
	after, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatal("move did not produce exactly one accepted operation")
	}
	if i.Coord() != to || i.StableID() != root.StableID {
		t.Fatal("wrong root identity or destination")
	}
	e.app.CommandStorage().UndoV("test")
	undone, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(undone.Tiles, before.Tiles) {
		t.Fatal("Undo failed to restore exact source tiles")
	}
	e.app.CommandStorage().RedoV("test")
	redone, _ := e.SaveSnapshot(context.Background())
	if !reflect.DeepEqual(redone.Tiles, after.Tiles) {
		t.Fatal("Redo changed unrelated source state")
	}
	if err := e.MoveCompositionRoot(root, util.Point{X: 3, Y: 1, Z: 1}); err == nil {
		t.Fatal("stale root coordinates were accepted")
	}
}

// APHELION EDIT ADDITION END
