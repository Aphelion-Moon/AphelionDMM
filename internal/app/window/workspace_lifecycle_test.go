package window_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
	"weak"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/rs/zerolog"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

// One application, environment and GL context outlive all opened workspaces.
// Do not register per-cycle testing cleanups: those would retain closed maps
// until the test exits and invalidate the resource observations.
func TestNativeWorkspaceLifecycle(t *testing.T) {
	primary, app := newMouseNetworkWorkspace(t)
	primary.OnFocusChange(false)
	platform.InitImGuiGLFW()
	t.Cleanup(platform.DisposeImGuiGLFW)
	platform.InitImGuiGL()
	t.Cleanup(platform.DisposeImGuiGL)
	glfw.SwapInterval(0)
	t.Cleanup(func() {
		lifecycleWindow.SetMouseButtonCallback(nil)
		lifecycleWindow.SetScrollCallback(nil)
		lifecycleWindow.SetKeyCallback(nil)
		lifecycleWindow.SetCharCallback(nil)
		lifecycleWindow.SetCursorPosCallback(nil)
	})
	input, err := os.ReadFile(primary.Map().Dmm().Path.Absolute)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reopened.dmm")
	if err := os.WriteFile(path, input, 0600); err != nil {
		t.Fatal(err)
	}
	duration := time.Duration(0)
	if value := os.Getenv("APHELION_LIFECYCLE_DURATION"); value != "" {
		duration, err = time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > time.Hour {
			t.Fatal("APHELION_LIFECYCLE_DURATION must be greater than zero and at most 1h")
		}
		// Endurance observes retained resources, not logging throughput. Keep
		// warnings/errors while avoiding per-shortcut logs for every reopen.
		level := zerolog.GlobalLevel()
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		t.Cleanup(func() { zerolog.SetGlobalLevel(level) })
	}
	var output *os.File
	if name := os.Getenv("APHELION_LIFECYCLE_OUTPUT"); name != "" {
		output, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := output.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	var current *wsmap.WsMap
	t.Cleanup(func() {
		if current != nil {
			current.OnFocusChange(false)
			current.Dispose()
			window.DrainFrameJobsForTest()
		}
	})
	var action func()
	frame := window.FrameRunnerForTest(lifecycleWindow, traceFrameApp{draw: func() {
		shortcut.Process()
		if action != nil {
			job := action
			action = nil
			job()
		}
		if current != nil {
			imgui.SetNextWindowPos(imgui.Vec2{})
			imgui.SetNextWindowSize(imgui.Vec2{X: 640, Y: 480})
			imgui.SetNextWindowFocus()
			imgui.BeginV("Workspace lifetime probe", nil, imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoMove|imgui.WindowFlagsNoScrollbar)
			current.Process()
			imgui.End()
		}
	}})
	hash := func(state model.Snapshot) string {
		t.Helper()
		value, err := state.Hash()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	cycle := func() weak.Pointer[editor.Editor] {
		data, err := dmmdata.New(path)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := dmmap.New(app.environment, data, path)
		app.commands.SetStack(path)
		current = wsmap.New(app, m)
		current.OnFocusChange(true)
		e := current.Map().Editor()
		pointer := weak.Make(e)
		snapshot := func() model.Snapshot {
			state, err := e.SaveSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return state
		}
		initial := snapshot()
		grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
		grab.Reset()
		grab.SelectArea([]util.Point{{X: 1, Y: 1, Z: 1}})
		frame()
		frame()
		step := func(job func()) {
			action = job
			frame()
			frame()
			if window.PendingFrameJobsForTest() != 0 {
				t.Fatal("following frame left deferred work")
			}
		}
		io := imgui.CurrentIO()
		io.KeyPress(int(glfw.KeyLeftAlt))
		io.KeyPress(int(glfw.KeyRight))
		frame()
		io.KeyRelease(int(glfw.KeyLeftAlt))
		io.KeyRelease(int(glfw.KeyRight))
		frame()
		if hash(snapshot()) == hash(initial) {
			t.Fatal("native nudge did not change authority")
		}
		step(func() { app.commands.UndoV(path) })
		step(func() { app.commands.RedoV(path) })
		step(func() { app.commands.UndoV(path) })
		final := snapshot()
		if final.Revision != initial.Revision+4 || hash(final) != hash(initial) {
			t.Fatal("edit/undo/redo did not restore exact authority")
		}
		if !current.Save() || current.HasUnsavedChanges() {
			t.Fatal("workspace save failed or remained dirty")
		}
		saved, err := dmmdata.New(path)
		if err != nil {
			t.Fatal(err)
		}
		reopened, _ := dmmap.New(app.environment, saved, path)
		// Inherited Save deliberately groups instances by DM path weight. Verify
		// that established file ordering, while retaining exact values and IDs
		// through the metadata comparison. The live authority stays unchanged.
		expectedSave := model.CloneSnapshot(final)
		for i := range expectedSave.Tiles {
			prefabs := expectedSave.Tiles[i].State.Prefabs
			sort.SliceStable(prefabs, func(i, j int) bool {
				return dm.PathWeight(prefabs[i].Path) < dm.PathWeight(prefabs[j].Path)
			})
		}
		roundtrip, err := mapadapter.Reimport(reopened, expectedSave)
		if err != nil || hash(roundtrip) != hash(expectedSave) {
			t.Fatal("saved file did not preserve authority in Save order", err)
		}
		texture := current.Map().Canvas().Texture()
		if texture == 0 || !gl.IsTexture(texture) {
			t.Fatal("workspace did not own a native texture")
		}
		current.OnFocusChange(false)
		current.Dispose()
		// WsArea.closeWorkspaceByIdx releases the command stack after content
		// disposal; preserve that ordering in this native content fixture.
		app.commands.DisposeStack(path)
		current = nil
		frame() // The production frame drains the queued texture deletion.
		gl.Finish()
		if gl.IsTexture(texture) || window.PendingFrameJobsForTest() != 0 || app.mouse != nil || app.commands.HasUndoV(path) || app.commands.HasRedoV(path) {
			t.Fatal("closed workspace retained native resources, jobs, callback or history")
		}
		if code := gl.GetError(); code != gl.NO_ERROR {
			t.Fatalf("workspace cycle GL error: 0x%x", code)
		}
		return pointer
	}
	// Warm up parser, renderer and command paths before the first checkpoint.
	for range 4 {
		pointer := cycle()
		runtime.GC()
		if pointer.Value() != nil {
			t.Fatal("closed editor remains reachable after warmup")
		}
	}
	start := time.Now()
	checkpoint := func(cycles int) {
		runtime.GC()
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		record := struct {
			Cycles      int     `json:"cycles"`
			Elapsed     float64 `json:"elapsed_seconds"`
			HeapAlloc   uint64  `json:"heap_alloc"`
			HeapObjects uint64  `json:"heap_objects"`
			HeapSys     uint64  `json:"heap_sys"`
			Goroutines  int     `json:"goroutines"`
		}{cycles, time.Since(start).Seconds(), memory.HeapAlloc, memory.HeapObjects, memory.HeapSys, runtime.NumGoroutine()}
		if output != nil {
			if err := json.NewEncoder(output).Encode(record); err != nil {
				t.Fatal(err)
			}
			if err := output.Sync(); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("checkpoint: %+v", record)
	}
	t.Logf("pid=%d fixture_sha256=%x gl_version=%s requested_duration=%s", os.Getpid(), sha256.Sum256(input), gl.GoStr(gl.GetString(gl.VERSION)), duration)
	checkpoint(0)
	cycles, nextCheckpoint := 0, start.Add(time.Minute)
	for cycles < 16 || time.Since(start) < duration {
		cycleStart := time.Now()
		pointer := cycle()
		cycles++
		runtime.GC()
		if pointer.Value() != nil {
			t.Fatalf("closed editor remains reachable after cycle %d", cycles)
		}
		if time.Now().After(nextCheckpoint) {
			checkpoint(cycles)
			nextCheckpoint = time.Now().Add(time.Minute)
		}
		if duration > 0 {
			// At most ten complete lifetimes per second; slow cycles run to
			// completion. No catch-up burst or throughput claim is made.
			time.Sleep(max(0, 100*time.Millisecond-time.Since(cycleStart)))
		}
	}
	checkpoint(cycles)
}
