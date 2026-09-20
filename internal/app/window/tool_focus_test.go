package window_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

func (*mouseNetworkApp) DoSelectPrefab(*dmmprefab.Prefab)     {}
func (*mouseNetworkApp) DoEditInstance(*dmminstance.Instance) {}
func (*mouseNetworkApp) ShowLayout(string, bool)              {}

func TestObjectMoveFocusTransferKeepsGestureOwner(t *testing.T) {
	first, app := newMouseNetworkWorkspace(t)
	firstEditor := first.Map().Editor()
	initial, err := firstEditor.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash := func(snapshot model.Snapshot) string {
		t.Helper()
		value, err := snapshot.Hash()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	otherMap := firstEditor.Dmm().Copy()
	otherMap.Name = "other.dmm"
	otherMap.Path.Absolute = filepath.Join(t.TempDir(), otherMap.Name)
	otherApp := &mouseNetworkApp{environment: app.environment, commands: app.commands, queued: make(chan struct{}, 8)}
	second := wsmap.New(otherApp, &otherMap)
	t.Cleanup(func() { second.OnFocusChange(false); second.Dispose(); window.DrainFrameJobsForTest() })
	secondInitial, err := second.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	frame := mouseWorkspaceFrame(t, first, app.mouse)
	tools.SetSelected(tools.TNMove)
	frame(false, 1, 1)
	frame(false, 1, 1)
	first.Map().CanvasState().SetHoveredInstance(firstEditor.Dmm().GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[2])
	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyLeftShift))
	frame(true, 1, 1)
	frame(true, 1, 1)
	frame(true, 2, 1)
	preview, err := mapadapter.Import(firstEditor.Dmm(), initial.DocumentID, initial.EnvironmentHash)
	if err != nil {
		t.Fatal(err)
	}
	if hash(preview) == hash(initial) {
		t.Fatal("fixture did not move the object's pixel offset")
	}
	first.OnFocusChange(false)
	second.OnFocusChange(true)
	app.commands.SetStack(second.CommandStackId())
	second.Map().CanvasState().SetHoveredInstance(second.Map().Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()[2])
	secondFrame := mouseWorkspaceFrame(t, second, otherApp.mouse)
	secondFrame(true, 2, 1)
	secondFrame(true, 3, 1)
	secondFrame(false, 3, 1)
	secondFrame(false, 3, 1)
	io.KeyRelease(int(glfw.KeyLeftShift))
	got, err := firstEditor.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatalf("focus transfer stranded the source map gesture: %v", err)
	}
	if hash(got) != hash(preview) {
		t.Fatal("later mouse input changed the source gesture after focus transfer")
	}
	secondGot, err := second.Map().Editor().SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hash(secondGot) != hash(secondInitial) || app.commands.HasUndoV(second.CommandStackId()) {
		t.Fatal("source gesture modified the destination map or history")
	}
	if !app.commands.HasUndoV(first.CommandStackId()) {
		t.Fatal("source gesture was not committed to its own history")
	}
	app.commands.UndoV(first.CommandStackId())
	got, err = firstEditor.SaveSnapshot(context.Background())
	if err != nil || hash(got) != hash(initial) {
		t.Fatal("source undo did not restore its original state", err)
	}
	// Releasing the transferred press must rearm the tool for a deliberate new
	// gesture in the destination map.
	second.Map().CanvasState().SetHoveredInstance(second.Map().Dmm().GetTile(util.Point{X: 3, Y: 1, Z: 1}).Instances()[2])
	io.KeyPress(int(glfw.KeyLeftShift))
	secondFrame(true, 3, 1)
	secondFrame(true, 3, 1)
	secondFrame(true, 4, 1)
	secondFrame(false, 4, 1)
	secondFrame(false, 4, 1)
	io.KeyRelease(int(glfw.KeyLeftShift))
	secondGot, err = second.Map().Editor().SaveSnapshot(context.Background())
	if err != nil || hash(secondGot) == hash(secondInitial) || !app.commands.HasUndoV(second.CommandStackId()) {
		t.Fatal("fresh destination gesture did not rearm after release", err)
	}
	app.commands.UndoV(second.CommandStackId())
	secondGot, err = second.Map().Editor().SaveSnapshot(context.Background())
	if err != nil || hash(secondGot) != hash(secondInitial) {
		t.Fatal("destination undo did not restore its own state", err)
	}
	if len(app.errors) != 0 || len(otherApp.errors) != 0 {
		t.Fatalf("unexpected editor errors: %v %v", app.errors, otherApp.errors)
	}
}

func TestGrabFocusTransferCancelsOnlySourcePreview(t *testing.T) {
	first, app := newMouseNetworkWorkspace(t)
	e := first.Map().Editor()
	initial, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}
	otherMap := e.Dmm().Copy()
	otherMap.Name = "other.dmm"
	otherMap.Path.Absolute = filepath.Join(t.TempDir(), otherMap.Name)
	otherApp := &mouseNetworkApp{environment: app.environment, commands: app.commands, queued: make(chan struct{}, 8)}
	second := wsmap.New(otherApp, &otherMap)
	t.Cleanup(func() { second.OnFocusChange(false); second.Dispose(); window.DrainFrameJobsForTest() })
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	frame := mouseWorkspaceFrame(t, first, app.mouse)
	frame(false, 1, 1)
	frame(false, 1, 1)
	frame(true, 1, 1)
	frame(true, 1, 1)
	frame(true, 2, 2)
	if grab.Stale() || grab.Bounds() != (util.Bounds{X1: 2, Y1: 2, X2: 3, Y2: 2}) {
		t.Fatal("fixture did not move Grab")
	}
	first.OnFocusChange(false)
	second.OnFocusChange(true)
	if !grab.Stale() || grab.HasSelectedArea() {
		t.Fatal("focus transfer retained the source selection")
	}
	got, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err := got.Hash()
	if err != nil || gotHash != wantHash || app.commands.HasUndoV(first.CommandStackId()) {
		t.Fatal("focus transfer committed or lost the cancelled preview", err)
	}
	secondFrame := mouseWorkspaceFrame(t, second, otherApp.mouse)
	secondFrame(true, 2, 2)
	secondFrame(true, 3, 2)
	if grab.HasSelectedArea() || !grab.Stale() {
		t.Fatal("held mouse press started a selection in another map")
	}
	secondFrame(false, 3, 2)
	secondFrame(false, 3, 2)
	grab.SelectArea([]util.Point{{X: 3, Y: 2, Z: 1}})
	wantBounds := grab.Bounds()
	first.OnFocusChange(false)
	if !grab.HasSelectedArea() || grab.Bounds() != wantBounds {
		t.Fatal("old pane deactivation cleared the new owner's selection")
	}
	if _, err := second.Map().Editor().SaveSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if app.commands.HasUndoV(second.CommandStackId()) || len(app.errors) != 0 || len(otherApp.errors) != 0 {
		t.Fatal("cancelled focus transfer changed history or reported an error")
	}
}
