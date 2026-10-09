package ingame

import (
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// DmmMap reads a live editor map. Use it on the thread that owns the map.
func DmmMap(dmm *dmmap.Dmm) Map { return dmmView{dmm} }

type dmmView struct{ dmm *dmmap.Dmm }

func (v dmmView) Prefabs(x, y, z int) ([]*dmmprefab.Prefab, bool) {
	p := util.Point{X: x, Y: y, Z: z}
	if v.dmm == nil || !v.dmm.HasTile(p) {
		return nil, false
	}
	return v.dmm.GetTile(p).Instances().Prefabs(), true
}

// Neighborhood returns points plus every in-bounds tile next to them on the
// same level, without duplicates. An edit changes how its neighbours connect.
func Neighborhood(dmm *dmmap.Dmm, points []util.Point) []util.Point {
	if dmm == nil || len(points) == 0 {
		return points
	}
	seen := make(map[util.Point]bool, len(points)*9)
	out := make([]util.Point, 0, len(points)*9)
	for _, p := range points {
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				n := util.Point{X: p.X + dx, Y: p.Y + dy, Z: p.Z}
				if !seen[n] && dmm.HasTile(n) {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	return out
}
