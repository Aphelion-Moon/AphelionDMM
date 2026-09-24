package window_test

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/app/window"
	"sdmm/internal/platform"
	"testing"
)

func TestNativeCompletedFrameRepaintDoesNotReenterUIOrDrainJobs(t *testing.T) {
	_, _ = newMouseNetworkWorkspace(t)
	platform.InitImGuiGLFW()
	t.Cleanup(func() {
		lifecycleWindow.SetMouseButtonCallback(nil)
		lifecycleWindow.SetScrollCallback(nil)
		lifecycleWindow.SetKeyCallback(nil)
		lifecycleWindow.SetCharCallback(nil)
		lifecycleWindow.SetCursorPosCallback(nil)
	})
	t.Cleanup(platform.DisposeImGuiGLFW)
	platform.InitImGuiGL()
	t.Cleanup(platform.DisposeImGuiGL)
	frames := 0
	frame, repaint := window.FrameRepaintForTest(lifecycleWindow, traceFrameApp{draw: func() { frames++; imgui.Begin("Repaint probe"); imgui.Text("Completed frame"); imgui.End() }})
	if repaint(true) {
		t.Fatal("uninitialized draw data replayed")
	}
	frame()
	queued := false
	window.RunLater(func() { queued = true })
	if repaint(false) {
		t.Fatal("replayed while draw data can be under construction")
	}
	for range 3 {
		if !repaint(true) {
			t.Fatal("completed native draw data did not repaint")
		}
	}
	if frames != 1 || queued {
		t.Fatal("repaint reentered UI or retired resources")
	}
	if err := gl.GetError(); err != gl.NO_ERROR {
		t.Fatalf("repaint GL error %#x", err)
	}
	window.DrainFrameJobsForTest()
}
