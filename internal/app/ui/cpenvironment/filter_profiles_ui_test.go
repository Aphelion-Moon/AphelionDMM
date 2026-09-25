package cpenvironment

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/app/ui/shortcut"
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
					e.openFilterProfiles()
					defer func() {
						if e.filterProfileDialog != nil {
							dialog.Close(e.filterProfileDialog)
						}
					}()
				}
				imgui.End()
				dialog.Process()
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

func TestProfileDialogOutlivesPanelAndDismisses(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	io.SetDeltaTime(1.0 / 60)
	io.KeyMap(imgui.KeyEscape, int(glfw.KeyEscape))
	io.Fonts().TextureDataRGBA32()
	e, a := profileFixture()
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	var button imgui.Vec2
	frame := func(panel bool) {
		shortcut.BeginFrame()
		imgui.NewFrame()
		if panel {
			imgui.SetNextWindowPos(imgui.Vec2{})
			imgui.SetNextWindowSize(imgui.Vec2{X: 200, Y: 500})
			imgui.Begin("Environment")
			button = imgui.CursorScreenPos().Plus(imgui.Vec2{X: 10, Y: 8})
			e.showFilterProfiles()
			imgui.End()
		}
		dialog.Process()
		imgui.Render()
	}
	for range 2 {
		frame(true)
		frame(true)
		io.SetMousePosition(button)
		io.SetMouseButtonDown(0, true)
		frame(true)
		io.SetMouseButtonDown(0, false)
		frame(true)
		frame(true)
		if !shortcut.BackgroundInputBlocked() {
			t.Fatal("click did not open the actual Profiles dialog")
		}
		frame(false)
		if len(imgui.RenderedDrawData().CommandLists()) == 0 {
			t.Fatal("open profile modal disappeared when the Environment panel was hidden")
		}
		io.KeyPress(int(glfw.KeyEscape))
		frame(false)
		io.KeyRelease(int(glfw.KeyEscape))
		frame(false)
		frame(false)
		if shortcut.BackgroundInputBlocked() {
			t.Fatal("Profiles retained input ownership after keyboard dismissal")
		}
	}
}
