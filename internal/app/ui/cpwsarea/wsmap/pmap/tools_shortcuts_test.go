package pmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"runtime"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"testing"
)

func TestTemporaryToolsDoNotStealPopupInput(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	previous := tools.Selected().Name()
	defer tools.SetSelected(previous)
	tools.SetSelected(tools.TNGrab)
	shortcut.BeginFrame()
	imgui.NewFrame()
	imgui.OpenPopup("Tool input popup")
	if !imgui.BeginPopup("Tool input popup") {
		t.Fatal("popup fixture did not open")
	}
	imgui.Text("Popup owns input")
	imgui.EndPopup()
	imgui.EndFrame()
	io.KeyPress(int(glfw.KeyD))
	shortcut.BeginFrame()
	imgui.NewFrame()
	processTempToolsMode()
	if !tools.IsSelected(tools.TNGrab) {
		t.Fatal("popup input switched to a held tool")
	}
	if imgui.BeginPopup("Tool input popup") {
		imgui.CloseCurrentPopup()
		imgui.EndPopup()
	}
	processTempToolsMode()
	if !tools.IsSelected(tools.TNGrab) {
		t.Fatal("popup dismissal frame switched to a held tool")
	}
	imgui.EndFrame()
	shortcut.BeginFrame()
}

func TestTemporaryToolsDoNotStealSaveShortcut(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	previous := tools.Selected().Name()
	defer tools.SetSelected(previous)
	tools.SetSelected(tools.TNGrab)
	io.KeyPress(int(glfw.KeyLeftControl))
	io.KeyPress(int(glfw.KeyS))
	imgui.NewFrame()
	defer imgui.EndFrame()
	processTempToolsMode()
	if !tools.IsSelected(tools.TNGrab) {
		t.Fatal("Ctrl+S switched Grab to a temporary tool")
	}
}

func TestTemporaryToolsDoNotStealTextInput(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	previous := tools.Selected().Name()
	defer tools.SetSelected(previous)
	tools.SetSelected(tools.TNGrab)
	value := ""
	for frame := 0; frame < 3; frame++ {
		if frame == 2 {
			io.KeyPress(int(glfw.KeyR))
		}
		imgui.NewFrame()
		imgui.Begin("Text input verification")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &value)
		if frame == 2 {
			if !imgui.IsAnyItemActive() {
				t.Fatal("test did not focus a text field")
			}
			processTempToolsMode()
			if !tools.IsSelected(tools.TNGrab) {
				t.Fatal("typing R changed the current tool")
			}
		}
		imgui.End()
		imgui.EndFrame()
	}
}
