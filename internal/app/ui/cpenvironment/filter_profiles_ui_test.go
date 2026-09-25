package cpenvironment

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
)

// The pinned ImGui implementation has one disabled-alpha backup. Nested
// disabled scopes overwrite it even when Begin/End calls are balanced. Exercise
// our real profile window, including the pending and read-only states that used
// to nest these scopes, rather than testing ImGui in isolation.
func TestProfileWindowPreservesApplicationAlpha(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, state := range []string{"idle", "compiling", "read-only"} {
		t.Run(state, func(t *testing.T) {
			ctx := imgui.CreateContext(nil)
			defer ctx.Destroy()
			io := imgui.CurrentIO()
			io.SetIniFilename("")
			io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
			io.SetDeltaTime(1.0 / 60)
			io.Fonts().TextureDataRGBA32()
			e, _ := profileFixture()
			e.filterCompilePending = state == "compiling"
			if state == "read-only" {
				e.filterProfileConfigError = "Invalid profile store"
			}
			for frame := range 5 {
				imgui.NewFrame()
				imgui.Begin("Environment")
				if frame == 0 {
					imgui.OpenPopup(filterProfilesPopup)
				}
				e.showFilterProfiles()
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
					t.Fatalf("%s frame %d changed unrelated text opacity: got %d, want 255", state, frame, alpha)
				}
			}
		})
	}
}
