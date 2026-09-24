package wsmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestHeldRotationRegistryTargetsPasteAndLeavesSelectionAlone(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	pressSelectionShortcut(glfw.KeyE)
	if resizeSnapshot(t, e).Revision != 0 {
		t.Fatal("Q/E rotated ordinary selection without held payload")
	}
	app.Clipboard().Copy(dm.NewPathsFilterEmpty(), e.Dmm(), []util.Point{{X: 1, Y: 1, Z: 1}})
	ws.Map().CanvasState().SetMousePosition(32, 0, 1)
	e.TilePasteSelected()
	settlePastePreview(t, ws, app)
	input := "value"
	io := imgui.CurrentIO()
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyE))
		}
		imgui.NewFrame()
		imgui.Begin("Held rotation text owner")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &input)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("text fixture inactive")
			}
			shortcut.Process()
		}
		imgui.End()
		imgui.EndFrame()
	}
	io.KeyRelease(int(glfw.KeyE))
	imgui.NewFrame()
	imgui.EndFrame()
	pressSelectionShortcut(glfw.KeyQ)
	settlePastePreview(t, ws, app)
	pressSelectionShortcut(glfw.KeyE)
	settlePastePreview(t, ws, app)
	pressSelectionShortcut(glfw.KeyE)
	settlePastePreview(t, ws, app)
	if resizeSnapshot(t, e).Revision != 0 {
		t.Fatal("held rotation committed before placement")
	}
	grab = tools.Selected().(*tools.ToolGrab)
	if !grab.ConfirmPlacement() {
		t.Fatal("rotated paste not confirmed")
	}
	settlePastePreview(t, ws, app)
	tile := e.Dmm().GetTile(util.Point{X: 2, Y: 1, Z: 1})
	for _, instance := range tile.Instances() {
		if instance.Prefab().Path() == "/obj/foo" && instance.Prefab().Vars().ValueV("dir", "") != "8" {
			t.Fatal("paste did not use Q/E rotated data")
		}
	}
	if resizeSnapshot(t, e).Revision != 1 {
		t.Fatal("rotation and paste were not one operation")
	}
	for _, instance := range app.Clipboard().Buffer().Buffer[0].Instances() {
		if instance.Prefab().Path() == "/obj/foo" && instance.Prefab().Vars().ValueV("dir", "") != "2" {
			t.Fatal("rotation mutated clipboard")
		}
	}
}
