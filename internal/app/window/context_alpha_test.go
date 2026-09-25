package window_test

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"runtime"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/window"
	"testing"
)

func TestCanvasOptionalTransparencyPreservesLockedContext(t *testing.T) {
	if lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native context alpha")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lifecycleWindow.MakeContextCurrent()
	defer glfw.DetachCurrentContext()
	c := canvas.New()
	defer func() { c.Dispose(); window.DrainFrameJobsForTest() }()
	for _, transparent := range []bool{false, true, false} {
		c.SetTransparent(transparent)
		c.Process(imgui.Vec2{X: 8, Y: 8})
		pixels := c.ReadPixels()
		want := byte(255)
		if transparent {
			want = 0
		}
		if len(pixels) != 8*8*4 || pixels[3] != want {
			t.Fatalf("transparent=%t alpha=%d want=%d", transparent, pixels[3], want)
		}
	}
}
