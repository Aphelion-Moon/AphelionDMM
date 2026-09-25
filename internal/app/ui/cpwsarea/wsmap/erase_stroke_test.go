package wsmap

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestShapeEraseNativeOverhangStaysInsideSelection(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	outside := util.Point{X: 1, Y: 1, Z: 1}
	inside := util.Point{X: 2, Y: 1, Z: 1}
	i := e.Dmm().GetTile(outside).Instances()[2]
	vars := dmvars.Set(dmvars.Set(i.Prefab().Vars(), "pixel_x", "24"), "layer", "10")
	e.InstanceReplace(i, dmmprefab.New(dmmprefab.IdNone, i.Prefab().Path(), vars))
	e.CommitOperation("offset fixture")
	ws.Map().Canvas().Render().UpdateBucket(e.Dmm(), 1)
	before := resizeSnapshot(t, e)
	selection := editing.RectangleSelection(util.Bounds{X1: 2, Y1: 1, X2: 2, Y2: 1}, 1)
	fence, err := e.BeginShapeDelete(1)
	if err != nil {
		t.Fatal(err)
	}
	target, found, err := e.PickShapeDeleteTarget(inside, selection, e.BrushFilter(), fence)
	if err != nil || !found || target.Coord != inside {
		t.Fatal("shape picked outside exact membership", target, found, err)
	}
	if err = e.EraseShape(selection, false, e.BrushFilter(), fence, editing.ShapeDeleteTargets{string(target.StableID): target.Coord}); err != nil {
		t.Fatal(err)
	}
	if len(e.Dmm().GetTile(outside).Instances()) != 3 || len(e.Dmm().GetTile(inside).Instances()) != 2 {
		t.Fatal("shape erased overhang source outside its footprint")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if !reflect.DeepEqual(before.Tiles, resizeSnapshot(t, e).Tiles) {
		t.Fatal("shape undo changed identities")
	}
}

func TestEraseStrokeNativeCoverageAndSingleUndo(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "picked", true: "all"}[all], func(t *testing.T) {
			ws, app := newSelectionWorkspace(t)
			e := ws.Map().Editor()
			before := resizeSnapshot(t, e)
			ws.Map().Canvas().Render().SetActiveLevel(e.Dmm(), 1)
			stroke, err := e.StartEraseStroke(all)
			if err != nil {
				t.Fatal(err)
			}
			stroke.Sample(16, 16, 1)
			stroke.Sample(127, 16, 1)
			for i := 0; i < 8; i++ {
				stroke.Sample(127, 16, 1)
			}
			stroke.Sample(-20, 16, 1)
			if stroke.Err() != nil || stroke.Changes() != 4 {
				t.Fatalf("coverage: %d changes, %v", stroke.Changes(), stroke.Err())
			}
			e.FinishEraseStroke(stroke)
			if resizeSnapshot(t, e).Revision != before.Revision+1 {
				t.Fatal("stroke did not submit once")
			}
			for x := 1; x <= 4; x++ {
				if len(e.Dmm().GetTile(util.Point{X: x, Y: 1, Z: 1}).Instances()) != 2 {
					t.Fatal("default validity or crossed coverage lost", x)
				}
			}
			app.commands.UndoV(e.Dmm().Path.Absolute)
			if !reflect.DeepEqual(resizeSnapshot(t, e).Tiles, before.Tiles) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
				t.Fatal("stroke was not one exact undo")
			}
		})
	}
}

func TestEraseStrokeNativeFrozenFilterAndOffsetIdentity(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	app.PathsFilter().TogglePath("/obj/foo")
	stroke, err := e.StartEraseStroke(true)
	if err != nil {
		t.Fatal(err)
	}
	app.PathsFilter().TogglePath("/obj/foo")
	stroke.Sample(16, 16, 1)
	e.FinishEraseStroke(stroke)
	if stroke.Changes() != 0 || resizeSnapshot(t, e).Revision != 0 || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("frozen exclusion or no-op history failed")
	}
	coord := util.Point{X: 1, Y: 1, Z: 1}
	i := e.Dmm().GetTile(coord).Instances()[2]
	offsetVars := dmvars.Set(i.Prefab().Vars(), "pixel_x", "24")
	offsetVars = dmvars.Set(offsetVars, "layer", "10")
	e.InstanceReplace(i, dmmprefab.New(dmmprefab.IdNone, i.Prefab().Path(), offsetVars))
	e.CommitOperation("offset fixture")
	before := resizeSnapshot(t, e)
	ws.Map().Canvas().Render().SetActiveLevel(e.Dmm(), 1)
	ws.Map().Canvas().Render().UpdateBucket(e.Dmm(), 1)
	stroke, err = e.StartEraseStroke(false)
	if err != nil {
		t.Fatal(err)
	}
	stroke.Sample(40, 16, 1)
	stroke.Sample(55, 16, 1)
	stroke.Sample(25, 16, 1)
	if stroke.Err() != nil || stroke.Changes() != 1 || !stroke.Processed(coord) {
		t.Fatalf("offset instance revisited: %d %v", stroke.Changes(), stroke.Err())
	}
	e.FinishEraseStroke(stroke)
	if len(e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()) != 3 {
		t.Fatal("offset hit drilled into adjacent tile")
	}
	app.commands.UndoV(e.Dmm().Path.Absolute)
	if !reflect.DeepEqual(resizeSnapshot(t, e).Tiles, before.Tiles) {
		t.Fatal("offset erase undo changed contents or identity")
	}
}

func TestEraseStrokeNativeCaptureFailureHasOneRecoveryError(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	e := ws.Map().Editor()
	fault := util.Point{X: 2, Y: 1, Z: 1}
	e.Dmm().GetTile(fault).Instances()[2].SetStableID("invalid-erase-capture")
	stroke, err := e.StartEraseStroke(true)
	if err != nil {
		t.Fatal(err)
	}
	stroke.Sample(16, 16, 1)
	stroke.Sample(127, 16, 1)
	if stroke.Err() == nil || stroke.Processed(fault) || stroke.Changes() != 1 {
		t.Fatal("failed target marked or stroke continued")
	}
	e.FinishEraseStroke(stroke)
	if len(app.errors) != 1 || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("fault duplicated error or committed partial stroke")
	}
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("unfinished stroke lost recovery guard")
	}
	if len(e.Dmm().GetTile(fault).Instances()) != 3 {
		t.Fatal("failed target was mutated")
	}
}
