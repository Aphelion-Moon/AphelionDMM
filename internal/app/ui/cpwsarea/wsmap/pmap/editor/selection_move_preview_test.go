package editor

import (
	"context"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
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
	counted := &trackedSelectionCapture{countedLocalEdits: &countedLocalEdits{Local: e.executor.(*executor.Local)}}
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
	if counted.captures != 1 || counted.source.DocumentID() != "" {
		t.Fatal("completed preview preparation kept its authority source pinned")
	}
	payload, presentation := e.selectionMovePreview.payload, e.selectionMovePreview.presentation
	if presentation == nil || presentation.MaySuppress == nil {
		t.Fatal("selection preview has no bounded suppression hint")
	}
	for i := 0; i < 100; i++ {
		if _, err := e.PreviewSelectionMovePreview(move, util.Point{X: i % 2}); err != nil {
			t.Fatal(err)
		}
		e.ProcessPasteWork()
		sourceBounds := util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}
		if !presentation.MaySuppress(sourceBounds) || !presentation.MaySuppress(move.Bounds()) || presentation.MaySuppress(util.Bounds{X1: 3, Y1: 1, X2: 24, Y2: 24}) {
			t.Fatal("selection suppression bounds lost the source, moving destination, or unaffected chunk")
		}
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

type trackedSelectionCapture struct {
	*countedLocalEdits
	source   engine.ScopedTileCapture
	captures int
}

func (local *trackedSelectionCapture) CaptureScopedTiles(ctx context.Context) (engine.ScopedTileCapture, error) {
	capture, err := local.Local.CaptureScopedTiles(ctx)
	if err == nil {
		local.captures++
		local.source = capture
	}
	return capture, err
}

func TestSelectionMoveReleasesCaptureOnSetupFailure(t *testing.T) {
	e := selectionEditor(t)
	counted := &trackedSelectionCapture{countedLocalEdits: &countedLocalEdits{Local: e.executor.(*executor.Local)}}
	e.executor = counted
	e.authoritative.DocumentID = ""
	if _, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)); err == nil {
		t.Fatal("mismatched authority started a preview")
	}
	if counted.captures != 1 || counted.source.DocumentID() != "" || e.editWorkBudget().Used() != 0 {
		t.Fatal("failed preview setup retained its source or reservation")
	}
}

func TestSelectionMoveRotationReleaseBeforeReadyAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "cancel"}[cancel], func(t *testing.T) {
			e := selectionEditor(t)
			app := e.app.(*editorTestApp)
			app.runLater = make(chan func(), 8)
			previousArea, previousTurf := dmmap.BaseArea, dmmap.BaseTurf
			dmmap.BaseArea, dmmap.BaseTurf = e.dmm.Tiles[0].Instances()[0].Prefab(), e.dmm.Tiles[0].Instances()[1].Prefab()
			t.Cleanup(func() { dmmap.BaseArea, dmmap.BaseTurf = previousArea, previousTurf })
			counted := &trackedSelectionCapture{countedLocalEdits: &countedLocalEdits{Local: e.executor.(*executor.Local)}}
			e.executor = counted
			before := e.dmm.Copy()
			id := e.dmm.Tiles[0].Instances()[2].StableID()
			move, err := e.BeginSelectionMovePreview(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1))
			if err != nil {
				t.Fatal(err)
			}
			session := e.selectionMovePreview
			for range 5 {
				if _, err := e.RotateSelectionMovePreview(move, true); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(before, e.dmm.Copy()) || e.authoritative.Revision != 0 {
				t.Fatal("rotation preview mutated authority")
			}
			if err := e.FinishSelectionMovePreview(move, cancel); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for e.selectionMovePreview != nil || e.localWork != nil || e.editWorkBudget().Used() != 0 {
				if time.Now().After(deadline) {
					t.Fatal("rotation did not settle or release reservation")
				}
				e.ProcessPasteWork()
				select {
				case job := <-app.runLater:
					job()
				default:
				}
				time.Sleep(time.Millisecond)
			}
			if counted.captures != 1 || counted.source.DocumentID() != "" {
				t.Fatal("rotation recaptured or retained the source")
			}
			if cancel {
				if !reflect.DeepEqual(before, e.dmm.Copy()) || app.commands.HasUndoV("test") {
					t.Fatal("cancel changed map/history")
				}
				return
			}
			if session.sourcePayload == nil || session.payload == session.sourcePayload {
				t.Fatal("rotation did not derive payload")
			}
			assertEditorDirection(t, e.dmm, "8")
			if e.authoritative.Revision != 1 || e.dmm.Tiles[0].Instances()[2].StableID() != id {
				t.Fatal("rotation lost single operation or identity")
			}
			app.commands.UndoV("test")
			for e.localWork != nil {
				app.runScheduled(t)
			}
			assertEditorDirection(t, e.dmm, "2")
			if app.commands.HasUndoV("test") {
				t.Fatal("rotation added more than one undo step")
			}
		})
	}
}
