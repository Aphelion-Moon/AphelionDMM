package window_test

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/platform"
)

func nativeImGuiRenderer(t testing.TB) (int, int) {
	t.Helper()
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
	return width, height
}

func rendererOffsetDrawData(reverse bool) imgui.DrawData {
	imgui.NewFrame()
	list := imgui.BackgroundDrawList()
	left, right := imgui.Vec4{X: 1, W: 1}, imgui.Vec4{Y: 1, W: 1}
	if reverse {
		left, right = right, left
	}
	// Distinct clip rectangles force separate commands in the same index
	// buffer. The second draw must use a nonzero element-buffer byte offset.
	list.PushClipRect(imgui.Vec2{}, imgui.Vec2{X: 32, Y: 32})
	list.AddRectFilled(imgui.Vec2{X: 4, Y: 4}, imgui.Vec2{X: 28, Y: 28}, imgui.PackedColorFromVec4(left))
	list.PopClipRect()
	list.PushClipRect(imgui.Vec2{X: 32}, imgui.Vec2{X: 64, Y: 32})
	list.AddRectFilled(imgui.Vec2{X: 36, Y: 4}, imgui.Vec2{X: 60, Y: 28}, imgui.PackedColorFromVec4(right))
	list.PopClipRect()
	imgui.Render()
	return imgui.RenderedDrawData()
}

func TestImGuiRendererBufferOffsets(t *testing.T) {
	_, height := nativeImGuiRenderer(t)
	data := rendererOffsetDrawData(false)
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

func TestImGuiRendererConsecutiveFramesPreserveState(t *testing.T) {
	width, height := nativeImGuiRenderer(t)
	var vao, arrayBuffer, elementBuffer uint32
	var textures, samplers [2]uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenBuffers(1, &arrayBuffer)
	gl.GenBuffers(1, &elementBuffer)
	gl.GenTextures(2, &textures[0])
	gl.GenSamplers(2, &samplers[0])
	program, err := platform.NewShaderProgram(
		"#version 330 core\nvoid main() { gl_Position = vec4(0.0); }\x00",
		"#version 330 core\nout vec4 Color; void main() { Color = vec4(1.0); }\x00")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		gl.UseProgram(0)
		gl.BindVertexArray(0)
		gl.BindBuffer(gl.ARRAY_BUFFER, 0)
		gl.DeleteVertexArrays(1, &vao)
		gl.DeleteBuffers(1, &arrayBuffer)
		gl.DeleteBuffers(1, &elementBuffer)
		gl.DeleteTextures(2, &textures[0])
		gl.DeleteSamplers(2, &samplers[0])
		gl.DeleteProgram(program)
		gl.ActiveTexture(gl.TEXTURE0)
		gl.Disable(gl.BLEND)
		gl.Disable(gl.CULL_FACE)
		gl.Disable(gl.DEPTH_TEST)
		gl.Disable(gl.SCISSOR_TEST)
		gl.PolygonMode(gl.FRONT_AND_BACK, gl.FILL)
		gl.Viewport(0, 0, int32(width), int32(height))
	})
	for frame, size := range []imgui.Vec2{{X: float32(width), Y: float32(height)}, {X: 96, Y: 64}, {X: float32(width), Y: float32(height)}} {
		if frame == 2 {
			platform.DisposeImGuiGL()
			platform.InitImGuiGL()
		}
		imgui.CurrentIO().SetDisplaySize(size)
		data := rendererOffsetDrawData(frame == 1)
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		gl.DrawBuffer(gl.BACK)
		gl.ReadBuffer(gl.BACK)
		gl.Disable(gl.SCISSOR_TEST)
		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		gl.BindVertexArray(vao)
		gl.BindBuffer(gl.ARRAY_BUFFER, arrayBuffer)
		gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, elementBuffer)
		gl.UseProgram(program)
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, textures[0])
		gl.BindSampler(0, samplers[0])
		gl.ActiveTexture(gl.TEXTURE3)
		gl.BindTexture(gl.TEXTURE_2D, textures[1])
		gl.BindSampler(3, samplers[1])
		gl.Enable(gl.CULL_FACE)
		gl.Enable(gl.DEPTH_TEST)
		gl.Enable(gl.SCISSOR_TEST)
		gl.Disable(gl.BLEND)
		gl.BlendEquationSeparate(gl.FUNC_SUBTRACT, gl.FUNC_REVERSE_SUBTRACT)
		gl.BlendFuncSeparate(gl.DST_COLOR, gl.ONE_MINUS_DST_COLOR, gl.DST_ALPHA, gl.ONE_MINUS_DST_ALPHA)
		gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)
		gl.Viewport(2, 3, 39, 44)
		gl.Scissor(1, 2, 33, 30)
		platform.Render(data)

		for name, want := range map[uint32][]int32{
			gl.CURRENT_PROGRAM: {int32(program)}, gl.ACTIVE_TEXTURE: {gl.TEXTURE3},
			gl.TEXTURE_BINDING_2D: {int32(textures[1])}, gl.SAMPLER_BINDING: {int32(samplers[1])},
			gl.VERTEX_ARRAY_BINDING: {int32(vao)}, gl.ARRAY_BUFFER_BINDING: {int32(arrayBuffer)},
			gl.ELEMENT_ARRAY_BUFFER_BINDING: {int32(elementBuffer)},
			// Core OpenGL has one polygon mode for both faces.
			gl.POLYGON_MODE: {gl.LINE}, gl.VIEWPORT: {2, 3, 39, 44}, gl.SCISSOR_BOX: {1, 2, 33, 30},
			gl.BLEND_SRC_RGB: {gl.DST_COLOR}, gl.BLEND_DST_RGB: {gl.ONE_MINUS_DST_COLOR},
			gl.BLEND_SRC_ALPHA: {gl.DST_ALPHA}, gl.BLEND_DST_ALPHA: {gl.ONE_MINUS_DST_ALPHA},
			gl.BLEND_EQUATION_RGB: {gl.FUNC_SUBTRACT}, gl.BLEND_EQUATION_ALPHA: {gl.FUNC_REVERSE_SUBTRACT},
		} {
			var got [4]int32
			gl.GetIntegerv(name, &got[0])
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("frame %d state %#x: got %v want %v", frame, name, got, want)
				}
			}
		}
		if !gl.IsEnabled(gl.CULL_FACE) || !gl.IsEnabled(gl.DEPTH_TEST) || !gl.IsEnabled(gl.SCISSOR_TEST) || gl.IsEnabled(gl.BLEND) {
			t.Errorf("frame %d changed enabled modes", frame)
		}
		gl.ActiveTexture(gl.TEXTURE0)
		for name, want := range map[uint32]uint32{gl.TEXTURE_BINDING_2D: textures[0], gl.SAMPLER_BINDING: samplers[0]} {
			var got int32
			gl.GetIntegerv(name, &got)
			if uint32(got) != want {
				t.Errorf("frame %d changed texture-unit-zero state %#x: got %d want %d", frame, name, got, want)
			}
		}
		for index, x := range []int32{16, 48} {
			want := [4]byte{0, 0, 0, 255}
			want[(index+frame%2)%2] = 255
			var pixel [4]byte
			gl.ReadPixels(x, int32(size.Y)-17, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&pixel[0]))
			if pixel != want {
				t.Errorf("frame %d pixel %d: got %v want %v", frame, index, pixel, want)
			}
		}
		if code := gl.GetError(); code != gl.NO_ERROR {
			t.Fatalf("frame %d renderer GL error: %#x", frame, code)
		}
	}
}

func BenchmarkImGuiRendererFrame(b *testing.B) {
	nativeImGuiRenderer(b)
	data := rendererOffsetDrawData(false)
	platform.Render(data)
	gl.Finish()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		platform.Render(data)
		gl.Finish()
	}
}
