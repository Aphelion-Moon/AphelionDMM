package render

import (
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

type fakeLighting struct {
	level, w, h int
	tiles       []float32
	seq         uint64
	rowsY0      int
	rowsY1      int
	full        bool
}

func (f *fakeLighting) Dimensions() (int, int, int) { return f.level, f.w, f.h }
func (f *fakeLighting) Tiles() []float32            { return f.tiles }
func (f *fakeLighting) Seq() uint64                 { return f.seq }
func (f *fakeLighting) RowsSince(seq uint64) (int, int, bool) {
	if seq == f.seq {
		return 0, 0, false
	}
	return f.rowsY0, f.rowsY1, f.full
}

func uniformTile(v float32) []float32 {
	t := make([]float32, 12)
	for i := range t {
		t[i] = v
	}
	return t
}

func TestLightingVisibleRows(t *testing.T) {
	cases := []struct {
		name         string
		view         util.Bounds
		size, h      int
		wantA, wantB int
	}{
		{"all", util.Bounds{X1: 0, Y1: 0, X2: 64, Y2: 320}, 32, 10, 0, 10},
		{"middle", util.Bounds{X1: 0, Y1: 70, X2: 64, Y2: 130}, 32, 10, 2, 5},
		{"below and above the map", util.Bounds{X1: 0, Y1: -500, X2: 64, Y2: 9000}, 32, 10, 0, 10},
		{"outside", util.Bounds{X1: 0, Y1: 400, X2: 64, Y2: 500}, 32, 10, 0, 0},
	}
	for _, c := range cases {
		a, b := lightingVisibleRows(c.view, c.size, c.h)
		if a != c.wantA || b != c.wantB {
			t.Errorf("%s: got %d..%d want %d..%d", c.name, a, b, c.wantA, c.wantB)
		}
	}
}

func TestLightingObserverReceivesEveryDisplayUpdate(t *testing.T) {
	r := &Render{Camera: newCamera()}
	var level int
	var got []util.Point
	r.SetTileObserver(func(l int, pts []util.Point) { level, got = l, append(got, pts...) })
	r.notifyTiles(2, []util.Point{{X: 3, Y: 4, Z: 2}})
	if level != 2 || len(got) != 1 || got[0].X != 3 {
		t.Fatalf("observer got level %d %v", level, got)
	}
	r.SetTileObserver(nil)
	r.notifyTiles(1, nil) // must not panic without an observer
}

func lightingFixture(t *testing.T) (*Render, func() []byte) {
	t.Helper()
	if renderTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 for the native lighting fixture")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	renderTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	previous := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previous })

	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 64, 64, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
	if gl.CheckFramebufferStatus(gl.FRAMEBUFFER) != gl.FRAMEBUFFER_COMPLETE {
		t.Fatal("framebuffer incomplete")
	}
	gl.Viewport(0, 0, 64, 64)
	gl.Enable(gl.BLEND)
	r := &Render{Camera: newCamera()}
	t.Cleanup(func() {
		r.DisposeLighting()
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		gl.DeleteFramebuffers(1, &fbo)
		gl.DeleteTextures(1, &tex)
	})
	read := func() []byte {
		pixels := make([]byte, 64*64*4)
		gl.ReadPixels(0, 0, 64, 64, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
		return pixels
	}
	return r, read
}

func pixel(p []byte, x, y int) [3]int {
	i := (y*64 + x) * 4
	return [3]int{int(p[i]), int(p[i+1]), int(p[i+2])}
}

func drawLit(r *Render, f LightingFrame, strength float32) {
	gl.ClearColor(1, 1, 1, 1) // the "map": solid white
	gl.Clear(gl.COLOR_BUFFER_BIT)
	r.SetLighting(f, strength)
	r.drawLighting(64, 64, r.viewportBounds(64, 64))
}

// A lit tile must stay brighter than an unlit one, and the strength mixes the
// multiplication: final = 1 - s*(1 - light).
func TestLightingPassMultipliesMapByTileLight(t *testing.T) {
	r, read := lightingFixture(t)
	tiles := append(uniformTile(1), uniformTile(0.1)...)
	f := &fakeLighting{level: 1, w: 2, h: 1, tiles: tiles, seq: 1, full: true}
	drawLit(r, f, 1)
	px := read()
	lit, dark := pixel(px, 16, 16), pixel(px, 48, 16)
	if lit != [3]int{255, 255, 255} {
		t.Fatalf("fully lit tile must leave the map unchanged, got %v", lit)
	}
	for _, c := range dark {
		if c < 24 || c > 28 {
			t.Fatalf("0.10 floor tile must multiply to ~26, got %v", dark)
		}
	}
	f.seq = 2
	drawLit(r, f, 0.5)
	px = read()
	for _, c := range pixel(px, 48, 16) {
		if want := int(math.Round(float64(255) * (1 - 0.5*(1-0.1)))); c < want-3 || c > want+3 {
			t.Fatalf("strength 0.5 should give ~%d, got %v", want, pixel(px, 48, 16))
		}
	}
	f.seq = 3
	drawLit(r, f, 0)
	for _, c := range pixel(read(), 48, 16) {
		if c != 255 {
			t.Fatal("strength 0 must show the map unchanged")
		}
	}
}

// Corner order is SW, SE, NW, NE with north up, interpolated across the tile.
func TestLightingPassInterpolatesCornersWithNorthUp(t *testing.T) {
	r, read := lightingFixture(t)
	tile := []float32{
		0, 0, 0, // SW dark
		1, 1, 1, // SE
		1, 1, 1, // NW
		1, 1, 1, // NE
	}
	f := &fakeLighting{level: 1, w: 1, h: 1, tiles: tile, seq: 1, full: true}
	drawLit(r, f, 1)
	px := read()
	sw, ne := pixel(px, 1, 1), pixel(px, 30, 30)
	if sw[0] >= 40 || ne[0] <= 215 {
		t.Fatalf("SW corner %v should be near black and NE %v near white (GL origin is bottom-left)", sw, ne)
	}
}

// The light must follow the camera like the map units do. The brush transform
// carries its shift in the z column (Translate2D is a Mat3), so a pass that
// emits z = 0 stays pinned to the bottom-left of the canvas.
func TestLightingPassFollowsCameraShift(t *testing.T) {
	r, read := lightingFixture(t)
	r.Camera.ShiftX, r.Camera.ShiftY = 32, 32
	f := &fakeLighting{level: 1, w: 1, h: 1, tiles: uniformTile(0), seq: 1, full: true}
	drawLit(r, f, 1)
	px := read()
	if got := pixel(px, 48, 48); got[0] != 0 {
		t.Fatalf("shifted tile at (32..64) must be dark, got %v", got)
	}
	if got := pixel(px, 16, 16); got[0] != 255 {
		t.Fatalf("origin must stay unlit once the camera moves, got %v", got)
	}
}

// A partial row update must replace only that row's colours.
func TestLightingPartialUploadUpdatesRows(t *testing.T) {
	r, read := lightingFixture(t)
	tiles := append(uniformTile(1), uniformTile(1)...) // 1 wide, 2 tall
	f := &fakeLighting{level: 1, w: 1, h: 2, tiles: tiles, seq: 1, full: true}
	drawLit(r, f, 1)
	if pixel(read(), 16, 48)[0] != 255 {
		t.Fatal("setup")
	}
	next := append([]float32(nil), tiles...)
	copy(next[12:24], uniformTile(0)) // top row dark
	f2 := &fakeLighting{level: 1, w: 1, h: 2, tiles: next, seq: 2, rowsY0: 1, rowsY1: 2}
	drawLit(r, f2, 1)
	px := read()
	// Row 0 (bottom) stays lit; row 1 (top) is black.
	if pixel(px, 16, 16)[0] != 255 || pixel(px, 16, 48)[0] != 0 {
		t.Fatalf("partial upload wrong: bottom %v top %v", pixel(px, 16, 16), pixel(px, 16, 48))
	}
}

// TestRecordLightingUploadTimings logs the GL cost of a full upload and a
// partial update at the 255x255 map limit. It is a measurement, not a gate.
func TestRecordLightingUploadTimings(t *testing.T) {
	r, _ := lightingFixture(t)
	const n = 255
	tiles := make([]float32, n*n*12)
	for i := range tiles {
		tiles[i] = float32(i%7) / 7
	}
	f := &fakeLighting{level: 1, w: n, h: n, tiles: tiles, seq: 1, full: true}
	r.SetLighting(f, 0.7)
	view := r.viewportBounds(64, 64)
	start := time.Now()
	r.drawLighting(64, 64, view)
	gl.Finish()
	full := time.Since(start)
	f2 := &fakeLighting{level: 1, w: n, h: n, tiles: tiles, seq: 2, rowsY0: 100, rowsY1: 108}
	r.SetLighting(f2, 0.7)
	start = time.Now()
	r.drawLighting(64, 64, view)
	gl.Finish()
	partial := time.Since(start)
	f3 := &fakeLighting{level: 1, w: n, h: n, tiles: tiles, seq: 2}
	r.SetLighting(f3, 0.7)
	start = time.Now()
	r.drawLighting(64, 64, view)
	gl.Finish()
	steady := time.Since(start)
	t.Logf("255x255: first upload+draw %v (%d KiB vertex data), 8-row partial upload+draw %v, steady-state draw %v", full, len(tiles)*4/1024, partial, steady)
}
