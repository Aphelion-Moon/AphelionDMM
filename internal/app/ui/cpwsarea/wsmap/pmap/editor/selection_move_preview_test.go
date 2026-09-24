package editor

import (
	"context"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSelectionMovePresentationCancelThenEdit(t *testing.T) {
	e := selectionEditor(t)
	previousArea, previousTurf := dmmap.BaseArea, dmmap.BaseTurf
	dmmap.BaseArea = e.dmm.Tiles[0].Instances()[0].Prefab()
	dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[1].Prefab()
	t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = previousArea, previousTurf })
	second := &dmmap.Tile{Coord: util.Point{X: 2, Y: 1, Z: 1}}
	second.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
	e.dmm.Tiles = append(e.dmm.Tiles, second)
	e.dmm.MaxX = 2
	e.initializeCollaboration()
	counted := &countedLocalEdits{Local: e.executor.(*executor.Local)}
	e.executor = counted
	before := e.dmm.Copy()
	generation, revision := e.SaveVersion()
	view, _ := e.MapViewVersion()
	move, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.CancelSelectionMovePreview)
	deadline := time.Now().Add(3 * time.Second)
	for e.selectionMovePreview.preparing {
		if time.Now().After(deadline) {
			t.Fatal("move source did not settle")
		}
		e.ProcessPasteWork()
		time.Sleep(time.Millisecond)
	}
	if e.selectionMovePreview.err != nil {
		t.Fatal(e.selectionMovePreview.err)
	}
	payload, presentation := e.selectionMovePreview.payload, e.selectionMovePreview.presentation
	for i := 0; i < 100; i++ {
		if _, err := e.PreviewSelectionMovePreview(move, util.Point{X: i % 2}); err != nil {
			t.Fatal(err)
		}
		e.ProcessPasteWork()
	}
	if !reflect.DeepEqual(before, e.dmm.Copy()) || len(e.pendingChanges) != 0 || counted.snapshots != 0 || counted.wireEdits != 0 {
		t.Fatal("move presentation entered map mutation, capture, or protocol work")
	}
	if e.selectionMovePreview.payload != payload || e.selectionMovePreview.presentation != presentation {
		t.Fatal("translation rebuilt source presentation")
	}
	if e.ChangedSinceSave(generation, revision) || e.app.CommandStorage().HasUndoV("test") {
		t.Fatal("pure movement changed dirty state or history")
	}
	if got, ready := e.MapViewVersion(); got != view || !ready {
		t.Fatal("pure movement invalidated committed query")
	}
	capture, _, err := e.CaptureSaveSnapshot(context.Background())
	if err != nil || capture.Snapshot().Revision != revision {
		t.Fatal("pure movement blocked committed save", err)
	}
	if err := e.FinishSelectionMovePreview(move, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, e.dmm.Copy()) || e.editWorkBudget().Used() != 0 || !e.CanStartMapEdit() {
		t.Fatal("cancel changed map or retained ownership/resources")
	}
	instance := e.dmm.Tiles[0].Instances()[2]
	e.InstanceReplace(instance, dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4")))
	e.CommitOperation("Edit after cancelled move")
	assertEditorDirection(t, e.dmm, "4")
	e.app.CommandStorage().UndoV("test")
	assertEditorDirection(t, e.dmm, "2")
}
