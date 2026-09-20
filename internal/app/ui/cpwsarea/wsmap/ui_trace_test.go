package wsmap

import (
	"bytes"
	"crypto/sha256"
	"os"
	"runtime/trace"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/diagnostics/uistage"
)

// This is a native CPU-stage workload, not input-to-photon or GPU timing.
func TestNativeUIStageTrace(t *testing.T) {
	ws, app := newSelectionWorkspace(t)
	activateSelectionWorkspace(t, ws)
	e := ws.Map().Editor()
	initial := resizeHash(t, resizeSnapshot(t, e))
	path := e.Dmm().Path.Absolute
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	size := imgui.Vec2{X: 320, Y: 240}
	cycle := func() {
		revision := resizeSnapshot(t, e).Revision
		pressSelectionShortcut(glfw.KeyLeftAlt, glfw.KeyRight)
		app.commands.UndoV(path)
		app.commands.RedoV(path)
		app.commands.UndoV(path)
		// Explicitly sample the renderer boundary. This does not drain the real
		// window queue or claim to measure the production next-displayed frame.
		ws.Map().Canvas().Render().UpdateBucket(e.Dmm(), 1)
		ws.Map().Canvas().Process(size)
		gl.Finish()
		current := resizeSnapshot(t, e)
		if current.Revision != revision+4 || resizeHash(t, current) != initial {
			t.Fatal("timing workload failed exact undo round trip")
		}
		if err := gl.GetError(); err != gl.NO_ERROR {
			t.Fatalf("render workload GL error: 0x%x", err)
		}
	}
	cycle() // Independent warmup, excluded from the trace.
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
	for _, name := range []uistage.Stage{uistage.CaptureTile, uistage.Commit, uistage.Dispatch, uistage.Refresh, uistage.BucketBuild, uistage.CanvasDraw} {
		if !bytes.Contains(data.Bytes(), []byte(name)) {
			t.Errorf("native workload did not emit stage %q", name)
		}
	}
	if destination := os.Getenv("APHELION_UI_TRACE_OUTPUT"); destination != "" {
		if err := os.WriteFile(destination, data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("fixture=4x4x2 prefabs=96 measured_cycles=10 edits=10 undo=20 redo=10 fixture_sha256=%x initial_and_final_hash=%s trace_bytes=%d", sha256.Sum256(input), initial, data.Len())
}
