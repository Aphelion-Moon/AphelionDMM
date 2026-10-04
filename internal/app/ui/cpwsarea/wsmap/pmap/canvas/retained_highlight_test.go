package canvas

import (
	"bytes"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type retainedHighlight struct{ color util.Color }

func (h retainedHighlight) Id() uint64        { return 0 }
func (h retainedHighlight) Color() util.Color { return h.color }

type retainedHighlightOverlay struct {
	units map[uint64]render.HighlightUnit
}

func (o *retainedHighlightOverlay) Areas() []render.OverlayArea            { return nil }
func (o *retainedHighlightOverlay) FlushAreas()                            {}
func (o *retainedHighlightOverlay) Units() map[uint64]render.HighlightUnit { return o.units }
func (o *retainedHighlightOverlay) FlushUnits()                            {}
func (o *retainedHighlightOverlay) AreasBorders() []render.AreaBorder      { return nil }
func (o *retainedHighlightOverlay) FlushAreasBorders()                     {}

func retainedHighlightPrefab() *dmmprefab.Prefab {
	vars := &dmvars.MutableVariables{}
	vars.Put("color", `"#ff0000"`)
	vars.Put("layer", "1")
	vars.Put("alpha", "255")
	return dmmprefab.New(dmmprefab.IdNone, "/obj/retained-highlight-test", vars.ToImmutable())
}

func retainedHighlightMap() (*dmmap.Dmm, []*dmminstance.Instance) {
	const maxX = 26
	tiles := make([]*dmmap.Tile, maxX)
	for x := 1; x <= maxX; x++ {
		point := util.Point{X: x, Y: 1, Z: 1}
		tiles[x-1] = &dmmap.Tile{Coord: point}
	}
	instances := []*dmminstance.Instance{}
	for _, x := range []int{1, 26} {
		point := util.Point{X: x, Y: 1, Z: 1}
		instance := dmminstance.New(point, retainedHighlightPrefab())
		tiles[x-1].Set(dmmap.Instances{instance})
		instances = append(instances, instance)
	}
	return &dmmap.Dmm{MaxX: maxX, MaxY: 1, MaxZ: 1, Tiles: tiles}, instances
}

func warmRetainedHighlightCanvas(tb testing.TB) (*Canvas, []*dmminstance.Instance, imgui.Vec2) {
	tb.Helper()
	resizeContext(tb)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	tb.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })
	c := resizeCanvas(tb)
	r := c.Render()
	tb.Cleanup(func() {
		r.CancelLevelBuilds()
		r.ReleaseRetainedSubmissions()
	})

	dmm, instances := retainedHighlightMap()
	r.SetActiveLevel(dmm, 1)
	r.UpdateBucketV(dmm, 1, nil)
	size := imgui.Vec2{X: 896, Y: 64}
	c.Process(size)
	for step := 0; step < 8; step++ {
		r.ProcessLevelBuild()
	}
	c.Process(size)
	warm := r.RetainedCacheStats()
	if warm.Builds != 2 || warm.UploadBytes == 0 {
		tb.Fatalf("expected two warm chunk-layer submissions, got %+v", warm)
	}
	return c, instances, size
}

func unrelatedHighlights(count int) map[uint64]render.HighlightUnit {
	units := make(map[uint64]render.HighlightUnit, count)
	for i := 0; i < count; i++ {
		units[uint64(1)<<63+uint64(i)] = retainedHighlight{color: util.MakeColor(0, 1, 0, 0.5)}
	}
	return units
}

func TestRetainedHighlightBypassesOnlyMatchingChunkLayer(t *testing.T) {
	c, instances, size := warmRetainedHighlightCanvas(t)
	r := c.Render()
	warm := r.RetainedCacheStats()

	units := unrelatedHighlights(10000)
	units[instances[0].Id()] = retainedHighlight{color: util.MakeColor(0, 1, 0, 0.5)}
	r.SetOverlay(&retainedHighlightOverlay{units: units})
	r.SetPresentation(&render.Presentation{Anchor: util.Point{Z: 1}, Ready: true})
	c.Process(size)
	streamPixels := c.ReadPixels()
	r.SetPresentation(nil)
	c.Process(size)
	if got := c.ReadPixels(); !bytes.Equal(got, streamPixels) {
		t.Fatal("selective retained rendering changed highlighted painter-order pixels")
	}
	retained := r.RetainedCacheStats()
	if retained.Builds != warm.Builds || retained.UploadBytes != warm.UploadBytes || retained.Hits <= warm.Hits {
		t.Fatalf("one highlighted chunk-layer disabled or rebuilt the whole level: warm=%+v after=%+v", warm, retained)
	}
	r.SetOverlay(nil)
}

func TestRetainedHighlightLookupDoesNotAllocatePerHighlight(t *testing.T) {
	c, _, size := warmRetainedHighlightCanvas(t)
	overlay := &retainedHighlightOverlay{}
	c.Render().SetOverlay(overlay)
	empty := testing.AllocsPerRun(10, func() { c.Process(size) })
	overlay.units = unrelatedHighlights(10000)
	populated := testing.AllocsPerRun(10, func() { c.Process(size) })
	if populated > empty {
		t.Fatalf("unrelated highlights add %.0f allocations per frame (empty=%.0f, populated=%.0f)", populated-empty, empty, populated)
	}
}

func BenchmarkRetainedHighlightFrame(b *testing.B) {
	for _, tc := range []struct {
		name    string
		count   int
		visible bool
	}{
		{name: "none"},
		{name: "hover", visible: true},
		{name: "flashes", count: 10000, visible: true},
		{name: "offscreen_flashes", count: 10000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c, instances, size := warmRetainedHighlightCanvas(b)
			units := unrelatedHighlights(tc.count)
			if tc.visible {
				units[instances[0].Id()] = retainedHighlight{color: util.MakeColor(0, 1, 0, 0.5)}
			}
			c.Render().SetOverlay(&retainedHighlightOverlay{units: units})
			c.Process(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.Process(size)
			}
		})
	}
}

func TestRetainedMovePreviewReusesOnlyUnaffectedChunks(t *testing.T) {
	c, instances, size := warmRetainedHighlightCanvas(t)
	r := c.Render()
	baseline := c.ReadPixels()
	source := instances[0].Coord()
	sourceBounds := util.Bounds{X1: float32(source.X), Y1: float32(source.Y), X2: float32(source.X), Y2: float32(source.Y)}
	ghost := &render.Presentation{Anchor: util.Point{X: 2, Y: 1, Z: 1}, IconSize: 32}
	appearance := render.PrepareAppearance(util.Point{X: 1, Y: 1, Z: 1}, instances[0], 32)
	appearance.R, appearance.G, appearance.B, appearance.A = 0, 1, 0, 0.5
	ghost.Add(appearance)
	ghost.Finish()
	destinationBounds := func() util.Bounds {
		return sourceBounds.Plus(float32(ghost.Anchor.X-source.X), float32(ghost.Anchor.Y-source.Y))
	}
	ghost.Suppress = func(u unit.Unit) bool {
		coord := u.Instance().Coord()
		return sourceBounds.Contains(float32(coord.X), float32(coord.Y)) || destinationBounds().Contains(float32(coord.X), float32(coord.Y))
	}
	maySuppress := func(bounds util.Bounds) bool {
		return bounds.ContainsV(sourceBounds) || bounds.ContainsV(destinationBounds())
	}
	r.SetPresentation(ghost)
	for _, tc := range []struct {
		x, width int
		wantHit  bool
	}{{2, 1, true}, {26, 1, false}, {2, 25, false}, {2, 1, true}} {
		x := tc.x
		ghost.Anchor.X = x
		sourceBounds.X2 = sourceBounds.X1 + float32(tc.width-1)
		ghost.MaySuppress = nil // The existing stream renderer is the pixel oracle.
		c.Process(size)
		want := c.ReadPixels()
		before := r.RetainedCacheStats()
		ghost.MaySuppress = maySuppress
		c.Process(size)
		if !bytes.Equal(c.ReadPixels(), want) {
			t.Fatalf("move to x=%d changed suppression or painter order", x)
		}
		after := r.RetainedCacheStats()
		if after.Builds != before.Builds || after.UploadBytes != before.UploadBytes {
			t.Fatal("moving preview rebuilt committed submissions")
		}
		if tc.wantHit && after.Hits <= before.Hits {
			t.Fatal("moving preview disabled unaffected chunks")
		}
		if !tc.wantHit && after.Hits != before.Hits {
			t.Fatal("source/destination chunk reused unsuppressed geometry")
		}
	}
	r.SetPresentation(nil)
	c.Process(size)
	if !bytes.Equal(c.ReadPixels(), baseline) {
		t.Fatal("ending preview changed committed pixels")
	}
}
