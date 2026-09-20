package dialog

import (
	"runtime"
	"testing"
	"weak"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
)

type dialogPayload [8 << 20]byte

func openPayloadDialog(name string, closeInFrame bool) weak.Pointer[dialogPayload] {
	payload := new(dialogPayload)
	for i := 0; i < len(payload); i += 4096 {
		payload[i] = 1
	}
	Open(TypeCustom{Title: name, CloseButton: true, Layout: w.Layout{w.Custom(func() {
		runtime.KeepAlive(payload)
		if closeInFrame {
			imgui.CloseCurrentPopup()
		}
	})}})
	return weak.Make(payload)
}

func TestCloseReleasesDialogPayloadAndKeepsOtherDialogs(t *testing.T) {
	previous := opened
	opened = nil
	t.Cleanup(func() { opened = previous; shortcut.SetModalOpen(len(opened) != 0) })
	names := []string{"first", "middle", "last"}
	pointers := make([]weak.Pointer[dialogPayload], 0, len(names))
	for _, name := range names {
		pointers = append(pointers, openPayloadDialog(name, false))
	}
	runtime.GC()
	for _, pointer := range pointers {
		if pointer.Value() == nil {
			t.Fatal("an open dialog lost its payload")
		}
	}
	// Close the tail first: removing its visible slice entry must also remove
	// the registry's backing-array reference, without needing another Open.
	for _, index := range []int{2, 0, 1} {
		Close(TypeCustom{Title: names[index]})
		runtime.GC()
		if pointers[index].Value() != nil {
			t.Fatalf("closed %s dialog retains its 8 MiB payload", names[index])
		}
		for _, live := range opened {
			for i, name := range names {
				if live.Name() == name && pointers[i].Value() == nil {
					t.Fatalf("closing another dialog released live %s payload", name)
				}
			}
		}
	}
	if len(opened) != 0 {
		t.Fatal("closed dialogs remain registered")
	}
}

func TestProcessReleasesDismissedDialogPayload(t *testing.T) {
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
	io.Fonts().TextureDataRGBA32()
	pointer := openPayloadDialog("Close during frame", true)
	shortcut.BeginFrame()
	imgui.NewFrame()
	Process()
	imgui.EndFrame()
	shortcut.BeginFrame()
	if len(opened) != 0 || shortcut.BackgroundInputBlocked() {
		t.Fatal("dismissed dialog still owns modal input")
	}
	runtime.GC()
	if pointer.Value() != nil {
		t.Fatal("dismissed dialog retains its 8 MiB payload after Process returned")
	}
}
