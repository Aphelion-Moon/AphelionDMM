package window_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"runtime/trace"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/model"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/app/window"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

type traceFrameApp struct{ draw func() }

func (a traceFrameApp) Process()                                            { a.draw() }
func (traceFrameApp) PostProcess()                                          {}
func (traceFrameApp) CloseCheck()                                           {}
func (traceFrameApp) IsClosed() bool                                        { return false }
func (traceFrameApp) LayoutIniPath() string                                 { return "" }
func (*mouseNetworkApp) CollaborationPresence() []collabui.ObservedPresence { return nil }

// This measures a hidden native frame and explicit GPU completion, not
// physical visibility, OS input latency or a representative mapping workload.
func TestQueuedNativeUIStageTrace(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	path := e.Dmm().Path.Absolute
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	platform.InitImGuiGLFW()
	t.Cleanup(platform.DisposeImGuiGLFW)
	platform.InitImGuiGL()
	t.Cleanup(platform.DisposeImGuiGL)
	glfw.SwapInterval(0)
	// Clear native callbacks before this fixture's ImGui context is destroyed.
	t.Cleanup(func() {
		lifecycleWindow.SetMouseButtonCallback(nil)
		lifecycleWindow.SetScrollCallback(nil)
		lifecycleWindow.SetKeyCallback(nil)
		lifecycleWindow.SetCharCallback(nil)
		lifecycleWindow.SetCursorPosCallback(nil)
	})
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	grab.Reset()
	grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
	var action func()
	frame := window.FrameRunnerForTest(lifecycleWindow, traceFrameApp{draw: func() {
		shortcut.Process()
		if action != nil {
			job := action
			action = nil
			job()
		}
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 640, Y: 480})
		imgui.SetNextWindowFocus()
		imgui.BeginV("Native frame probe", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoMove|imgui.WindowFlagsNoScrollbar)
		ws.Process()
		imgui.End()
	}})
	frame()
	frame()
	snapshot := func() model.Snapshot {
		t.Helper()
		state, err := e.SaveSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	hash := func(state model.Snapshot) string {
		t.Helper()
		value, err := state.Hash()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	initial := hash(snapshot())
	io := imgui.CurrentIO()
	// Each action is processed inside a frame after that frame has drained the
	// queue. Its deferred bucket work must run at the start of the next frame.
	step := func(name string, job func()) {
		t.Helper()
		complete := trace.StartRegion(context.Background(), "aphelion.probe."+name+"_to_gpu_complete")
		cpu := trace.StartRegion(context.Background(), "aphelion.probe."+name+"_to_following_frame")
		action = job
		if job == nil {
			io.KeyPress(int(glfw.KeyLeftAlt))
			io.KeyPress(int(glfw.KeyRight))
		}
		frame()
		if window.PendingFrameJobsForTest() == 0 {
			t.Fatal("action did not enqueue a bucket update")
		}
		io.KeyRelease(int(glfw.KeyLeftAlt))
		io.KeyRelease(int(glfw.KeyRight))
		frame()
		cpu.End()
		gl.Finish()
		complete.End()
		if window.PendingFrameJobsForTest() != 0 {
			t.Fatal("following frame left deferred work queued")
		}
		if code := gl.GetError(); code != gl.NO_ERROR {
			t.Fatalf("native frame GL error: 0x%x", code)
		}
	}
	var initialPixels [sha256.Size]byte
	checkPixels := false
	cycle := func() {
		revision := snapshot().Revision
		step("nudge", nil)
		if checkPixels && sha256.Sum256(ws.Map().Canvas().ReadPixels()) == initialPixels {
			t.Fatal("following frame did not show the changed selection/map")
		}
		step("undo", func() { app.commands.UndoV(path) })
		step("redo", func() { app.commands.RedoV(path) })
		step("undo", func() { app.commands.UndoV(path) })
		current := snapshot()
		if current.Revision != revision+4 || hash(current) != initial || grab.Bounds() != (util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}) {
			t.Fatal("frame workload did not restore exact authority and selection")
		}
		if checkPixels && sha256.Sum256(ws.Map().Canvas().ReadPixels()) != initialPixels {
			t.Fatal("following frame did not restore the initial canvas pixels")
		}
	}
	cycle()
	initialPixels = sha256.Sum256(ws.Map().Canvas().ReadPixels())
	checkPixels = true
	var data bytes.Buffer
	if err := trace.Start(&data); err != nil {
		t.Fatal(err)
	}
	defer trace.Stop()
	workload := uistage.Begin(uistage.Workload)
	for range 10 {
		cycle()
	}
	workload.End()
	trace.Stop()
	for _, stage := range []string{string(uistage.Frame), string(uistage.Present), string(uistage.CanvasDraw), "aphelion.bucket.queued", "aphelion.bucket.deferred", "aphelion.probe.nudge_to_following_frame"} {
		if !bytes.Contains(data.Bytes(), []byte(stage)) {
			t.Errorf("native frame trace lacks %q", stage)
		}
	}
	if output := os.Getenv("APHELION_UI_TRACE_OUTPUT"); output != "" {
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(data.Bytes())
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("trace output: %v %v", writeErr, closeErr)
		}
	}
	t.Logf("fixture=4x4x1 prefabs=48 measured_cycles=10 frames=80 edits=10 undo=20 redo=10 pending_jobs=%d fixture_sha256=%x initial_and_final_hash=%s trace_bytes=%d", window.PendingFrameJobsForTest(), sha256.Sum256(input), initial, data.Len())
	t.Logf("canvas=640x480 initial_and_final_pixels_sha256=%x gl_version=%s", initialPixels, gl.GoStr(gl.GetString(gl.VERSION)))
}
