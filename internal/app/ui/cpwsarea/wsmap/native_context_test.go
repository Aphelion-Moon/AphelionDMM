package wsmap

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/render/brush"
)

var workspaceWindow *glfw.Window

// Keep one native context for serial tests and benchmark calibration passes.
// Brush caches are process scoped, so recreating the context between benchmark
// invocations would give them resource names from a destroyed context.
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
	win, err := glfw.CreateWindow(64, 64, "Workspace verification", nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		glfw.Terminate()
		os.Exit(1)
	}
	win.MakeContextCurrent()
	if err = gl.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		win.Destroy()
		glfw.Terminate()
		os.Exit(1)
	}
	workspaceWindow = win
	glfw.DetachCurrentContext()
	code := m.Run()
	win.MakeContextCurrent()
	brush.Dispose()
	win.Destroy()
	glfw.Terminate()
	os.Exit(code)
}

func workspaceContext(tb testing.TB) {
	tb.Helper()
	if workspaceWindow == nil {
		tb.Skip("set APHELIONDMM_GL_TEST=1 for native workspace verification")
	}
	runtime.LockOSThread()
	tb.Cleanup(runtime.UnlockOSThread)
	workspaceWindow.MakeContextCurrent()
	tb.Cleanup(glfw.DetachCurrentContext)
	tb.Logf("GL renderer=%s version=%s", gl.GoStr(gl.GetString(gl.RENDERER)), gl.GoStr(gl.GetString(gl.VERSION)))
}
