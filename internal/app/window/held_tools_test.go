package window_test

import (
	"context"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func TestHeldToolsRespectGrabMouseOwnership(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func() {
		t.Helper()
		state, err := e.SaveSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got, err := state.Hash()
		if err != nil || got != wantHash {
			t.Fatal("held tool switch changed authority", err)
		}
		display, err := mapadapter.Import(e.Dmm(), initial.DocumentID, initial.EnvironmentHash)
		if err != nil {
			t.Fatal(err)
		}
		got, err = display.Hash()
		if err != nil || got != wantHash || app.commands.HasUndoV(ws.CommandStackId()) {
			t.Fatal("held tool switch committed or retained the preview", err)
		}
	}
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	grab.Reset()
	area := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}}
	grab.SelectArea(area)
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	frame(false, 1, 1)
	frame(false, 1, 1)
	frame(true, 1, 1)
	frame(true, 1, 1)
	frame(true, 2, 2)
	if grab.Stale() || grab.Bounds() != (util.Bounds{X1: 2, Y1: 2, X2: 3, Y2: 2}) {
		t.Fatal("fixture did not create a moved preview")
	}
	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyS))
	frame(true, 2, 2)
	if !tools.IsSelected(tools.TNGrab) || grab.Stale() {
		t.Fatal("held S stole an active mouse drag")
	}
	io.KeyPress(int(glfw.KeyD))
	frame(true, 2, 2)
	if !tools.IsSelected(tools.TNGrab) || grab.Stale() {
		t.Fatal("overlapping held keys stole an active mouse drag")
	}
	frame(false, 2, 2)
	frame(false, 2, 2)
	if !tools.IsSelected(tools.TNGrab) || !grab.Stale() || !grab.HasSelectedArea() {
		t.Fatal("previously blocked held keys activated after mouse release")
	}
	committed, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	committedHash, err := committed.Hash()
	if err != nil || committedHash == wantHash || !app.commands.HasUndoV(ws.CommandStackId()) {
		t.Fatal("mouse release did not commit the original drag", err)
	}
	io.KeyRelease(int(glfw.KeyS))
	io.KeyRelease(int(glfw.KeyD))
	frame(false, 2, 2)
	// A fresh key press now owns temporary selection and clears Grab geometry,
	// but it must not alter the completed map operation or its undo history.
	io.KeyPress(int(glfw.KeyS))
	frame(false, 2, 2)
	if !tools.IsSelected(tools.TNPick) || grab.HasSelectedArea() {
		t.Fatal("fresh S press did not select Pick and clear Grab")
	}
	io.KeyPress(int(glfw.KeyD))
	frame(false, 2, 2)
	if !tools.IsSelected(tools.TNPick) {
		t.Fatal("lower-priority hold displaced Pick")
	}
	io.KeyRelease(int(glfw.KeyS))
	frame(false, 3, 2)
	if !tools.IsSelected(tools.TNDelete) {
		t.Fatal("remaining D hold did not select Delete")
	}
	io.KeyRelease(int(glfw.KeyD))
	frame(false, 3, 2)
	if !tools.IsSelected(tools.TNGrab) {
		t.Fatal("releasing holds did not restore Grab")
	}
	current, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	currentHash, err := current.Hash()
	if err != nil || currentHash != committedHash {
		t.Fatal("held tool transitions altered the committed map", err)
	}
	app.commands.UndoV(ws.CommandStackId())
	assertUnchanged()
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}
}
