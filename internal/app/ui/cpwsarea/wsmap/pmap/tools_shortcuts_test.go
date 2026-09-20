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

func TestTemporaryToolReleaseOwnership(t *testing.T) {
	type step struct {
		keys             []glfw.Key
		selectTool, want string
	}
	for _, scenario := range []struct {
		name  string
		steps []step
	}{
		{"already_selected_tool", []step{
			{keys: []glfw.Key{glfw.KeyD}, want: tools.TNDelete},
			{want: tools.TNGrab},
			{selectTool: tools.TNPick, keys: []glfw.Key{glfw.KeyS}, want: tools.TNPick},
			{want: tools.TNPick},
		}},
		{"overlapping_holds", []step{
			{keys: []glfw.Key{glfw.KeyS}, want: tools.TNPick},
			{keys: []glfw.Key{glfw.KeyS, glfw.KeyD}, want: tools.TNPick},
			{keys: []glfw.Key{glfw.KeyD}, want: tools.TNDelete},
			{want: tools.TNGrab},
		}},
		{"explicit_selection", []step{
			{keys: []glfw.Key{glfw.KeyD}, want: tools.TNDelete},
			{selectTool: tools.TNMove, keys: []glfw.Key{glfw.KeyD}, want: tools.TNMove},
			{want: tools.TNMove},
		}},
		{"shortcut_release", []step{
			{keys: []glfw.Key{glfw.KeyLeftControl, glfw.KeyS}, want: tools.TNGrab},
			{keys: []glfw.Key{glfw.KeyS}, want: tools.TNGrab},
			{want: tools.TNGrab},
		}},
		{"three_holds", []step{
			{keys: []glfw.Key{glfw.KeyR}, want: tools.TNReplace},
			{keys: []glfw.Key{glfw.KeyR, glfw.KeyD}, want: tools.TNDelete},
			{keys: []glfw.Key{glfw.KeyR, glfw.KeyD, glfw.KeyS}, want: tools.TNPick},
			{keys: []glfw.Key{glfw.KeyR, glfw.KeyS}, want: tools.TNPick},
			{keys: []glfw.Key{glfw.KeyR}, want: tools.TNReplace},
			{want: tools.TNGrab},
		}},
		{"release_during_shortcut", []step{
			{keys: []glfw.Key{glfw.KeyD}, want: tools.TNDelete},
			{keys: []glfw.Key{glfw.KeyLeftControl}, want: tools.TNDelete},
			{want: tools.TNGrab},
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
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
			keys := []glfw.Key{glfw.KeyS, glfw.KeyD, glfw.KeyR, glfw.KeyLeftControl}
			defer func() {
				for _, key := range keys {
					io.KeyRelease(int(key))
				}
				shortcut.BeginFrame()
				imgui.NewFrame()
				processTempToolsMode()
				imgui.EndFrame()
			}()
			tools.SetSelected(tools.TNGrab)
			for i, s := range scenario.steps {
				for _, key := range keys {
					io.KeyRelease(int(key))
				}
				for _, key := range s.keys {
					io.KeyPress(int(key))
				}
				if s.selectTool != "" {
					tools.SetSelected(s.selectTool)
				}
				shortcut.BeginFrame()
				imgui.NewFrame()
				processTempToolsMode()
				imgui.EndFrame()
				if got := tools.Selected().Name(); got != s.want {
					t.Fatalf("step %d: selected %s, want %s", i, got, s.want)
				}
			}
		})
	}
}
