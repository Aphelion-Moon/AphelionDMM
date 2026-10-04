package pmap

import (
	"fmt"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"testing"
)

func TestAreaBorderCacheTracksGeometryLevelAndScale(t *testing.T) {
	zones := []editor.AreaZone{{Borders: []editor.AreaBorder{{Coord: util.Point{X: 2, Y: 1, Z: 1}, Dirs: dm.DirEast | dm.DirSouth}, {Coord: util.Point{X: 1, Y: 2, Z: 2}, Dirs: dm.DirNorth}}}}
	var cache areaBorderCache
	first := cache.resolve(1, 1, 32, zones)
	if len(first[0].Borders()) != 2 || first[0].Borders()[0].X1 != 64 {
		t.Fatal("incorrect initial lines")
	}
	if allocs := testing.AllocsPerRun(100, func() { cache.resolve(1, 1, 32, zones) }); allocs != 0 {
		t.Fatal("unchanged borders allocate", allocs)
	}
	second := cache.resolve(1, 2, 32, zones)
	if len(second[0].Borders()) != 1 || second[0].Borders()[0].Y1 != 64 {
		t.Fatal("level reused stale lines")
	}
	if cache.resolve(1, 2, 64, zones)[0].Borders()[0].Y1 != 128 {
		t.Fatal("icon size reused stale geometry")
	}
	zones[0].Borders[1].Dirs = dm.DirWest
	if cache.resolve(2, 2, 64, zones)[0].Borders()[0].X2 != 0 {
		t.Fatal("accepted area change reused stale geometry")
	}
	if len(first[0].Borders()) != 2 {
		t.Fatal("cache replacement mutated prior frame")
	}
}

func BenchmarkAreaBorderCacheAreaEdit(b *testing.B) {
	zones := make([]editor.AreaZone, 128)
	for i := range zones {
		zones[i].Name = fmt.Sprintf("/area/zone%d", i)
		zones[i].Generation = 1
		zones[i].Borders = make([]editor.AreaBorder, 256)
		for j := range zones[i].Borders {
			zones[i].Borders[j] = editor.AreaBorder{Coord: util.Point{X: j + 1, Y: i + 1, Z: 1}, Dirs: dm.DirNorth | dm.DirSouth}
		}
	}
	var cache areaBorderCache
	cache.resolve(1, 1, 32, zones)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		zones[0].Borders[0].Dirs ^= dm.DirEast
		zones[0].Generation = uint64(i) + 2
		cache.resolve(uint64(i)+2, 1, 32, zones)
	}
}

func TestAreaBorderCacheReusesUnchangedZones(t *testing.T) {
	zones := []editor.AreaZone{
		{Name: "/area/a", Generation: 1, Borders: []editor.AreaBorder{{Coord: util.Point{X: 1, Y: 1, Z: 1}, Dirs: dm.DirNorth}}},
		{Name: "/area/b", Generation: 1, Borders: []editor.AreaBorder{{Coord: util.Point{X: 2, Y: 1, Z: 1}, Dirs: dm.DirSouth}}},
	}
	var cache areaBorderCache
	first := cache.resolve(1, 1, 32, zones)
	if len(first) != 2 {
		t.Fatal("expected independently retained zone geometry", len(first))
	}
	zones[0].Generation = 2
	zones[0].Borders[0].Dirs = dm.DirEast
	second := cache.resolve(2, 1, 32, zones)
	if len(second) != 2 || second[0].Borders()[0] != (util.Bounds{X1: 32, Y1: 0, X2: 32, Y2: 32}) {
		t.Fatal("changed area kept stale borders")
	}
	if &first[1].Borders()[0] != &second[1].Borders()[0] {
		t.Fatal("unaffected area geometry was rebuilt")
	}
	if first[0].Borders()[0] != (util.Bounds{X1: 0, Y1: 32, X2: 32, Y2: 32}) {
		t.Fatal("area edit mutated a retained frame")
	}
	removed := cache.resolve(3, 1, 32, zones[1:])
	if len(removed) != 1 || &removed[0].Borders()[0] != &first[1].Borders()[0] {
		t.Fatal("removed area survived or reordered area rebuilt")
	}
	zones[0].Generation = 4
	zones[0].Borders[0].Dirs = dm.DirWest
	restored := cache.resolve(4, 1, 32, zones)
	if len(restored) != 2 || restored[0].Borders()[0] != (util.Bounds{X1: 0, Y1: 0, X2: 0, Y2: 32}) {
		t.Fatal("restored area reused removed geometry")
	}
}
