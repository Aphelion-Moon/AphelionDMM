package editing

import (
	"fmt"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"strings"
)

// SelectAreaMask matches map-defined area content, independent of visibility,
// appearance, display names and instance identities. Results remain persistent.
func SelectAreaMask(m *dmmap.Dmm, start util.Point, all bool) (Selection, error) {
	if m == nil || !m.HasTile(start) {
		return Selection{}, fmt.Errorf("select a tile on the map")
	}
	key := func(p util.Point) string {
		for _, instance := range m.GetTile(p).Instances() {
			path := instance.Prefab().Path()
			if path == "/area" || strings.HasPrefix(path, "/area/") {
				return instance.Prefab().ContentKey()
			}
		}
		return ""
	}
	target := key(start)
	if target == "" {
		return Selection{}, fmt.Errorf("selected tile has no area definition")
	}
	var points []util.Point
	if all {
		for x := 1; x <= m.MaxX; x++ {
			for y := 1; y <= m.MaxY; y++ {
				p := util.Point{X: x, Y: y, Z: start.Z}
				if key(p) == target {
					points = append(points, p)
				}
			}
		}
	} else {
		seen := map[util.Point]bool{start: true}
		queue := []util.Point{start}
		for next := 0; next < len(queue); next++ {
			p := queue[next]
			points = append(points, p)
			for _, step := range []util.Point{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}} {
				n := p.Plus(step)
				if !seen[n] && m.HasTile(n) {
					seen[n] = true
					if key(n) == target {
						queue = append(queue, n)
					}
				}
			}
		}
	}
	return MaskSelection(points)
}
