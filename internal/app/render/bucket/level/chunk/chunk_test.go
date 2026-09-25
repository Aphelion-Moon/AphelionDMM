package chunk

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

var chunkTestWindow *glfw.Window

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
	win, err := glfw.CreateWindow(64, 64, "Chunk culling verification", nil, nil)
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
	chunkTestWindow = win
	glfw.DetachCurrentContext()
	code := m.Run()
	win.MakeContextCurrent()
	win.Destroy()
	glfw.Terminate()
	os.Exit(code)
}

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
func TestChunkRevisionAdvancesAfterGeometryRebuild(t *testing.T) {
	dmm := newChunkTestMap(1, 1, 1)
	c := New(1, 1, 1, 1, 32)
	if c.Revision() != 0 {
		t.Fatalf("initial revision=%d, want 0", c.Revision())
	}
	c.Update(dmm, 1)
	if c.Revision() != 1 {
		t.Fatalf("revision after first rebuild=%d, want 1", c.Revision())
	}
	c.Update(dmm, 1)
	if c.Revision() != 2 {
		t.Fatalf("revision after second rebuild=%d, want 2", c.Revision())
	}
}

// APHELION EDIT ADDITION END

func TestUpdateIncludesSpriteOverhangAndRebuildsBoundsPerLevel(t *testing.T) {
	if chunkTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the native sprite-bounds fixture")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	chunkTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })

	dmm := newChunkTestMap(24, 24, 2)
	setChunkTestInstance(dmm, util.Point{X: 1, Y: 1, Z: 1}, map[string]string{
		"pixel_x": "-64",
		"pixel_y": "-16",
	})
	setChunkTestInstance(dmm, util.Point{X: 24, Y: 24, Z: 1}, map[string]string{
		"pixel_x": "48",
		"pixel_y": "48",
	})
	setChunkTestInstance(dmm, util.Point{X: 1, Y: 24, Z: 2}, map[string]string{
		"pixel_x": "-96",
		"pixel_y": "64",
	})

	levelOne := New(1, 1, 24, 24, 32)
	levelOne.Update(dmm, 1)
	wantLevelOne := util.Bounds{X1: -64, Y1: -16, X2: 816, Y2: 816}
	if levelOne.ViewBounds != wantLevelOne {
		t.Fatalf("level 1 view bounds = %+v, want %+v", levelOne.ViewBounds, wantLevelOne)
	}
	if levelOne.MapBounds != (util.Bounds{X1: 1, Y1: 1, X2: 24, Y2: 24}) {
		t.Fatalf("sprite extents changed map bounds: %+v", levelOne.MapBounds)
	}

	levelTwo := New(1, 1, 24, 24, 32)
	levelTwo.Update(dmm, 2)
	wantLevelTwo := util.Bounds{X1: -96, Y1: 0, X2: 768, Y2: 832}
	if levelTwo.ViewBounds != wantLevelTwo {
		t.Fatalf("level 2 view bounds = %+v, want %+v", levelTwo.ViewBounds, wantLevelTwo)
	}

	dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Set(nil)
	dmm.GetTile(util.Point{X: 24, Y: 24, Z: 1}).Set(nil)
	levelOne.Update(dmm, 1)
	wantBase := util.Bounds{X1: 0, Y1: 0, X2: 768, Y2: 768}
	if levelOne.ViewBounds != wantBase {
		t.Fatalf("rebuilt bounds retained removed sprite overhang: got %+v, want %+v", levelOne.ViewBounds, wantBase)
	}
}

func newChunkTestMap(maxX, maxY, maxZ int) *dmmap.Dmm {
	dmm := &dmmap.Dmm{
		MaxX:  maxX,
		MaxY:  maxY,
		MaxZ:  maxZ,
		Tiles: make([]*dmmap.Tile, maxX*maxY*maxZ),
	}
	for z := 1; z <= maxZ; z++ {
		for y := 1; y <= maxY; y++ {
			for x := 1; x <= maxX; x++ {
				coord := util.Point{X: x, Y: y, Z: z}
				index := maxX*maxY*(z-1) + maxX*(y-1) + (x - 1)
				dmm.Tiles[index] = &dmmap.Tile{Coord: coord}
			}
		}
	}
	return dmm
}

func setChunkTestInstance(dmm *dmmap.Dmm, coord util.Point, values map[string]string) {
	vars := &dmvars.MutableVariables{}
	for name, value := range values {
		vars.Put(name, value)
	}
	prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/chunk-test", vars.ToImmutable())
	tile := dmm.GetTile(coord)
	tile.Set(dmmap.Instances{dmminstance.New(coord, prefab)})
}
