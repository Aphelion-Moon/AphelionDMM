package shortcut

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestPopupOwnsCustomShortcutsAndDismissal(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	io.KeyMap(imgui.KeyEscape, int(glfw.KeyEscape))
	previous := shortcuts
	shortcuts = nil
	UseSettings(nil)
	t.Cleanup(func() { shortcuts = previous; UseSettings(nil); popupOpenBeforeFrame = false; modalOpen = false })
	var holder Shortcuts
	background, contextual := 0, 0
	holder.Add(Shortcut{Name: "pmap#edit", FirstKey: glfw.KeyEscape, IsVisible: true, Action: func() { background++ }})
	holder.Add(Shortcut{Name: "menu#DoCopy", FirstKey: glfw.KeyF6, IsVisible: true, Action: func() { contextual++ }})
	if err := SetBindings("menu#DoCopy", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	frame := func(open bool, close bool) {
		BeginFrame()
		imgui.NewFrame()
		Process() // Same ordering as the application's global dispatcher.
		imgui.Begin("Map host")
		if open {
			imgui.OpenPopup("Tile popup")
		}
		if imgui.BeginPopup("Tile popup") {
			imgui.Text("Context actions")
			ProcessPopup("menu#DoCopy")
			if close {
				imgui.CloseCurrentPopup()
			}
			imgui.EndPopup()
		}
		imgui.End()
		imgui.EndFrame()
	}
	frame(true, false)
	frame(false, false)
	io.KeyPress(int(glfw.KeyF8))
	frame(false, false)
	if contextual != 1 || background != 0 {
		t.Fatalf("popup routing: background=%d contextual=%d", background, contextual)
	}
	io.KeyRelease(int(glfw.KeyF8))
	frame(false, false)
	io.KeyPress(int(glfw.KeyEscape))
	frame(false, true)
	if background != 0 {
		t.Fatal("popup dismissal leaked Escape to the map")
	}
	io.KeyRelease(int(glfw.KeyEscape))
	frame(false, false)
	io.KeyPress(int(glfw.KeyEscape))
	frame(false, false)
	if background != 1 {
		t.Fatal("map shortcut did not resume after popup closed")
	}
}

func TestCustomShortcutRepeatAndTextFocus(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	io.SetDeltaTime(0.1)
	previous := shortcuts
	shortcuts = nil
	UseSettings(nil)
	t.Cleanup(func() { shortcuts = previous; UseSettings(nil); popupOpenBeforeFrame = false; modalOpen = false })
	count := 0
	var holder Shortcuts
	holder.Add(Shortcut{Name: "pmap#pan", FirstKey: glfw.KeyRight, IsVisible: true, Action: func() { count++ }})
	if err := SetBindings("pmap#pan", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	io.KeyPress(int(glfw.KeyF8))
	for range 10 {
		BeginFrame()
		imgui.NewFrame()
		Process()
		imgui.EndFrame()
	}
	if count < 2 {
		t.Fatal("holding a rebound action did not repeat")
	}
	io.KeyRelease(int(glfw.KeyF8))
	BeginFrame()
	imgui.NewFrame()
	imgui.EndFrame()
	value := ""
	for frame := 0; frame < 3; frame++ {
		BeginFrame()
		imgui.NewFrame()
		imgui.Begin("Input")
		if frame == 0 {
			imgui.SetKeyboardFocusHere()
		}
		imgui.InputText("Value", &value)
		imgui.End()
		imgui.EndFrame()
	}
	before := count
	io.KeyPress(int(glfw.KeyF8))
	BeginFrame()
	imgui.NewFrame()
	if !imgui.IsAnyItemActive() {
		t.Fatal("input fixture has no active field")
	}
	Process()
	imgui.Begin("Input")
	imgui.InputText("Value", &value)
	imgui.End()
	imgui.EndFrame()
	if count != before {
		t.Fatal("rebound shortcut stole a text field's key")
	}
}
