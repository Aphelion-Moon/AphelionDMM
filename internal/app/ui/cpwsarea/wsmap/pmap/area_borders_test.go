package pmap

import (
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
