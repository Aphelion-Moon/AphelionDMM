package wsmap

import (
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/app/ui/shortcut"
)

type shortcutModal struct{}

func (shortcutModal) Name() string         { return "Shortcut modal verification" }
func (shortcutModal) HasCloseButton() bool { return true }
func (shortcutModal) Process()             { imgui.Text("Map commands must pause here") }

func TestSelectionShortcutsRespectPendingAndOpenModal(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	defer shortcut.ResetBindings("pmap#nudgeSelectionRight")
	if err := shortcut.SetBindings("pmap#nudgeSelectionRight", [][][2]glfw.Key{{{glfw.KeyF8, 0}}}); err != nil {
		t.Fatal(err)
	}
	initialHash := resizeHash(t, resizeSnapshot(t, ws.Map().Editor()))
	area := grab.Bounds()
	modal := shortcutModal{}
	dialog.Open(modal) // Not rendered yet: the next frame's dispatcher runs first.
	defer dialog.Close(modal)
	io := imgui.CurrentIO()
	for frame := 0; frame < 4; frame++ {
		if frame%2 == 0 {
			io.KeyPress(int(glfw.KeyF8))
		} else {
			io.KeyRelease(int(glfw.KeyF8))
		}
		shortcut.BeginFrame()
		imgui.NewFrame()
		shortcut.Process()
		dialog.Process()
		if frame == 3 && imgui.BeginPopupModalV(modal.Name(), nil, imgui.WindowFlagsNone) {
			imgui.CloseCurrentPopup()
			imgui.EndPopup()
		}
		imgui.EndFrame()
		if resizeHash(t, resizeSnapshot(t, ws.Map().Editor())) != initialHash || grab.Bounds() != area {
			t.Fatalf("modal frame %d changed map or selection", frame)
		}
	}
	dialog.Close(modal)
	shortcut.BeginFrame()
	pressSelectionShortcut(glfw.KeyF8)
	if grab.Bounds() == area {
		t.Fatal("custom map shortcut did not resume after modal closed")
	}
}
