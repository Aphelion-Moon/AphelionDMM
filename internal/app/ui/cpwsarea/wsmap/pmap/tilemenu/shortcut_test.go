package tilemenu

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmclip"
)

type popupApp struct {
	App
	commands  *command.Storage
	clipboard *dmmclip.Clipboard
}

func (a *popupApp) CommandStorage() *command.Storage { return a.commands }
func (a *popupApp) Clipboard() *dmmclip.Clipboard    { return a.clipboard }

func TestTileMenuRoutesCustomCopyAndCloseInsidePopup(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 800, Y: 600})
	io.Fonts().TextureDataRGBA32()
	shortcut.UseSettings(nil)
	defer shortcut.UseSettings(nil)
	m := New(&popupApp{commands: command.NewStorage(), clipboard: dmmclip.New()}, nil)
	defer m.Dispose()
	var commands shortcut.Shortcuts
	defer commands.Dispose()
	copyCount, behindCount := 0, 0
	commands.Add(shortcut.Shortcut{Name: "menu#DoCopy", FirstKey: glfw.KeyC, IsVisible: true, Action: func() { copyCount++ }})
	commands.Add(shortcut.Shortcut{Name: "pmap#behind", FirstKey: glfw.KeyF8, IsVisible: true, Action: func() { behindCount++ }})
	if err := shortcut.SetBindings("menu#DoCopy", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := shortcut.SetBindings("tileMenu#close", [][][2]glfw.Key{{{glfw.KeyF9, 0}}}); err != nil {
		t.Fatal(err)
	}
	frame := func(open bool) {
		shortcut.BeginFrame()
		imgui.NewFrame()
		shortcut.Process()
		imgui.Begin("Map")
		if open {
			m.tile = &dmmap.Tile{}
			m.opened = true
			imgui.OpenPopup("tileMenu")
		}
		m.Process()
		imgui.End()
		imgui.EndFrame()
	}
	frame(true)
	frame(false)
	io.KeyPress(int(glfw.KeyF8))
	frame(false)
	if copyCount != 1 || behindCount != 0 {
		t.Fatalf("tile menu route: copy=%d behind=%d", copyCount, behindCount)
	}
	io.KeyRelease(int(glfw.KeyF8))
	frame(false)
	io.KeyPress(int(glfw.KeyF9))
	frame(false)
	if m.opened || imgui.IsPopupOpenV("", imgui.PopupFlagsAnyPopup) {
		t.Fatal("custom close did not close both tile state and native popup")
	}
	io.KeyRelease(int(glfw.KeyF9))
	frame(false)
	io.KeyPress(int(glfw.KeyF8))
	frame(false)
	if copyCount != 2 {
		t.Fatal("global binding did not resume after context menu closed")
	}
}
