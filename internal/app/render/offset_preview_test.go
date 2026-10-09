package render

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/render/bucket"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

var renderTestWindow *glfw.Window

// TestMain provides a hidden GL context only when APHELIONDMM_GL_TEST=1.
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
	win, err := glfw.CreateWindow(64, 64, "Render offset verification", nil, nil)
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
	renderTestWindow = win
	glfw.DetachCurrentContext()
	code := m.Run()
	win.MakeContextCurrent()
	win.Destroy()
	glfw.Terminate()
	os.Exit(code)
}

func offsetPrefab(path string, pixelX int) *dmmprefab.Prefab {
	vars := dmvars.Set((&dmvars.MutableVariables{}).ToImmutable(), "pixel_x", fmt.Sprint(pixelX))
	return dmmprefab.New(dmmprefab.IdNone, path, vars)
}

// unitX1 returns the rendered X1 of the instance in the level, or ok=false.
func unitX1(r *Render, z int, id uint64) (float32, bool) {
	level := r.bucket.Level(z)
	if level == nil {
		return 0, false
	}
	var found bool
	var x1 float32
	for _, c := range level.Chunks {
		for _, units := range c.UnitsByLayers {
			for _, u := range units {
				if u.Instance().Id() == id {
					if found {
						panic("duplicate unit for instance")
					}
					found, x1 = true, u.ViewBounds().X1
				}
			}
		}
	}
	return x1, found
}

func readyOffsetRender(t *testing.T, dmm *dmmap.Dmm) *Render {
	t.Helper()
	if renderTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the native offset fixture")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	renderTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	r := &Render{Camera: newCamera(), bucket: bucket.New()}
	t.Cleanup(func() { r.InvalidateLevelBuilds(nil) })
	r.SetActiveLevel(dmm, 1)
	for steps := 0; !r.LevelReady(1) && steps < 200; steps++ {
		r.ProcessLevelBuild()
	}
	if !r.LevelReady(1) {
		t.Fatal("level never became ready")
	}
	return r
}

// A pixel_x edit applied inside the frame batch must be visible after the same
// frame's EndUpdateBatch, including when the sprite now overhangs the next chunk.
func TestOffsetEditAcrossChunkBoundaryIsVisibleWhenFrameBatchEnds(t *testing.T) {
	previous := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previous })
	dmm := levelBuildTestMap(49, 25, 1)
	edge := util.Point{X: chunk.Size, Y: 5, Z: 1}
	instance := dmminstance.New(edge, offsetPrefab("/obj/offset-test", 0))
	dmm.GetTile(edge).Set(dmmap.Instances{instance})
	r := readyOffsetRender(t, dmm)

	baseX := float32((edge.X - 1) * dmmap.WorldIconSize)
	if x1, ok := unitX1(r, 1, instance.Id()); !ok || x1 != baseX {
		t.Fatalf("initial unit x1=%v ok=%v, want %v", x1, ok, baseX)
	}
	for _, pixel := range []int{10, 40, 41, 0, -40} {
		r.BeginUpdateBatch()
		instance.SetPrefab(offsetPrefab("/obj/offset-test", pixel))
		r.UpdateBucketV(dmm, 1, []util.Point{edge})
		r.EndUpdateBatch(dmm)
		x1, ok := unitX1(r, 1, instance.Id())
		if want := baseX + float32(pixel); !ok || x1 != want {
			t.Fatalf("pixel_x=%d: rendered x1=%v ok=%v, want %v on the same frame", pixel, x1, ok, want)
		}
	}
}

// Moving an instance to a tile in another chunk must leave no stale unit in the
// old chunk when both footprints are invalidated.
func TestOffsetPreviewLeavesNoStaleUnitAtOldFootprint(t *testing.T) {
	previous := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previous })
	dmm := levelBuildTestMap(49, 25, 1)
	edge := util.Point{X: chunk.Size, Y: 5, Z: 1}
	instance := dmminstance.New(edge, offsetPrefab("/obj/offset-test", 0))
	dmm.GetTile(edge).Set(dmmap.Instances{instance})
	r := readyOffsetRender(t, dmm)

	next := util.Point{X: chunk.Size + 1, Y: 5, Z: 1}
	dmm.GetTile(edge).Set(nil)
	instance.SetCoord(next)
	instance.SetPrefab(offsetPrefab("/obj/offset-test", -40))
	dmm.GetTile(next).Set(dmmap.Instances{instance})
	r.BeginUpdateBatch()
	r.UpdateBucketV(dmm, 1, []util.Point{edge, next})
	r.EndUpdateBatch(dmm)
	want := float32((next.X-1)*dmmap.WorldIconSize - 40)
	if x1, ok := unitX1(r, 1, instance.Id()); !ok || x1 != want {
		t.Fatalf("rendered x1=%v ok=%v, want %v", x1, ok, want)
	}
}
