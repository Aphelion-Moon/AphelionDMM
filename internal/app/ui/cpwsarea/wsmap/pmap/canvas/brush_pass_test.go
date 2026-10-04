package canvas

import (
	"bytes"
	"image"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

func TestBrushDrawPassPreservesPixelsAndPainterOrder(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	textures := []uint32{
		platform.CreateTexture(&image.NRGBA{Pix: []byte{255, 0, 0, 255}, Stride: 4, Rect: image.Rect(0, 0, 1, 1)}),
		platform.CreateTexture(&image.NRGBA{Pix: []byte{0, 255, 0, 255}, Stride: 4, Rect: image.Rect(0, 0, 1, 1)}),
	}
	defer gl.DeleteTextures(int32(len(textures)), &textures[0])
	background := func() { brush.RectTexturedV(0, 0, 16, 16, 1, 1, 0, 1, 0, 0, 0, 1, 1) }
	base := func() {
		brush.RectTexturedV(2, 2, 12, 12, 1, 1, 1, 1, textures[0], 0, 0, 1, 1)
		brush.RectTexturedV(4, 4, 10, 10, 0, 0, 1, 1, 0, 0, 0, 1, 1)
		brush.RectTexturedV(6, 6, 9, 9, 1, 1, 1, 1, textures[0], 0, 0, 1, 1)
		brush.Line(0, 14, 15, 14, util.MakeColor(0, 0, 0, 1))
	}
	between := func() { brush.RectTexturedV(7, 3, 14, 7, 0, 1, 1, 1, 0, 0, 0, 1, 1) }
	front := func() { brush.RectTexturedV(8, 2, 14, 10, 1, 1, 1, 1, textures[1], 0, 0, 1, 1) }
	overlay := func() {
		brush.RectTexturedV(0, 0, 3, 3, 1, 1, 1, 1, 0, 0, 0, 1, 1)
		brush.RectTexturedV(11, 11, 15, 15, 1, 1, 1, 1, textures[1], 0, 0, 1, 1)
	}
	baseSubmission, frontSubmission := brush.CaptureSubmission(base), brush.CaptureSubmission(front)
	defer baseSubmission.Dispose()
	defer frontSubmission.Dispose()
	for _, camera := range []struct{ w, h, x, y, scale float32 }{{16, 16, 0, 0, 1}, {20, 18, 2, -1, 1.25}, {16, 16, 0, 0, 1}} {
		c.Process(imgui.Vec2{X: camera.w, Y: camera.h})
		reset := func() {
			gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
			gl.ClearColor(1, 0, 1, 1)
			gl.Clear(gl.COLOR_BUFFER_BIT)
		}
		reset()
		background()
		base()
		between()
		front()
		overlay()
		brush.Draw(camera.w, camera.h, camera.x, camera.y, camera.scale)
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		want := c.ReadPixels()
		reset()
		// Each pass must establish its own state, including the first untextured draw.
		gl.BindTexture(gl.TEXTURE_2D, textures[1])
		pass := brush.NewDrawPass(camera.w, camera.h, camera.x, camera.y, camera.scale)
		background()
		pass.DrawSubmission(baseSubmission)
		between()
		pass.DrawSubmission(frontSubmission)
		overlay()
		pass.Flush()
		pass.End()
		var program, vertexArray int32
		gl.GetIntegerv(gl.CURRENT_PROGRAM, &program)
		gl.GetIntegerv(gl.VERTEX_ARRAY_BINDING, &vertexArray)
		if program != 0 || vertexArray != 0 {
			t.Fatalf("draw pass left program=%d vertexArray=%d bound", program, vertexArray)
		}
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		got := c.ReadPixels()
		if !bytes.Equal(got, want) {
			t.Fatalf("shared draw changed pixels or painter order for camera %+v", camera)
		}
		if camera.scale == 1 {
			for _, pixel := range []struct {
				x, y int
				rgba [4]byte
			}{
				{1, 10, [4]byte{255, 255, 0, 255}},
				{3, 3, [4]byte{255, 0, 0, 255}},
				{4, 4, [4]byte{0, 0, 255, 255}},
				{6, 6, [4]byte{255, 0, 0, 255}},
				{7, 4, [4]byte{0, 255, 255, 255}},
				{8, 4, [4]byte{0, 255, 0, 255}},
				{1, 1, [4]byte{255, 255, 255, 255}},
			} {
				offset := 4 * (pixel.y*int(camera.w) + pixel.x)
				if !bytes.Equal(got[offset:offset+4], pixel.rgba[:]) {
					t.Fatalf("pixel (%d,%d) got %v want %v", pixel.x, pixel.y, got[offset:offset+4], pixel.rgba)
				}
			}
		}
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error after shared brush passes: 0x%x", code)
	}
}

func TestEmptyBrushDrawPassLeavesStateAndStreamUntouched(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	c.Process(imgui.Vec2{X: 4, Y: 4})
	submission := brush.CaptureSubmission(func() { brush.RectFilled(0, 0, 4, 4, util.MakeColor(1, 0, 0, 1)) })
	defer submission.Dispose()
	pass := brush.NewDrawPass(4, 4, 0, 0, 1)
	pass.DrawSubmission(submission)
	var program, vertexArray, texture int32
	gl.GetIntegerv(gl.CURRENT_PROGRAM, &program)
	gl.GetIntegerv(gl.VERTEX_ARRAY_BINDING, &vertexArray)
	gl.GetIntegerv(gl.TEXTURE_BINDING_2D, &texture)
	pass.End()
	gl.UseProgram(uint32(program))
	gl.BindVertexArray(uint32(vertexArray))
	empty := brush.NewDrawPass(4, 4, 0, 0, 1)
	empty.Flush()
	empty.End()
	var gotProgram, gotVertexArray, gotTexture int32
	gl.GetIntegerv(gl.CURRENT_PROGRAM, &gotProgram)
	gl.GetIntegerv(gl.VERTEX_ARRAY_BINDING, &gotVertexArray)
	gl.GetIntegerv(gl.TEXTURE_BINDING_2D, &gotTexture)
	gl.BindVertexArray(0)
	gl.UseProgram(0)
	if gotProgram != program || gotVertexArray != vertexArray || gotTexture != texture {
		t.Fatal("unused pass changed GL bindings")
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
	brush.RectFilled(0, 0, 4, 4, util.MakeColor(0, 0, 1, 1))
	empty.DrawSubmission(nil)
	empty.DrawSubmission(&brush.Submission{})
	empty.End()
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	checkResizePixels(t, c, imgui.Vec2{X: 4, Y: 4}, false)
	gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
	brush.Draw(4, 4, 0, 0, 1)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	if got := c.ReadPixels(); !bytes.Equal(got, bytes.Repeat([]byte{0, 0, 255, 255}, 16)) {
		t.Fatal("nil submission consumed the queued stream geometry")
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error after empty brush passes: 0x%x", code)
	}
}

func BenchmarkRetainedBrushDraw(b *testing.B) {
	for _, name := range []string{"Standalone", "Shared"} {
		b.Run(name, func(b *testing.B) {
			resizeContext(b)
			c := resizeCanvas(b)
			c.Process(imgui.Vec2{X: 64, Y: 64})
			gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
			b.Cleanup(func() { gl.BindFramebuffer(gl.FRAMEBUFFER, 0) })
			var submissions [64]*brush.Submission
			for i := range submissions {
				x, y := float32(i%8)*8, float32(i/8)*8
				submissions[i] = brush.CaptureSubmission(func() {
					brush.RectFilled(x, y, x+8, y+8, util.MakeColor(1, 0, 0, 1))
				})
				b.Cleanup(submissions[i].Dispose)
				submissions[i].Draw(64, 64, 0, 0, 1)
			}
			gl.Finish()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if name == "Shared" {
					pass := brush.NewDrawPass(64, 64, 0, 0, 1)
					for _, submission := range submissions {
						pass.DrawSubmission(submission)
					}
					pass.End()
				} else {
					for _, submission := range submissions {
						submission.Draw(64, 64, 0, 0, 1)
					}
				}
				gl.Finish()
			}
			b.StopTimer()
			if code := gl.GetError(); code != gl.NO_ERROR {
				b.Fatalf("GL error after retained brush draws: 0x%x", code)
			}
		})
	}
}
