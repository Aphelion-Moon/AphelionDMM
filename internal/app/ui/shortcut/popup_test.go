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
	holder.Add(Shortcut{Name: "pmap#pan", FirstKey: glfw.KeyRight, IsVisible: true, AllowWhenItemActive: func() bool { return true }, Action: func() { count++ }})
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
	if !io.WantTextInput() {
		t.Fatal("active text input did not claim text ownership")
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

func TestExplicitShortcutCanRunDuringWidgetGesture(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	previous := shortcuts
	shortcuts = nil
	UseSettings(nil)
	t.Cleanup(func() { shortcuts = previous; UseSettings(nil); popupOpenBeforeFrame = false; modalOpen = false })
	blocked, allowed := 0, 0
	var holder Shortcuts
	holder.Add(Shortcut{Name: "pmap#regular", FirstKey: glfw.KeyF8, IsVisible: true, Action: func() { blocked++ }})
	holder.Add(Shortcut{Name: "pmap#gesture", FirstKey: glfw.KeyF9, IsVisible: true, AllowWhenItemActive: func() bool { return true }, Action: func() { allowed++ }})
	frame := func() imgui.Vec2 {
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 200, Y: 100})
		imgui.BeginV("Gesture owner", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize)
		imgui.Button("Hold this widget")
		point := imgui.ItemRectMin().Plus(imgui.ItemRectMax()).Times(0.5)
		imgui.End()
		imgui.EndFrame()
		return point
	}
	point := frame()
	io.SetMousePosition(point)
	io.SetMouseButtonDown(0, true)
	frame()
	if !imgui.IsAnyItemActive() {
		t.Fatal("mouse gesture did not activate the widget")
	}
	press := func(key glfw.Key) {
		io.KeyPress(int(key))
		BeginFrame()
		imgui.NewFrame()
		if !imgui.IsAnyItemActive() {
			t.Fatal("widget lost active input ownership")
		}
		Process()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 200, Y: 100})
		imgui.BeginV("Gesture owner", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize)
		imgui.Button("Hold this widget")
		imgui.End()
		imgui.EndFrame()
		io.KeyRelease(int(key))
	}
	press(glfw.KeyF8)
	press(glfw.KeyF9)
	if blocked != 0 || allowed != 1 {
		t.Fatalf("active widget routing: regular=%d explicit-gesture=%d", blocked, allowed)
	}
	io.SetMouseButtonDown(0, false)
}
