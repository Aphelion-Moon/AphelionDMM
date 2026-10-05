package chunk

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/renderprep"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
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

func TestOccurrenceMaskFiltersBeforeUnitConstruction(t *testing.T) {
	dmm := newChunkTestMap(1, 1, 1)
	tile := dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1})
	tile.InstancesAdd(dmmprefab.New(0, "/obj/not_materialized", (&dmvars.MutableVariables{}).ToImmutable()))
	c := New(1, 1, 1, 1, 32)
	// Previously populated and empty layers must disappear when filtered out.
	c.UnitsByLayers = map[float32][]unit.Unit{1: make([]unit.Unit, 4), 2: nil}
	seen := 0
	c.Update(dmm, 1, func(*dmminstance.Instance) bool { seen++; return false })
	if seen != 1 || len(c.UnitsByLayers) != 0 {
		t.Fatal("masked instance allocated render units", seen, c.UnitsByLayers)
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

func TestUnitBatchMatchesUncachedAppearanceAndPlacement(t *testing.T) {
	if chunkTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native unit preparation")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	chunkTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)

	parent := (&dmvars.MutableVariables{}).ToImmutable()
	for name, value := range map[string]string{
		"pixel_x": "16777216", "step_x": "1", "pixel_y": "-17",
		"step_y": "3", "pixel_w": "0", "pixel_z": "-9",
		"plane": "-1", "layer": "25004", "color": "\"#804020\"", "alpha": "111",
	} {
		parent = dmvars.Set(parent, name, value)
	}
	shared := dmmprefab.New(0, "/obj/batch-shared", dmvars.FromParent(parent))
	var batch renderprep.UnitBatch
	check := func(p *dmmprefab.Prefab, x, y, iconSize int) unit.Unit {
		t.Helper()
		instance := dmminstance.New(util.Point{X: x, Y: y, Z: 1}, p)
		got := batch.Make(x, y, instance, iconSize)
		want := unit.Make(x, y, instance, iconSize)
		if got != want {
			t.Fatalf("cached unit differs at (%d,%d), scale %d: got %+v, want %+v", x, y, iconSize, got, want)
		}
		return got
	}
	first := check(shared, 1, 1, 1)
	second := check(shared, 2, 2, 1)
	// Independent expectations also guard the offset helper shared by both paths.
	if first.ViewBounds().X1 != 16777216 || first.ViewBounds().Y1 != -23 ||
		second.ViewBounds().X1 != 16777218 || second.ViewBounds().Y1 != -22 {
		t.Fatal("integer placement or inherited pixel/step offsets changed")
	}
	for n := 0; n < 140; n++ {
		p := dmmprefab.New(0, "/obj/batch-unique", dmvars.Set(parent, "pixel_x", fmt.Sprint(n)))
		check(p, n%11+1, n%7+1, 32)
		check(p, n%7+1, n%11+1, 64)
	}
	check(shared, 4, 7, 32)
	replacement := dmmprefab.New(0, shared.Path(), dmvars.Set(shared.Vars(), "alpha", "255"))
	check(replacement, 4, 7, 32)

	// Async publication updates the retained sprite handle. Both existing and
	// subsequently placed units must observe its current dimensions.
	dmi := first.Sprite().Dmi()
	width, height := dmi.IconWidth, dmi.IconHeight
	t.Cleanup(func() { dmi.IconWidth, dmi.IconHeight = width, height })
	dmi.IconWidth, dmi.IconHeight = width+64, height+16
	bounds := first.ViewBounds()
	if bounds.X2 != bounds.X1+float32(width+64) || bounds.Y2 != bounds.Y1+float32(height+16) {
		t.Fatal("prepared unit retained old sprite dimensions", bounds)
	}
	check(shared, 3, 6, 32)
}

func TestChunkBatchPreservesFiltersOrderAndReplacement(t *testing.T) {
	if chunkTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for native chunk preparation")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	chunkTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)

	dmm := newChunkTestMap(3, 2, 1)
	prefab := dmmprefab.New(0, "/obj/batch-order", dmvars.Set((&dmvars.MutableVariables{}).ToImmutable(), "pixel_x", "-13"))
	for _, tile := range dmm.Tiles {
		tile.InstancesAdd(prefab)
		tile.InstancesAdd(prefab)
	}
	blocked := dmm.Tiles[2].Instances()[0]
	c := New(1, 1, 3, 2, float32(dmmap.WorldIconSize))
	for pass := 0; pass < 2; pass++ {
		calls := 0
		c.Update(dmm, 1, func(i *dmminstance.Instance) bool {
			calls++
			return i != blocked
		})
		want := map[float32][]unit.Unit{}
		bounds := c.baseViewBounds
		for x := 1; x <= 3; x++ {
			for y := 1; y <= 2; y++ {
				for _, i := range dmm.GetTile(util.Point{X: x, Y: y, Z: 1}).Instances() {
					if i == blocked {
						continue
					}
					u := unit.Make(x, y, i, dmmap.WorldIconSize)
					want[u.Layer()] = append(want[u.Layer()], u)
					bounds = includeViewBounds(bounds, u.ViewBounds())
				}
			}
		}
		if calls != 12 || c.ViewBounds != bounds {
			t.Fatalf("pass %d: filter calls=%d, bounds=%+v, want 12, %+v", pass, calls, c.ViewBounds, bounds)
		}
		if len(c.UnitsByLayers) != len(want) {
			t.Fatalf("pass %d retained removed layers: got %d, want %d", pass, len(c.UnitsByLayers), len(want))
		}
		for layer, expected := range want {
			got := c.UnitsByLayers[layer]
			if len(got) != len(expected) {
				t.Fatalf("pass %d: layer %v has %d units, want %d", pass, layer, len(got), len(expected))
			}
			for n, u := range expected {
				if got[n] != u {
					t.Fatalf("pass %d: layer %v unit %d differs from uncached painter order", pass, layer, n)
				}
			}
		}
		for _, tile := range dmm.Tiles {
			for _, instance := range tile.Instances() {
				instance.SetPrefab(dmmprefab.New(0, prefab.Path(), dmvars.Set(prefab.Vars(), "layer", "5")))
			}
		}
		dmm.Tiles[0].Instances()[0].SetPrefab(dmmprefab.New(0, prefab.Path(), dmvars.Set(dmm.Tiles[0].Instances()[0].Prefab().Vars(), "pixel_x", "53")))
		blocked = dmm.Tiles[3].Instances()[1]
	}
}
