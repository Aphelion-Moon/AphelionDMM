package window_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/app/window"
)

func TestResizeHistoryReleasesReplacedCanvas(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	pane, e := ws.Map(), ws.Map().Editor()
	path := e.Dmm().Path.Absolute
	window.DrainFrameJobsForTest()
	camera := pane.Canvas().Render().Camera
	camera.ShiftX, camera.ShiftY, camera.Scale = 17, 23, 2
	initialCamera := *camera
	snapshotHash := func() string {
		t.Helper()
		state, err := e.SaveSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		hash, err := state.Hash()
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	initialHash := snapshotHash()
	initialCanvas := pane.Canvas()
	if err := e.ResizeMap(4, 4, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.ResizeMap(0, 4, 1); err == nil {
		t.Fatal("invalid resize was accepted")
	}
	if pane.Canvas() != initialCanvas || window.PendingFrameJobsForTest() != 0 {
		t.Fatal("no-op or rejected resize replaced or disposed the canvas")
	}
	step := func(name string, action func()) {
		t.Helper()
		previous := pane.Canvas()
		previous.Process(imgui.Vec2{X: 64, Y: 64})
		texture, pixels := previous.Texture(), previous.ReadPixels()
		if texture == 0 || !gl.IsTexture(texture) || len(pixels) != 64*64*4 {
			t.Fatal("previous canvas did not allocate a readable texture")
		}
		action()
		next := pane.Canvas()
		if next == previous || next.Render().Camera != camera || *camera != initialCamera {
			t.Fatalf("%s did not replace the canvas while preserving the camera", name)
		}
		// Old draw commands and screenshot readback remain valid until the
		// production deferred queue drains, even if the replacement is drawn.
		next.Process(imgui.Vec2{X: 64, Y: 64})
		nextTexture := next.Texture()
		if !gl.IsTexture(texture) || !bytes.Equal(previous.ReadPixels(), pixels) || !gl.IsTexture(nextTexture) {
			t.Fatalf("%s destroyed same-frame canvas resources", name)
		}
		window.DrainFrameJobsForTest()
		if gl.IsTexture(texture) || previous.Texture() != 0 || !gl.IsTexture(nextTexture) || window.PendingFrameJobsForTest() != 0 {
			t.Fatalf("%s retained replaced canvas resources or deleted the current canvas", name)
		}
		previous.Process(imgui.Vec2{X: 128, Y: 128})
		if previous.Texture() != 0 {
			t.Fatalf("%s allowed a stale canvas to recreate its texture", name)
		}
		if code := gl.GetError(); code != gl.NO_ERROR {
			t.Fatalf("%s GL error: 0x%x", name, code)
		}
	}
	for range 8 {
		step("resize", func() {
			if err := e.ResizeMap(5, 4, 1); err != nil {
				t.Fatal(err)
			}
		})
		expandedHash := snapshotHash()
		step("undo", func() { app.commands.UndoV(path) })
		if snapshotHash() != initialHash {
			t.Fatal("resize undo changed original contents or identities")
		}
		step("redo", func() { app.commands.RedoV(path) })
		if snapshotHash() != expandedHash {
			t.Fatal("resize redo changed expanded contents or identities")
		}
		step("undo", func() { app.commands.UndoV(path) })
		if snapshotHash() != initialHash {
			t.Fatal("resize cycle did not restore initial authority")
		}
	}
}
