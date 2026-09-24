package window_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"runtime"
	"runtime/trace"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/model"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

type traceFrameApp struct{ draw func() }

func (a traceFrameApp) Process()                                            { dmicon.Cache.ProcessUploads(); a.draw() }
func (traceFrameApp) PostProcess()                                          {}
func (traceFrameApp) CloseCheck()                                           {}
func (traceFrameApp) IsClosed() bool                                        { return false }
func (traceFrameApp) LayoutIniPath() string                                 { return "" }
func (*mouseNetworkApp) CollaborationPresence() []collabui.ObservedPresence { return nil }

// This measures a hidden native frame and explicit GPU completion, not
// physical visibility, OS input latency or a representative mapping workload.
func TestQueuedNativeUIStageTrace(t *testing.T) {
	runQueuedNativeUIStageTrace(t, "", "")
}

func TestRepresentativeNativeUIStageTrace(t *testing.T) {
	mapPath, dmePath := os.Getenv("APHELION_AUDIT_MAP"), os.Getenv("APHELION_AUDIT_DME")
	if mapPath == "" && dmePath == "" {
		t.Skip("set APHELION_AUDIT_MAP and APHELION_AUDIT_DME for representative native frames")
	}
	if mapPath == "" || dmePath == "" {
		t.Fatal("both representative fixture paths are required")
	}
	for _, path := range []string{mapPath, dmePath} {
		input, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		initial := sha256.Sum256(input)
		t.Logf("source=%s sha256=%x", path, initial)
		t.Cleanup(func() {
			current, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(current) != initial {
				t.Error("representative source changed", path, err)
			}
		})
	}
	runQueuedNativeUIStageTrace(t, mapPath, dmePath)
}

func runQueuedNativeUIStageTrace(t *testing.T, mapPath, dmePath string) {
	t.Helper()
	started := time.Now()
	ws, app := newNativeMapWorkspace(t, mapPath, dmePath)
	t.Logf("workspace_setup_ms=%.3f", float64(time.Since(started).Microseconds())/1000)
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
	selected := util.Point{X: 1, Y: 1, Z: 1}
	cycles := 10
	if mapPath != "" {
		cycles = 5
		// Choose a real object tile with room to nudge right. No map mutation
		// or synthetic icon substitutes are needed to make the fixture visible.
		found := false
		for _, tile := range e.Dmm().Tiles {
			coord := tile.Coord
			if coord.X < 2 || coord.X >= e.Dmm().MaxX || coord.Y < 2 {
				continue
			}
			for _, instance := range tile.Instances() {
				if strings.HasPrefix(instance.Prefab().Path(), "/obj/") {
					selected, found = coord, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatal("representative fixture has no interior object tile")
		}
		ws.Map().SetActiveLevel(selected.Z)
	}
	grab.SelectArea([]util.Point{selected})
	var action func()
	runFrame := window.FrameRunnerForTest(lifecycleWindow, traceFrameApp{draw: func() {
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
	var frameTimes []time.Duration
	frame := func() { start := time.Now(); runFrame(); frameTimes = append(frameTimes, time.Since(start)) }
	frame()
	frame()
	if mapPath != "" {
		cold := time.Now()
		for dmicon.Cache.Loading() && time.Since(cold) < 90*time.Second {
			dmicon.Cache.ProcessUploads()
			time.Sleep(time.Millisecond)
		}
		if dmicon.Cache.Loading() {
			t.Fatal("cold icon queue did not settle")
		}
		t.Logf("cold_icon_settle_ms=%.3f", float64(time.Since(cold).Microseconds())/1000)
		frame()
	}
	if mapPath != "" {
		camera := ws.Map().Canvas().Render().Camera
		camera.ShiftX = ws.Map().Size().X/2/camera.Scale - float32((selected.X-1)*dmmap.WorldIconSize)
		camera.ShiftY = ws.Map().Size().Y/2/camera.Scale - float32((selected.Y-1)*dmmap.WorldIconSize)
		frame()
	}
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
	// Pump the real frame owner until the accepted revision is presented. A
	// worker may need more than the old synchronous path's fixed two frames.
	var operationTimes []time.Duration
	step := func(name string, job func()) {
		t.Helper()
		_, revision := e.SaveVersion()
		start := time.Now()
		complete := trace.StartRegion(context.Background(), "aphelion.probe."+name+"_to_gpu_complete")
		cpu := trace.StartRegion(context.Background(), "aphelion.probe."+name+"_to_committed_frame")
		action = job
		if job == nil {
			io.KeyPress(int(glfw.KeyLeftAlt))
			io.KeyPress(int(glfw.KeyRight))
		}
		frame()
		io.KeyRelease(int(glfw.KeyLeftAlt))
		io.KeyRelease(int(glfw.KeyRight))
		frame()
		for {
			_, current := e.SaveVersion()
			if current == revision+1 && e.CanStartMapEdit() && window.PendingFrameJobsForTest() == 0 {
				break
			}
			if time.Since(start) > 10*time.Second {
				t.Fatalf("%s did not settle: revision=%d want=%d jobs=%d", name, current, revision+1, window.PendingFrameJobsForTest())
			}
			runtime.Gosched()
			frame()
		}
		cpu.End()
		gl.Finish()
		complete.End()
		operationTimes = append(operationTimes, time.Since(start))
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
		if current.Revision != revision+4 || hash(current) != initial || grab.Bounds() != (util.Bounds{X1: float32(selected.X), Y1: float32(selected.Y), X2: float32(selected.X), Y2: float32(selected.Y)}) {
			t.Fatal("frame workload did not restore exact authority and selection")
		}
		if checkPixels && sha256.Sum256(ws.Map().Canvas().ReadPixels()) != initialPixels {
			t.Fatal("following frame did not restore the initial canvas pixels")
		}
	}
	cycle()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	initialPixels = sha256.Sum256(ws.Map().Canvas().ReadPixels())
	checkPixels = true
	frameTimes = nil
	operationTimes = nil
	var data bytes.Buffer
	traced := os.Getenv("APHELION_UI_UNPROFILED") != "1"
	if traced {
		if err := trace.Start(&data); err != nil {
			t.Fatal(err)
		}
		defer trace.Stop()
	}
	workload := uistage.Begin(uistage.Workload)
	for range cycles {
		cycle()
	}
	workload.End()
	if traced {
		trace.Stop()
	}
	for _, stage := range []string{string(uistage.Frame), string(uistage.Present), string(uistage.CanvasDraw), "aphelion.probe.nudge_to_committed_frame"} {
		if traced && !bytes.Contains(data.Bytes(), []byte(stage)) {
			t.Errorf("native frame trace lacks %q", stage)
		}
	}
	metrics := func(label string, values []time.Duration) {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		if len(values) > 0 {
			t.Logf("%s count=%d p50_ms=%.3f p95_ms=%.3f worst_ms=%.3f", label, len(values), float64(values[len(values)/2].Microseconds())/1000, float64(values[(len(values)*95-1)/100].Microseconds())/1000, float64(values[len(values)-1].Microseconds())/1000)
		}
	}
	metrics("frame_cpu_and_present", frameTimes)
	metrics("action_to_gpu_complete", operationTimes)
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
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	t.Logf("dimensions=%dx%dx%d cells=%d selected=%v measured_cycles=%d frames=%d pending_jobs=%d fixture_sha256=%x initial_and_final_hash=%s trace_bytes=%d", e.Dmm().MaxX, e.Dmm().MaxY, e.Dmm().MaxZ, len(e.Dmm().Tiles), selected, cycles, len(frameTimes), window.PendingFrameJobsForTest(), sha256.Sum256(input), initial, data.Len())
	t.Logf("post_gc_heap_before=%d post_gc_heap_after=%d total_alloc_delta=%d", before.HeapAlloc, after.HeapAlloc, after.TotalAlloc-before.TotalAlloc)
	t.Logf("canvas=640x480 initial_and_final_pixels_sha256=%x gl_version=%s", initialPixels, gl.GoStr(gl.GetString(gl.VERSION)))
	if mapPath != "" {
		// The fixture owns a temporary map and backup; exercise the workspace Save
		// entry while continuing native presentation. Keep this outside the matched
		// edit/history allocation sample above.
		frameTimes = nil
		var saveBefore, saveAfter runtime.MemStats
		runtime.ReadMemStats(&saveBefore)
		done, saved := false, false
		start := time.Now()
		ws.SaveAsync(func(ok bool) { done, saved = true, ok })
		dispatch := time.Since(start)
		for !done && time.Since(start) < 60*time.Second {
			frame()
			runtime.Gosched()
		}
		elapsed := time.Since(start)
		if !done || !saved {
			t.Fatal("representative workspace save did not complete")
		}
		runtime.ReadMemStats(&saveAfter)
		t.Logf("save_dispatch_ms=%.3f save_total_ms=%.3f save_alloc_bytes=%d", float64(dispatch.Microseconds())/1000, float64(elapsed.Microseconds())/1000, saveAfter.TotalAlloc-saveBefore.TotalAlloc)
		metrics("save_frame_cpu_and_present", frameTimes)
	}
}
