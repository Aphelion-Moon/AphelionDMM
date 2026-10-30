package pquickedit

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
)

// The pinned ImGui (1.85) keeps one disabled-alpha backup that every
// BeginDisabled call overwrites, even inside an already disabled scope. A
// nested scope therefore leaves Style.Alpha multiplied by DisabledAlpha after
// balanced Begin/End calls. The map pane draws this panel every frame while an
// instance is selected, so a leak fades every later window, dialogs included,
// to nothing within a few frames. Selecting an unknown-type instance with the
// Pick tool reached exactly that path.
func TestQuickEditUnknownTypePreservesApplicationAlpha(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for name, objects := range map[string]map[string]*dmenv.Object{
		"unknown type": {},
		"known type":   {"/obj/audit_unknown": {Path: "/obj/audit_unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := imgui.CreateContext(nil)
			defer ctx.Destroy()
			io := imgui.CurrentIO()
			io.SetIniFilename("")
			io.SetDisplaySize(imgui.Vec2{X: 600, Y: 400})
			io.SetDeltaTime(1.0 / 60)
			io.Fonts().TextureDataRGBA32()
			dmmap.PrefabStorage.Free()
			defer dmmap.PrefabStorage.Free()
			instance := unknownQuickEditInstance()
			panel := New(&capturePanelApp{environment: &dmenv.Dme{Objects: objects}}, &capturePanelEditor{})
			for frame := range 5 {
				imgui.NewFrame()
				imgui.Begin("Map pane quick edit")
				panel.ProcessV(instance)
				imgui.End()
				imgui.Begin("Unrelated application panel")
				imgui.TextColored(imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, "Legible text")
				vertices, size := imgui.WindowDrawList().VertexBuffer()
				stride, _, _, colorOffset := imgui.VertexBufferLayout()
				var alpha byte
				if size >= stride {
					alpha = unsafe.Slice((*byte)(vertices), size)[size-stride+colorOffset+3]
				}
				imgui.End()
				imgui.Render()
				if alpha != 255 {
					t.Fatalf("frame %d: quick edit faded unrelated text: alpha %d, want 255", frame, alpha)
				}
			}
		})
	}
}
