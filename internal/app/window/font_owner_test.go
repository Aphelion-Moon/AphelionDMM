package window_test

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"

	"sdmm/internal/app/window"
)

// Monospace and micro fonts belong to the context that built them; a later
// context must not receive the old handles (pushing one asserts in imgui).
func TestThemeFontsBelongToTheirContext(t *testing.T) {
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native font checks")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)

	first := imgui.CreateContext(nil)
	window.SetPointSize(1)
	if _, ok := window.MonoFont(); !ok {
		t.Fatal("the building context cannot use its monospace font")
	}
	first.Destroy()

	second := imgui.CreateContext(nil)
	defer second.Destroy()
	if _, ok := window.MonoFont(); ok {
		t.Fatal("a new context received the previous context's monospace font")
	}
	if _, ok := window.MicroFont(); ok {
		t.Fatal("a new context received the previous context's micro font")
	}
}
