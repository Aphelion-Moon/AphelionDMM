package window_test

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/render"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func newPickingScene(tb testing.TB, width, height int) (*render.Render, *dmmap.Dmm) {
	tb.Helper()
	if lifecycleWindow == nil {
		tb.Skip("set APHELIONDMM_GL_TEST=1 for native picking checks")
	}
	runtime.LockOSThread()
	tb.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	tb.Cleanup(glfw.DetachCurrentContext)
	previousIconSize := dmmap.WorldIconSize
	dmmap.WorldIconSize = 32
	tb.Cleanup(func() { dmmap.WorldIconSize = previousIconSize })
	m := &dmmap.Dmm{MaxX: width, MaxY: height, MaxZ: 1}
	for y := 1; y <= height; y++ {
		for x := 1; x <= width; x++ {
			m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}})
		}
	}
	r := render.New()
	tb.Cleanup(r.CancelLevelBuilds)
	return r, m
}

func addPickingInstance(m *dmmap.Dmm, x, y int, values map[string]string) *dmminstance.Instance {
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "2")
	for name, value := range values {
		vars.Put(name, value)
	}
	tile := m.GetTile(util.Point{X: x, Y: y, Z: 1})
	tile.InstancesAdd(dmmprefab.New(0, "/obj/picking", vars.ToImmutable()))
	return tile.Instances()[len(tile.Instances())-1]
}

func TestNativePickAtStopsAtTopmostHit(t *testing.T) {
	r, m := newPickingScene(t, 1, 1)
	var top *dmminstance.Instance
	for range 64 {
		top = addPickingInstance(m, 1, 1, nil)
	}
	for range 64 {
		addPickingInstance(m, 1, 1, map[string]string{"pixel_x": "32"})
	}
	r.UpdateBucket(m, 1)
	calls := 0
	got := r.PickAt(16, 16, 1, func(*dmminstance.Instance) bool { calls++; return true })
	if got != top {
		t.Fatal("pick did not return the last drawn opaque instance")
	}
	if calls != 1 {
		t.Fatalf("pick inspected %d candidates despite an opaque topmost hit, want 1", calls)
	}
}

func TestNativePickAtPreservesOrderAndEligibility(t *testing.T) {
	r, m := newPickingScene(t, 26, 1)
	first := addPickingInstance(m, 1, 1, map[string]string{"layer": "1"})
	laterChunk := addPickingInstance(m, 26, 1, map[string]string{"layer": "1", "pixel_x": "-800"})
	higherLayer := addPickingInstance(m, 1, 1, map[string]string{"layer": "2"})
	addPickingInstance(m, 26, 1, map[string]string{"layer": "3", "pixel_x": "-800", "alpha": "0"})
	addPickingInstance(m, 26, 1, map[string]string{"layer": "4"})
	r.UpdateBucket(m, 1)
	for _, tc := range []struct {
		name     string
		want     *dmminstance.Instance
		eligible func(*dmminstance.Instance) bool
	}{
		{"layer before chunk", higherLayer, func(*dmminstance.Instance) bool { return true }},
		{"later overlapping chunk", laterChunk, func(i *dmminstance.Instance) bool { return i != higherLayer }},
		{"filtered upper instances", first, func(i *dmminstance.Instance) bool { return i == first }},
		{"all filtered", nil, func(*dmminstance.Instance) bool { return false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.PickAt(16, 16, 1, tc.eligible); got != tc.want {
				t.Fatalf("picked %p, want %p", got, tc.want)
			}
		})
	}
	if got := r.PickAt(-100, -100, 1, func(*dmminstance.Instance) bool { return true }); got != nil {
		t.Fatal("out-of-bounds point picked an instance")
	}
	r.InvalidateLevelBuilds(m)
	if got := r.PickAt(16, 16, 1, func(*dmminstance.Instance) bool { return true }); got != nil {
		t.Fatal("invalidated geometry remained pickable")
	}
}

func BenchmarkNativePickAt(b *testing.B) {
	r, m := newPickingScene(b, 25, 25)
	var want *dmminstance.Instance
	for y := 1; y <= 25; y++ {
		for x := 1; x <= 25; x++ {
			for layer := 1; layer <= 4; layer++ {
				instance := addPickingInstance(m, x, y, map[string]string{"layer": strconv.Itoa(layer)})
				if x == 13 && y == 13 {
					want = instance
				}
			}
		}
	}
	r.UpdateBucket(m, 1)
	visible := dm.NewPathsFilterEmpty()
	hidden := dm.NewPathsFilterEmpty()
	hidden.ApplyHiddenPaths([]string{"/obj/picking"})
	for _, tc := range []struct {
		name   string
		filter *dm.PathsFilter
		x, y   int
		want   *dmminstance.Instance
	}{
		{"hit", visible, 400, 400, want},
		{"filtered", hidden, 400, 400, nil},
		{"outside", visible, -16, 400, nil},
	} {
		b.Run(tc.name, func(b *testing.B) {
			eligible := func(i *dmminstance.Instance) bool {
				return i != nil && i.Prefab() != nil && tc.filter.IsVisiblePath(i.Prefab().Path())
			}
			b.ReportAllocs()
			for b.Loop() {
				if got := r.PickAt(tc.x, tc.y, 1, eligible); got != tc.want {
					b.Fatal("picked wrong instance")
				}
			}
		})
	}
}
