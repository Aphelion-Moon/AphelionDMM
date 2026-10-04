package canvas

import (
	"bytes"
	"math"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"sdmm/internal/aphelion/rendercache"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func TestRetiredSubmissionReplacementOwnershipAndPixels(t *testing.T) {
	resizeContext(t)
	c := resizeCanvas(t)
	size := imgui.Vec2{X: 16, Y: 16}
	c.Process(size)
	cache := rendercache.New()
	defer func() { cache.Clear(); cache.DisposeRetired() }()
	key := rendercache.Key{Chunk: chunk.New(1, 1, 1, 1, 32)}
	budget := resources.DefaultBudget()
	base := budget.Used()
	reset := func() {
		gl.BindFramebuffer(gl.FRAMEBUFFER, c.frameBuffer)
		gl.Viewport(0, 0, 16, 16)
		gl.ClearColor(1, 0, 1, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
	}
	behind := func() { brush.RectFilled(1, 1, 12, 12, util.MakeColor(1, 1, 0, 1)) }
	ahead := func() { brush.RectFilled(5, 5, 10, 10, util.MakeColor(0, 0, 1, 1)) }
	builds := []func(){
		func() { brush.RectFilled(2, 2, 8, 8, util.MakeColor(1, 0, 0, 1)) },
		func() {
			brush.RectFilled(2, 2, 9, 9, util.MakeColor(0, 1, 0, 1))
			brush.Line(0, 3, 15, 3, util.MakeColor(1, 0, 0, 1))
			brush.RectFilled(0, 0, 4, 4, util.MakeColor(0, 1, 1, 1))
		},
		func() { brush.Line(0, 7, 15, 7, util.MakeColor(0, 1, 0, 1)) },
	}
	var previous *brush.Submission
	var oldEstimate uint64
	for i, build := range builds {
		versions := rendercache.Versions{Chunk: uint64(i + 1)}
		if _, found := cache.Get(key, versions); found {
			t.Fatal("changed version retained stale geometry")
		}
		reset()
		behind()
		build()
		ahead()
		brush.Draw(16, 16, 1, 2, 1.1)
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		want := c.ReadPixels()

		reset()
		behind() // Capture must leave this earlier frame geometry intact.
		estimate := uint64(4096)
		if i == 1 {
			estimate = 8192
		}
		submission, err := cache.CaptureAdmittedSubmission(estimate, func() {
			if got := budget.Used(); got != base+oldEstimate+estimate {
				t.Errorf("replacement lost peak reservation: got %d want %d", got, base+oldEstimate+estimate)
			}
			build()
		})
		if err != nil || submission == nil {
			t.Fatalf("capture %d failed: %v", i, err)
		}
		if !cache.Put(key, versions, submission) {
			submission.Dispose()
			t.Fatal("replacement was not retained")
		}
		if previous != nil && submission != previous {
			t.Error("replacement allocated a new submission instead of reusing its retired owner")
		}
		if cache.HasRetired() || budget.Used() != base+estimate {
			t.Fatalf("replacement did not transfer retirement ownership and reservation: used=%d", budget.Used())
		}
		cache.DisposeRetired() // Must not delete the now-live replacement.
		submission.Draw(16, 16, 1, 2, 1.1)
		ahead()
		brush.Draw(16, 16, 1, 2, 1.1)
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		if !bytes.Equal(c.ReadPixels(), want) {
			t.Fatalf("replacement %d changed pixels, draw modes, or painter order", i)
		}
		previous, oldEstimate = submission, estimate
	}
	if cache.Stats().Reuses != 2 {
		t.Errorf("expected two reused uploads, got %+v", cache.Stats())
	}

	cache.Clear()
	empty, err := cache.CaptureAdmittedSubmission(4096, func() {})
	if err != nil || empty != nil || cache.HasRetired() || budget.Used() != base {
		t.Fatalf("empty replacement leaked its resources: submission=%v err=%v used=%d", empty, err, budget.Used())
	}
	for _, panicBuild := range []bool{false, true} {
		submission, err := cache.CaptureAdmittedSubmission(4096, builds[0])
		if err != nil || !cache.Put(key, rendercache.Versions{}, submission) {
			t.Fatalf("prepare discarded replacement: %v", err)
		}
		cache.Clear()
		if panicBuild {
			func() {
				defer func() {
					if recover() != "capture interrupted" {
						t.Error("capture panic was lost")
					}
				}()
				_, _ = cache.CaptureAdmittedSubmission(8192, func() { panic("capture interrupted") })
			}()
		} else {
			called := false
			denied, err := cache.CaptureAdmittedSubmission(math.MaxUint64, func() { called = true })
			if err == nil || denied != nil || called {
				t.Fatal("denied replacement allocated or captured geometry")
			}
		}
		if cache.HasRetired() || budget.Used() != base {
			t.Fatalf("discarded replacement leaked resources: panic=%v used=%d baseline=%d", panicBuild, budget.Used(), base)
		}
	}
	if code := gl.GetError(); code != gl.NO_ERROR {
		t.Fatalf("GL error after retired replacements: 0x%x", code)
	}
}

func BenchmarkRetainedChunkRebuild(b *testing.B) {
	resizeContext(b)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	b.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })
	c := resizeCanvas(b)
	r := c.Render()
	b.Cleanup(func() { r.CancelLevelBuilds(); r.ReleaseRetainedSubmissions() })
	policy := &retainedTestPolicy{revision: 1, visible: true}
	r.SetUnitProcessor(policy)
	dmm, _ := retainedTestMap()
	r.SetActiveLevel(dmm, 1)
	r.UpdateBucketV(dmm, 1, nil)
	size := imgui.Vec2{X: 96, Y: 96}
	for i := 0; i < 8; i++ {
		c.Process(size)
		r.ProcessLevelBuild()
	}
	warm := r.RetainedCacheStats()
	if warm.Builds != 1 {
		b.Fatalf("expected one warm chunk-layer, got %+v", warm)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.revision++
		c.Process(size)
		for step := 0; step < 16 && r.RetainedCacheStats().Builds == warm.Builds+uint64(i); step++ {
			r.ProcessLevelBuild()
		}
		c.Process(size)
	}
	b.StopTimer()
	after := r.RetainedCacheStats()
	if after.Builds != warm.Builds+uint64(b.N) {
		b.Fatalf("not every invalidation rebuilt: before=%+v after=%+v", warm, after)
	}
	b.ReportMetric(float64(after.Reuses-warm.Reuses)/float64(b.N), "reuses/op")
}
