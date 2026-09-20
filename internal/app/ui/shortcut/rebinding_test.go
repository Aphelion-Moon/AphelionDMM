package shortcut

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/hotkeys"
)

func TestRebindingRealDispatcherAndPaneLifetime(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := shortcuts
	shortcuts = nil
	UseSettings(&hotkeys.Settings{})
	t.Cleanup(func() { shortcuts = previous; UseSettings(nil) })
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	counts := [3]int{}
	panes := [3]Shortcuts{}
	for i := range panes {
		panes[i].Add(Shortcut{Name: "pmap#action", FirstKey: glfw.KeyF6, IsVisible: i != 0, IsEnabled: func() bool { return i != 1 }, Action: func() { counts[i]++ }})
	}
	chords, err := hotkeys.Parse("Ctrl+K; Alt+F8")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetBindings("pmap#action", chords); err != nil {
		t.Fatal(err)
	}
	press := func(keys ...glfw.Key) {
		for _, key := range keys {
			io.KeyPress(int(key))
		}
		imgui.NewFrame()
		Process()
		imgui.EndFrame()
		for _, key := range keys {
			io.KeyRelease(int(key))
		}
		imgui.NewFrame()
		Process()
		imgui.EndFrame()
	}
	press(glfw.KeyF6)
	press(glfw.KeyRightControl, glfw.KeyK)
	press(glfw.KeyLeftControl, glfw.KeyLeftShift, glfw.KeyK)
	press(glfw.KeyRightAlt, glfw.KeyF8)
	if counts != [3]int{0, 0, 2} {
		t.Fatalf("dispatcher did not honor effective chords and enabled pane: %v", counts)
	}
	if len(Reference()) != 2 {
		t.Fatalf("reference does not show both replacements: %v", Reference())
	}
	panes[2].Dispose()
	var reopened Shortcuts
	reopened.Add(Shortcut{Name: "pmap#action", FirstKey: glfw.KeyF6, IsVisible: true, Action: func() { counts[2]++ }})
	press(glfw.KeyRightControl, glfw.KeyK)
	if counts[2] != 3 {
		t.Fatal("reopened pane lost custom binding")
	}
	if err := SetBindings("pmap#action", nil); err != nil {
		t.Fatal(err)
	}
	press(glfw.KeyRightControl, glfw.KeyK)
	if counts[2] != 3 {
		t.Fatal("disabled action fired")
	}
	ResetBindings("pmap#action")
	press(glfw.KeyF6)
	if counts[2] != 4 {
		t.Fatal("reset did not restore defaults")
	}
}
