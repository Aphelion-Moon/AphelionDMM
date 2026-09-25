package canvas

import (
	"bytes"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/render"
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

func TestRetainedHighlightBypassesOnlyMatchingChunkLayer(t *testing.T) {
	resizeContext(t)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	t.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })
	c := resizeCanvas(t)
	r := c.Render()
	defer r.ReleaseRetainedSubmissions()

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
		t.Fatalf("expected two warm chunk-layer submissions, got %+v", warm)
	}

	r.SetOverlay(&retainedHighlightOverlay{units: map[uint64]render.HighlightUnit{
		instances[0].Id(): retainedHighlight{color: util.MakeColor(0, 1, 0, 0.5)},
	}})
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
