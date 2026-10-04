package window_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
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
	// Set the frame-probe size on the creating thread; resizing this shared
	// native window from a test goroutine can block Windows message delivery.
	win, err := glfw.CreateWindow(640, 480, "Canvas lifecycle verification", nil, nil)
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

// The baseline filters after full geometry construction, as screenshots did.
type screenshotMaskFixture struct{ points map[util.Point]bool }

func (p screenshotMaskFixture) ProcessUnit(u unit.Unit) bool {
	return p.points[u.Instance().Coord()] && u.Instance().Prefab().Path() != "/obj/hidden"
}

type screenshotGeometryFixture struct{ screenshotMaskFixture }

func (p screenshotGeometryFixture) GeometryInstanceVisible(i *dmminstance.Instance) bool {
	return p.points[i.Coord()] && i.Prefab().Path() != "/obj/hidden"
}

func TestNativeSparseScreenshotMatchesFullPreparation(t *testing.T) {
	newMouseNetworkWorkspace(t)
	selected := []util.Point{{X: 25, Y: 2, Z: 2}, {X: 27, Y: 2, Z: 2}}
	mask := screenshotMaskFixture{points: map[util.Point]bool{selected[0]: true, selected[1]: true}}
	dmm := &dmmap.Dmm{MaxX: 75, MaxY: 2, MaxZ: 2}
	for z := 1; z <= dmm.MaxZ; z++ {
		for y := 1; y <= dmm.MaxY; y++ {
			for x := 1; x <= dmm.MaxX; x++ {
				point := util.Point{X: x, Y: y, Z: z}
				tile := &dmmap.Tile{Coord: point}
				for layer, path := range []string{"/obj/base", "/obj/overhang", "/obj/hidden"} {
					vars := &dmvars.MutableVariables{}
					vars.Put("layer", fmt.Sprint(layer+1))
					vars.Put("alpha", "255")
					vars.Put("color", "\"#ff0000\"")
					if layer == 1 {
						vars.Put("color", "\"#00ff00\"")
						vars.Put("pixel_x", "16")
						vars.Put("alpha", "128")
					}
					tile.Set(append(tile.Instances(), dmminstance.New(point, dmmprefab.New(0, path, vars.ToImmutable()))))
				}
				dmm.Tiles = append(dmm.Tiles, tile)
			}
		}
	}
	renderPixels := func(partial bool) []byte {
		c := canvas.New()
		defer func() { c.Dispose(); window.DrainFrameJobsForTest() }()
		c.ClearColor = canvas.Color{}
		r := c.Render()
		r.Camera.Level = 2
		r.Camera.Translate(-24*32, -32)
		if partial {
			r.SetUnitProcessor(screenshotGeometryFixture{mask})
			r.UpdateBucketV(dmm, 2, selected)
		} else {
			r.SetUnitProcessor(mask)
			r.UpdateBucket(dmm, 1)
			r.UpdateBucket(dmm, 2)
		}
		c.Process(imgui.Vec2{X: 96, Y: 32})
		c.Dispose()
		return c.ReadPixels()
	}
	baseline, partial := renderPixels(false), renderPixels(true)
	if !bytes.Equal(baseline, partial) {
		t.Fatal("partial screenshot changed sparse-mask, layer, or overhang pixels")
	}
	visible := false
	for i := 3; i < len(partial); i += 4 {
		visible = visible || partial[i] != 0
	}
	if !visible {
		t.Fatal("screenshot comparison rendered no selected geometry")
	}
}
