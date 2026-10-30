package dialog

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
)

// Dialogs with the same name share one ImGui popup ID. Rendering both in one
// frame appends the second into the first's window with duplicate widget IDs,
// so a repeated request (such as pressing the window close button again)
// stacked prompts that answered together. The later dialog must wait its turn.
func TestSameNameDialogsAreShownOneAtATime(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	previous := opened
	opened = nil
	t.Cleanup(func() { opened = previous; shortcut.SetModalOpen(len(opened) != 0) })
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()

	var rendered []string
	var appearing [2]int
	closeFirst := false
	open := func(index int, label string) {
		Open(TypeCustom{Title: "Save Workspace?", Layout: w.Layout{w.Custom(func() {
			rendered = append(rendered, label)
			if imgui.IsWindowAppearing() {
				appearing[index]++
			}
			if index == 0 && closeFirst {
				imgui.CloseCurrentPopup()
			}
		})}})
	}
	open(0, "first")
	open(1, "second")
	if !IsOpen("Save Workspace?") || IsOpen("Another dialog") {
		t.Fatal("IsOpen does not report queued dialogs by name")
	}
	frame := func() []string {
		rendered = nil
		imgui.NewFrame()
		Process()
		imgui.Render()
		return rendered
	}
	for i := range 4 {
		if got := frame(); len(got) != 1 || got[0] != "first" {
			t.Fatalf("frame %d rendered %v, want only the first same-name dialog", i, got)
		}
	}
	if appearing[0] != 1 {
		t.Fatalf("first dialog appeared %d times, want 1", appearing[0])
	}
	closeFirst = true
	frame()
	closeFirst = false
	if len(opened) != 1 {
		t.Fatalf("closing the shown dialog left %d dialogs, want the queued one", len(opened))
	}
	for i := range 3 {
		if got := frame(); len(got) != 1 || got[0] != "second" {
			t.Fatalf("frame %d after close rendered %v, want only the second dialog", i, got)
		}
	}
	if appearing[1] != 1 {
		t.Fatalf("queued dialog appeared %d times, want 1", appearing[1])
	}
}
