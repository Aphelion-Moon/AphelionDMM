package window_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/window"
)

var lifecycleWindow *glfw.Window

// The renderer caches process-wide GL objects, so all repetitions share one
// context, as in the canvas/workspace native fixtures.
func TestMain(m *testing.M) {
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" {
		os.Exit(m.Run())
	}
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	glfw.WindowHint(glfw.Visible, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	win, err := glfw.CreateWindow(64, 64, "Canvas lifecycle verification", nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		glfw.Terminate()
		os.Exit(1)
	}
	win.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		win.Destroy()
		glfw.Terminate()
		os.Exit(1)
	}
	lifecycleWindow = win
	glfw.DetachCurrentContext()
	code := m.Run()
	win.MakeContextCurrent()
	window.DrainFrameJobsForTest()
	brush.Dispose()
	win.Destroy()
	glfw.Terminate()
	os.Exit(code)
}

func TestCanvasDeferredDisposalLifecycle(t *testing.T) {
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native canvas lifetime checks")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	window.DrainFrameJobsForTest()
	t.Cleanup(window.DrainFrameJobsForTest)
	for cycle := range 32 {
		c := canvas.New()
		c.Process(imgui.Vec2{X: 8, Y: 8})
		texture, pixels := c.Texture(), c.ReadPixels()
		if texture == 0 || !gl.IsTexture(texture) || len(pixels) != 8*8*4 {
			t.Fatal("fixture did not create a readable texture")
		}
		c.Dispose()
		c.Dispose()
		if window.PendingFrameJobsForTest() != 1 {
			t.Fatal("duplicate Dispose queued duplicate resource deletion")
		}
		// Screenshot readback after scheduling disposal remains valid until the
		// next frame. Late Process calls may not resize/recreate that texture.
		c.Process(imgui.Vec2{X: 16, Y: 16})
		if c.Texture() != texture || !bytes.Equal(c.ReadPixels(), pixels) || !gl.IsTexture(texture) {
			t.Fatal("pending disposal changed same-frame screenshot pixels")
		}
		window.DrainFrameJobsForTest()
		if c.Texture() != 0 || gl.IsTexture(texture) || len(c.ReadPixels()) != 0 {
			t.Fatal("drained disposal retained a live or exposed deleted texture")
		}
		c.Process(imgui.Vec2{X: 32, Y: 32})
		if c.Texture() != 0 {
			t.Fatal("late render recreated a disposed texture")
		}
		other := canvas.New()
		other.Process(imgui.Vec2{X: 4, Y: 4})
		otherTexture := other.Texture()
		c.Dispose()
		if window.PendingFrameJobsForTest() != 0 {
			t.Fatal("completed disposal was queued again")
		}
		window.DrainFrameJobsForTest()
		if !gl.IsTexture(otherTexture) {
			t.Fatal("old disposal deleted another canvas texture")
		}
		other.Dispose()
		window.DrainFrameJobsForTest()
		if gl.IsTexture(otherTexture) || window.PendingFrameJobsForTest() != 0 {
			t.Fatal("cycle left resources or cleanup jobs")
		}
		if code := gl.GetError(); code != gl.NO_ERROR {
			t.Fatalf("cycle %d GL error: 0x%x", cycle, code)
		}
	}
}
