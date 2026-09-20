package window_test

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/platform"
)

func TestImGuiRendererBufferOffsets(t *testing.T) {
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native renderer checks")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	ctx := imgui.CreateContext(nil)
	t.Cleanup(ctx.Destroy)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	width, height := lifecycleWindow.GetFramebufferSize()
	io.SetDisplaySize(imgui.Vec2{X: float32(width), Y: float32(height)})
	io.SetDisplayFrameBufferScale(imgui.Vec2{X: 1, Y: 1})
	io.SetDeltaTime(1.0 / 60)
	platform.InitImGuiGL()
	t.Cleanup(platform.DisposeImGuiGL)

	imgui.NewFrame()
	list := imgui.BackgroundDrawList()
	// Distinct clip rectangles force separate commands in the same index
	// buffer. The second draw must use a nonzero element-buffer byte offset.
	list.PushClipRect(imgui.Vec2{}, imgui.Vec2{X: 32, Y: 32})
	list.AddRectFilled(imgui.Vec2{X: 4, Y: 4}, imgui.Vec2{X: 28, Y: 28}, imgui.PackedColorFromVec4(imgui.Vec4{X: 1, W: 1}))
	list.PopClipRect()
	list.PushClipRect(imgui.Vec2{X: 32}, imgui.Vec2{X: 64, Y: 32})
	list.AddRectFilled(imgui.Vec2{X: 36, Y: 4}, imgui.Vec2{X: 60, Y: 28}, imgui.PackedColorFromVec4(imgui.Vec4{Y: 1, W: 1}))
	list.PopClipRect()
	imgui.Render()
	data := imgui.RenderedDrawData()
	commands := 0
	for _, drawList := range data.CommandLists() {
		for _, cmd := range drawList.Commands() {
			if cmd.ElementCount() != 0 {
				commands++
			}
		}
	}
	if commands < 2 {
		t.Fatal("fixture did not generate multiple indexed draws")
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.DrawBuffer(gl.BACK)
	gl.ReadBuffer(gl.BACK)
	gl.Disable(gl.SCISSOR_TEST)
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	platform.Render(data)
	// Read the actual ImGui output before swapping the hidden window's buffers.
	// Interior pixels avoid rasterization-edge differences between drivers.
	for _, sample := range []struct {
		x    int32
		want [4]byte
	}{
		{16, [4]byte{255, 0, 0, 255}},
		{48, [4]byte{0, 255, 0, 255}},
		{80, [4]byte{0, 0, 0, 255}},
	} {
		var pixel [4]byte
		gl.ReadPixels(sample.x, int32(height)-17, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&pixel[0]))
		if pixel != sample.want {
			t.Errorf("pixel at x=%d: got %v, want %v", sample.x, pixel, sample.want)
		}
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("renderer GL error: 0x%x", code)
	}
}
