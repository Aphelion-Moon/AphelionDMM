package wsmap

import (
	"context"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func pressSelectionShortcut(keys ...glfw.Key) {
	io := imgui.CurrentIO()
	for _, key := range keys {
		io.KeyPress(int(key))
	}
	imgui.NewFrame()
	shortcut.Process()
	imgui.EndFrame()
	for _, key := range keys {
		io.KeyRelease(int(key))
	}
	imgui.NewFrame()
	imgui.EndFrame()
}

func activateSelectionWorkspace(t *testing.T, ws *WsMap) *tools.ToolGrab {
	t.Helper()
	ws.OnCommandContextChange(true)
	ws.OnFocusChange(true)
	t.Cleanup(func() { ws.OnCommandContextChange(false); ws.OnFocusChange(false) })
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
	return grab
}

func TestMapShortcutOwnershipFollowsActiveDocument(t *testing.T) {
	first, app := newSelectionWorkspace(t)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	first.OnCommandContextChange(true)
	first.OnFocusChange(true)
	t.Cleanup(func() { first.OnCommandContextChange(false); first.OnFocusChange(false) })
	defer shortcut.ResetBindings("pmap#doMoveCameraRight")
	if err := shortcut.SetBindings("pmap#doMoveCameraRight", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}

	secondDmm := first.Map().Dmm().Copy()
	secondDmm.Name = "second map"
	secondDmm.Path = dmmap.DmmPath{Readable: "second.dmm", Absolute: first.Map().Dmm().Path.Absolute + ".second"}
	second := New(app, &secondDmm)
	t.Cleanup(second.Dispose)
	firstCamera := first.Map().Canvas().Render().Camera
	secondCamera := second.Map().Canvas().Render().Camera
	firstX, secondX := firstCamera.ShiftX, secondCamera.ShiftX

	first.OnFocusChange(false) // Pointer focus may leave the map for its palette.
	first.PreProcess()
	pressSelectionShortcut(glfw.KeyF8)
	if firstCamera.ShiftX == firstX || secondCamera.ShiftX != secondX {
		t.Fatalf("palette focus lost document command context: first-camera=%v second-camera=%v", firstCamera.ShiftX, secondCamera.ShiftX)
	}
	firstX = firstCamera.ShiftX

	first.OnCommandContextChange(false)
	second.OnCommandContextChange(true)
	second.OnFocusChange(true)
	first.PreProcess()
	second.PreProcess()
	pressSelectionShortcut(glfw.KeyF8)
	if firstCamera.ShiftX != firstX || secondCamera.ShiftX == secondX {
		t.Fatalf("shortcut after switching to second map: first-camera=%v second-camera=%v", firstCamera.ShiftX, secondCamera.ShiftX)
	}

	second.OnCommandContextChange(false)
	second.OnFocusChange(false)
	first.OnCommandContextChange(true)
	first.OnFocusChange(true)
	first.PreProcess()
	second.PreProcess()
	pressSelectionShortcut(glfw.KeyF8)
	if firstCamera.ShiftX == firstX || secondCamera.ShiftX == secondX {
		t.Fatalf("shortcut after switching back to first map: first-camera=%v second-camera=%v", firstCamera.ShiftX, secondCamera.ShiftX)
	}
}

func TestSelectionNudgeWorkspaceShortcuts(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := before.Hash()
	if err != nil {
		t.Fatal(err)
	}
	id := e.Dmm().Tiles[0].Instances()[2].StableID()
	camera := ws.Map().Canvas().Render().Camera
	initialCamera := *camera
	pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyLeft)
	pressSelectionShortcut(glfw.KeyLeftControl, glfw.KeyLeftAlt, glfw.KeyRight)
	if grab.Bounds() != (util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}) || app.commands.HasUndoV(e.Dmm().Path.Absolute) {
		t.Fatal("out-of-bounds or unrelated modified chord changed selection/history")
	}
	pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyRight)
	if got := grab.Bounds(); got != (util.Bounds{X1: 2, Y1: 1, X2: 2, Y2: 1}) {
		t.Fatalf("Alt+Right did not nudge: %v", got)
	}
	pressSelectionShortcut(glfw.KeyRightAlt, glfw.KeyUp)
	if got := grab.Bounds(); got != (util.Bounds{X1: 2, Y1: 2, X2: 2, Y2: 2}) {
		t.Fatalf("Alt+Up did not nudge: %v", got)
	}
	if e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2].StableID() != id {
		t.Fatal("nudge changed moved ID")
	}
	if *camera != initialCamera {
		t.Fatal("nudge also panned camera")
	}
	// Alt+Arrow belongs to the text editor while an input has focus.
	input := "text"
	area := grab.Bounds()
	io := imgui.CurrentIO()
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyLeftAlt))
			io.KeyPress(int(glfw.KeyRight))
		}
		imgui.NewFrame()
		imgui.Begin("Nudge input verification")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text-field fixture is not active")
			}
			shortcut.Process()
			if grab.Bounds() != area {
				t.Fatal("nudge stole text-field input")
			}
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyLeftAlt))
	io.KeyRelease(int(glfw.KeyRight))
	path := e.Dmm().Path.Absolute
	app.commands.UndoV(path)
	app.commands.UndoV(path)
	after, err := e.CollaborationSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotHash, err := after.Hash()
	if err != nil || gotHash != wantHash {
		t.Fatal("two undos did not restore exact map")
	}
	app.commands.RedoV(path)
	app.commands.RedoV(path)
	if e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()[2].StableID() != id {
		t.Fatal("redo changed moved ID")
	}
}

func TestSelectionWorkspaceRetainsFastCameraPan(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	camera := ws.Map().Canvas().Render().Camera
	initial := camera.ShiftX
	area := grab.Bounds()
	pressSelectionShortcut(glfw.KeyRight)
	normal := camera.ShiftX - initial
	if normal == 0 {
		t.Fatal("unmodified arrow did not pan")
	}
	initial = camera.ShiftX
	pressSelectionShortcut(glfw.KeyLeftShift, glfw.KeyRight)
	if got := camera.ShiftX - initial; got != normal*5 {
		t.Fatalf("Shift+Arrow pan = %v, want %v", got, normal*5)
	}
	if grab.Bounds() != area {
		t.Fatal("camera pan moved selection")
	}
	defer shortcut.ResetBindings("pmap#panCameraFastRight")
	if err := shortcut.SetBindings("pmap#panCameraFastRight", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	initial = camera.ShiftX
	pressSelectionShortcut(glfw.KeyF8)
	if got := camera.ShiftX - initial; got != normal*5 {
		t.Fatalf("rebound fast pan = %v, want %v", got, normal*5)
	}
	defer shortcut.ResetBindings("pmap#doMoveCameraRight")
	if err := shortcut.SetBindings("pmap#doMoveCameraRight", [][][2]glfw.Key{{{glfw.KeyLeftShift, glfw.KeyRightShift}, {glfw.KeyF9, 0}}}); err != nil {
		t.Fatal(err)
	}
	initial = camera.ShiftX
	pressSelectionShortcut(glfw.KeyRightShift, glfw.KeyF9)
	if got := camera.ShiftX - initial; got != normal {
		t.Fatalf("rebound normal pan = %v, want %v", got, normal)
	}
}

func TestSelectionConfiguredGridStepWorkspaceUndoRedo(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	app.selectionStep = 2
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	before := resizeSnapshot(t, e)
	initialHash := resizeHash(t, before)
	id := e.Dmm().Tiles[0].Instances()[2].StableID()
	pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyRight)
	if grab.Bounds() != (util.Bounds{X1: 3, Y1: 1, X2: 3, Y2: 1}) || resizeSnapshot(t, e).Revision != before.Revision+1 {
		t.Fatal("configured step did not move as one operation")
	}
	movedHash := resizeHash(t, resizeSnapshot(t, e))
	pressSelectionShortcut(glfw.KeyRightAlt, glfw.KeyRight)
	if resizeHash(t, resizeSnapshot(t, e)) != movedHash {
		t.Fatal("out-of-bounds configured step changed authority")
	}
	path := e.Dmm().Path.Absolute
	app.commands.UndoV(path)
	if resizeHash(t, resizeSnapshot(t, e)) != initialHash || grab.Bounds() != (util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}) {
		t.Fatal("one undo did not restore exact map and selection")
	}
	app.commands.RedoV(path)
	if resizeHash(t, resizeSnapshot(t, e)) != movedHash || e.Dmm().GetTile(util.Point{X: 3, Y: 1, Z: 1}).Instances()[2].StableID() != id {
		t.Fatal("redo lost contents or identity")
	}
	app.selectionStep = 1
	pressSelectionShortcut(glfw.KeyRightAlt, glfw.KeyRight)
	if grab.Bounds() != (util.Bounds{X1: 4, Y1: 1, X2: 4, Y2: 1}) {
		t.Fatal("live preference change did not reach shortcut dispatch")
	}
}
