package window_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

func TestLocalRecoveryWorkspaceSave(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	tools.SetSelected(tools.TNMove)
	frame(false, 1, 1)
	frame(false, 1, 1)
	instance := e.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2]
	ws.Map().CanvasState().SetHoveredInstance(instance)
	e.Dmm().GetTile(util.Point{X: 3, Y: 1, Z: 1}).Instances()[2].SetStableID("damaged-destination")
	frame(true, 1, 1)
	frame(true, 1, 1)
	frame(true, 2, 1)
	frame(true, 3, 1)
	frame(false, 3, 1)
	frame(false, 3, 1)
	if instance.Coord().X != 2 {
		t.Fatal("fixture did not retain the earlier valid move")
	}
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("damaged gesture allowed Save")
	}
	retained := e.Dmm().Copy()
	draft, err := e.InspectLocalRecovery()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "failed-move.json")
	if err := draft.Export(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	// The actual pane action opens the application's modal registry, which also
	// fences shortcuts. Closing inspection must leave the display untouched.
	ws.Map().OpenLocalRecovery()
	t.Cleanup(func() { dialog.Close(dialog.TypeCustom{Title: "Local edit recovery"}) })
	imgui.NewFrame()
	dialog.Process()
	if !imgui.IsPopupOpen("Local edit recovery") {
		t.Fatal("pane action did not open recovery modal")
	}
	if imgui.BeginPopupModalV("Local edit recovery", nil, imgui.WindowFlagsNone) {
		imgui.CloseCurrentPopup()
		imgui.EndPopup()
	}
	imgui.EndFrame()
	dialog.Close(dialog.TypeCustom{Title: "Local edit recovery"})
	if !reflect.DeepEqual(e.Dmm(), &retained) {
		t.Fatal("inspection changed the retained move")
	}
	if err := e.DiscardLocalRecovery(draft); err != nil {
		t.Fatal(err)
	}
	recovered, err := e.SaveSnapshot(context.Background())
	if err != nil || !reflect.DeepEqual(recovered, initial) {
		t.Fatal("discard failed to restore original authority", err)
	}
	if !ws.Save() {
		t.Fatal("native workspace could not Save after recovery")
	}
	if _, err := dmmdata.New(e.Dmm().Path.Absolute); err != nil {
		t.Fatal("recovered saved map did not parse", err)
	}
	if ws.HasUnsavedChanges() {
		t.Fatal("successful recovered Save left map dirty")
	}
	if app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("discard created an undo operation")
	}
}
