package canvas

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/rendercache"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

var resizeWindow *glfw.Window

// Keep one native context for serial tests and benchmark calibration passes.
// Brush caches are process scoped, so recreating the context between benchmark
// invocations would give them resource names from a destroyed context.
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
	win, err := glfw.CreateWindow(64, 64, "Canvas resize verification", nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		glfw.Terminate()
		os.Exit(1)
	}
	win.MakeContextCurrent()
	if err = gl.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		win.Destroy()
		glfw.Terminate()
		os.Exit(1)
	}
	resizeWindow = win
	glfw.DetachCurrentContext()
	code := m.Run()
	win.MakeContextCurrent()
	brush.Dispose()
	win.Destroy()
	glfw.Terminate()
	os.Exit(code)
}

func resizeContext(tb testing.TB) {
	tb.Helper()
	if resizeWindow == nil {
		tb.Skip("set APHELIONDMM_GL_TEST=1 for real framebuffer resize verification")
	}
	runtime.LockOSThread()
	tb.Cleanup(runtime.UnlockOSThread)
	resizeWindow.MakeContextCurrent()
	tb.Cleanup(glfw.DetachCurrentContext)
	tb.Logf("GL renderer=%s version=%s", gl.GoStr(gl.GetString(gl.RENDERER)), gl.GoStr(gl.GetString(gl.VERSION)))
}

func resizeCanvas(tb testing.TB) *Canvas {
	tb.Helper()
	c := New()
	c.ClearColor = Color{R: 1, B: 1, A: 1}
	tb.Cleanup(func() { gl.DeleteTextures(1, &c.texture); gl.DeleteFramebuffers(1, &c.frameBuffer) })
	return c
}

func checkResizePixels(t *testing.T, c *Canvas, size imgui.Vec2, marker bool) {
	t.Helper()
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error after resize: 0x%x", code)
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
	if status := gl.CheckFramebufferStatus(gl.FRAMEBUFFER); status != gl.FRAMEBUFFER_COMPLETE {
		t.Fatalf("incomplete framebuffer: 0x%x", status)
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	pixels := c.ReadPixels()
	for y := 0; y < int(size.Y); y++ {
		for x := 0; x < int(size.X); x++ {
			want := [4]byte{255, 0, 255, 255}
			if marker && x >= 1 && x < 4 && y >= 1 && y < 4 {
				want = [4]byte{0, 255, 0, 255}
			}
			i := 4 * (y*int(size.X) + x)
			if got := [4]byte{pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]}; got != want {
				t.Fatalf("pixel (%d,%d) size=%v got=%v want=%v", x, y, size, got, want)
			}
		}
	}
	t.Logf("pixels size=%dx%d marker=%v sha256=%x", int(size.X), int(size.Y), marker, sha256.Sum256(pixels))
}

func TestCanvasResizePixelsAndAllocation(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	for _, size := range []imgui.Vec2{{X: 1, Y: 1}, {X: 7, Y: 5}, {X: 640, Y: 480}, {X: 641, Y: 481}, {X: 320, Y: 240}, {X: 640, Y: 480}} {
		c.Process(size)
		checkResizePixels(t, c, size, false)
		if size.X >= 4 && size.Y >= 4 {
			brush.RectFilled(1, 1, 4, 4, util.MakeColor(0, 1, 0, 1))
			c.Process(size)
			checkResizePixels(t, c, size, true)
			c.Process(size)
			checkResizePixels(t, c, size, false)
		}
	}
	const iterations = 10
	c.Process(imgui.Vec2{X: 641, Y: 481})
	gl.Finish()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < iterations; i++ {
		c.Process(imgui.Vec2{X: float32(640 + i%2), Y: float32(480 + i%2)})
		gl.Finish()
	}
	runtime.ReadMemStats(&after)
	bytes := (after.TotalAlloc - before.TotalAlloc) / iterations
	t.Logf("resize_cpu_bytes_per_iteration=%d", bytes)
	if bytes > 64*1024 {
		t.Fatalf("canvas resize allocates pixel-sized CPU storage: %d bytes/iteration", bytes)
	}
}

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
func TestRetainedBrushSubmissionMatchesStreamPainterOrder(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	size := imgui.Vec2{X: 16, Y: 16}
	c.Process(size)
	reset := func() {
		gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
		gl.Viewport(0, 0, int32(size.X), int32(size.Y))
		gl.ClearColor(1, 0, 1, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
	}
	base := func() { brush.RectFilled(2, 2, 8, 8, util.MakeColor(1, 0, 0, 1)) }
	blueOverlay := func() { brush.RectFilled(5, 5, 10, 10, util.MakeColor(0, 0, 1, 1)) }
	const shiftX, shiftY, scale = 2.0, 1.0, 1.25

	reset()
	base()
	blueOverlay()
	brush.Draw(size.X, size.Y, shiftX, shiftY, scale)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	want := c.ReadPixels()

	submission := brush.CaptureSubmission(base)
	if submission == nil {
		t.Fatal("base primitives produced no retained submission")
	}
	cache := rendercache.New()
	key := rendercache.Key{Chunk: chunk.New(1, 1, 1, 1, 32), Layer: rendercache.LayerKey(1)}
	versions := rendercache.Versions{Chunk: 1, Policy: 2, Appearance: 3}
	if !cache.Put(key, versions, submission) {
		t.Fatal("retained submission was not stored")
	}
	entry, ok := cache.Get(key, versions)
	if !ok || entry.Submission != submission {
		t.Fatal("unchanged cache key did not reuse the retained submission")
	}
	defer func() { cache.Clear(); cache.DisposeRetired() }()
	reset()
	entry.Submission.Draw(size.X, size.Y, shiftX, shiftY, scale)
	blueOverlay()
	brush.Draw(size.X, size.Y, shiftX, shiftY, scale)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	got := c.ReadPixels()
	if !bytes.Equal(got, want) {
		t.Fatal("retained static submission changed pixels or painter order")
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error after retained submission: 0x%x", code)
	}
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - RETAINED RENDER CACHE COUNTERS

type retainedTestPolicy struct {
	revision uint64
	visible  bool
}

func (p *retainedTestPolicy) ProcessUnit(unit.Unit) bool   { return p.visible }
func (p *retainedTestPolicy) RenderPolicyRevision() uint64 { return p.revision }

func retainedTestPrefab(color string) *dmmprefab.Prefab {
	vars := &dmvars.MutableVariables{}
	vars.Put("color", color)
	vars.Put("layer", "1")
	vars.Put("alpha", "255")
	return dmmprefab.New(dmmprefab.IdNone, "/obj/retained-cache-test", vars.ToImmutable())
}

func retainedTestMap() (*dmmap.Dmm, *dmminstance.Instance) {
	point := util.Point{X: 1, Y: 1, Z: 1}
	tile := &dmmap.Tile{Coord: point}
	instance := dmminstance.New(point, retainedTestPrefab(`"#ff0000"`))
	tile.Set(dmmap.Instances{instance})
	return &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}, instance
}

func TestRetainedRenderCacheWarmReuseAndInvalidation(t *testing.T) {
	resizeContext(t)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })

	c := resizeCanvas(t)
	r := c.Render()
	defer r.ReleaseRetainedSubmissions()
	size := imgui.Vec2{X: 96, Y: 96}
	dmm, instance := retainedTestMap()
	r.SetActiveLevel(dmm, 1)
	r.UpdateBucketV(dmm, 1, nil)

	// A ready empty presentation selects the existing stream renderer while
	// producing no ghost geometry, giving the same scene a native baseline.
	streamFrame := func() []byte {
		r.SetPresentation(&render.Presentation{Anchor: util.Point{Z: 1}, Ready: true})
		c.Process(size)
		pixels := c.ReadPixels()
		r.SetPresentation(nil)
		return pixels
	}
	baseline := streamFrame()
	if got := r.RetainedCacheStats(); got.Builds != 0 || got.UploadBytes != 0 {
		t.Fatalf("stream baseline unexpectedly built retained geometry: %+v", got)
	}
	warmRetained := func() {
		for step := 0; step < 8; step++ {
			r.ProcessLevelBuild()
		}
		c.Process(size)
	}

	c.Process(size)
	warmRetained()
	warmPixels := c.ReadPixels()
	if !bytes.Equal(warmPixels, baseline) {
		t.Fatal("retained Render.Draw changed native pixels from stream baseline")
	}
	warm := r.RetainedCacheStats()
	if warm.Builds != 1 || warm.UploadBytes == 0 {
		t.Fatalf("first retained draw did not record one static upload: %+v", warm)
	}

	c.Process(size)
	if got := r.RetainedCacheStats(); got.Builds != warm.Builds || got.UploadBytes != warm.UploadBytes || got.Hits <= warm.Hits {
		t.Fatalf("unchanged warm draw rebuilt or uploaded geometry: before=%+v after=%+v", warm, got)
	}

	// Camera state belongs to the draw transform, not the map-space cache key.
	r.Camera.Translate(8, 4)
	cameraBaseline := streamFrame()
	c.Process(size)
	if got := c.ReadPixels(); !bytes.Equal(got, cameraBaseline) {
		t.Fatal("camera movement changed retained pixels from stream baseline")
	}
	cameraStats := r.RetainedCacheStats()
	if cameraStats.Builds != warm.Builds || cameraStats.UploadBytes != warm.UploadBytes || cameraStats.Hits <= warm.Hits {
		t.Fatalf("camera movement rebuilt a valid map-space submission: before=%+v after=%+v", warm, cameraStats)
	}

	policy := &retainedTestPolicy{revision: 1, visible: true}
	r.SetUnitProcessor(policy)
	c.Process(size)
	warmRetained()
	policyWarm := r.RetainedCacheStats()
	policy.visible = false
	policy.revision++
	policyBaseline := streamFrame()
	c.Process(size)
	warmRetained()
	if got := c.ReadPixels(); !bytes.Equal(got, policyBaseline) {
		t.Fatal("policy change failed to reproduce stream-rendered pixels")
	}
	policyChanged := r.RetainedCacheStats()
	if policyChanged.Invalidations != policyWarm.Invalidations+1 || policyChanged.Builds != policyWarm.Builds+1 {
		t.Fatalf("policy revision did not invalidate and rebuild one chunk-layer: before=%+v after=%+v", policyWarm, policyChanged)
	}

	// Start a fresh default-policy cache, then change the existing map object
	// and rebuild just its owning chunk to exercise the chunk revision fence.
	r.SetUnitProcessor(nil)
	c.Process(size)
	warmRetained()
	editWarm := r.RetainedCacheStats()
	instance.SetPrefab(retainedTestPrefab(`"#00ff00"`))
	r.UpdateBucketV(dmm, 1, []util.Point{{X: 1, Y: 1, Z: 1}})
	editBaseline := streamFrame()
	c.Process(size)
	warmRetained()
	if got := c.ReadPixels(); !bytes.Equal(got, editBaseline) {
		t.Fatal("chunk edit failed to reproduce stream-rendered pixels")
	}
	editChanged := r.RetainedCacheStats()
	if editChanged.Invalidations != editWarm.Invalidations+1 || editChanged.Builds != editWarm.Builds+1 {
		t.Fatalf("chunk revision did not invalidate and rebuild one chunk-layer: before=%+v after=%+v", editWarm, editChanged)
	}
}

// APHELION EDIT ADDITION END - RETAINED RENDER CACHE COUNTERS

// One process selects one fixed fixture size. Alternate dimensions force every
// timed Canvas.Process call to resize; Finish includes completion of GPU work.
func TestRetainedMissDefersUploadToVisualScheduler(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	r := c.Render()
	defer r.ReleaseRetainedSubmissions()
	dmm, _ := retainedTestMap()
	r.SetActiveLevel(dmm, 1)
	r.UpdateBucketV(dmm, 1, nil)
	policy := &retainedTestPolicy{revision: 1, visible: true}
	r.SetUnitProcessor(policy)
	size := imgui.Vec2{X: 96, Y: 96}
	c.Process(size)
	fallback := c.ReadPixels()
	if r.RetainedCacheStats().Builds != 0 {
		t.Fatal("Draw synchronously uploaded a retained miss")
	}
	r.ProcessLevelBuild()
	c.Process(size)
	if !bytes.Equal(fallback, c.ReadPixels()) || r.RetainedCacheStats().Builds != 1 {
		t.Fatal("scheduler failed to warm equivalent pixels")
	}
	policy.visible = false
	policy.revision++
	c.Process(size)
	if r.RetainedCacheStats().Builds != 1 {
		t.Fatal("policy invalidation uploaded during Draw")
	}
	fallback = c.ReadPixels()
	for step := 0; step < 4; step++ {
		r.ProcessLevelBuild()
	}
	c.Process(size)
	if !bytes.Equal(fallback, c.ReadPixels()) || r.RetainedCacheStats().Builds != 2 {
		t.Fatal("new policy was not coherently warmed")
	}
}

func BenchmarkCanvasResize(b *testing.B) {
	resizeContext(b)
	c := resizeCanvas(b)
	width, height := 640, 480
	if os.Getenv("APHELION_CANVAS_BENCH_SIZE") == "1920x1080" {
		width, height = 1920, 1080
	}
	c.Process(imgui.Vec2{X: float32(width + 1), Y: float32(height + 1)})
	gl.Finish()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Process(imgui.Vec2{X: float32(width + i%2), Y: float32(height + i%2)})
		gl.Finish()
	}
	b.StopTimer()
	if code := gl.GetError(); code != gl.NO_ERROR {
		b.Fatalf("GL error: 0x%x", code)
	}
	// Validate the complete final target outside the timed/allocation interval,
	// including the larger benchmark fixture absent from the small pixel sweep.
	pixels := c.ReadPixels()
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 255 || pixels[i+1] != 0 || pixels[i+2] != 255 || pixels[i+3] != 255 {
			b.Fatalf("uncleared final pixel %d", i/4)
		}
	}
	b.Logf("result size=%dx%d sha256=%x", int(c.width), int(c.height), sha256.Sum256(pixels))
}
