package menu

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/shortcut"
)

func TestShortcutEditorConflictConsentAndRendering(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	shortcut.UseSettings(nil)
	defer shortcut.UseSettings(nil)
	var bindings shortcut.Shortcuts
	bindings.Add(shortcut.Shortcut{Name: "menu#DoSave", FirstKey: glfw.KeyF6})
	bindings.Add(shortcut.Shortcut{Name: "menu#DoOpen", FirstKey: glfw.KeyF7})
	defer bindings.Dispose()
	m := &Menu{showHotkeys: true, hotkeyAction: "menu#DoSave", hotkeyDraft: "F7"}
	m.applyShortcutEdit()
	if m.hotkeyError == "" || shortcut.Label("menu#DoSave") != "F6" {
		t.Fatal("conflict was applied without acknowledgment")
	}
	m.hotkeyAllowShared = true
	m.applyShortcutEdit()
	if m.hotkeyError != "" || shortcut.Label("menu#DoSave") != "F7" {
		t.Fatal("acknowledged binding did not apply")
	}
	m.hotkeyDraft = "unknown"
	m.applyShortcutEdit()
	if m.hotkeyError == "" || shortcut.Label("menu#DoSave") != "F7" {
		t.Fatal("invalid edit replaced last valid binding")
	}
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 900, Y: 700})
	io.Fonts().TextureDataRGBA32()
	for range 2 {
		imgui.NewFrame()
		m.showShortcutReference()
		imgui.Render()
	}
	if len(imgui.RenderedDrawData().CommandLists()) == 0 {
		t.Fatal("shortcut editor produced no UI draw data")
	}
}
