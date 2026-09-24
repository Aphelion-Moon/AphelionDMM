package editing

import (
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// NewPlacement owns a clipboard template without capturing or changing the map.
// Preview's shift is relative to (1,1) on z. Each destination must fit in full.
// Unlike a move, source coordinates are metadata and never cleared in the map.
func NewPlacement(m *dmmap.Dmm, source []dmmap.Tile, z int, visible func(string) bool, capture func(util.Point) error, regenerate func(*dmmap.Tile), release func(util.Point)) (*Move, error) {
	if m == nil || visible == nil || capture == nil || z < 1 || z > m.MaxZ || len(source) == 0 {
		return nil, fmt.Errorf("paste requires a nonempty selection on a valid map level")
	}
	minX, minY, maxX, maxY := source[0].Coord.X, source[0].Coord.Y, source[0].Coord.X, source[0].Coord.Y
	seen := make(map[util.Point]struct{}, len(source))
	for _, tile := range source {
		c := tile.Coord
		if c.X < 1 || c.Y < 1 || c.Z < 1 || c.Z != source[0].Coord.Z {
			return nil, fmt.Errorf("clipboard must contain positive coordinates on one level")
		}
		if _, exists := seen[c]; exists {
			return nil, fmt.Errorf("clipboard contains duplicate tiles")
		}
		seen[c] = struct{}{}
		minX, minY, maxX, maxY = min(minX, c.X), min(minY, c.Y), max(maxX, c.X), max(maxY, c.Y)
		for _, instance := range tile.Instances() {
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				return nil, fmt.Errorf("clipboard contains an invalid instance")
			}
		}
	}
	width, height := maxX-minX+1, maxY-minY+1
	if width > m.MaxX || height > m.MaxY {
		return nil, fmt.Errorf("clipboard selection is larger than the map")
	}
	area := util.Bounds{X1: 1, Y1: 1, X2: float32(width), Y2: float32(height)}
	p := &Move{m: m, origin: area, bounds: area, z: z, placement: true, sourceCoords: make(map[util.Point]struct{}, len(source)), background: make(map[util.Point]dmmap.Tile), visible: visible, capture: capture, regenerate: regenerate, release: release}
	for _, tile := range source {
		coord := util.Point{X: tile.Coord.X - minX + 1, Y: tile.Coord.Y - minY + 1, Z: z}
		copy := dmmap.Tile{Coord: coord}
		for _, instance := range tile.Instances() {
			if !visible(instance.Prefab().Path()) {
				continue
			}
			id, err := model.NewStableID()
			if err != nil {
				return nil, fmt.Errorf("create pasted instance identity: %w", err)
			}
			pasted := dmminstance.New(coord, instance.Prefab())
			pasted.SetStableID(string(id))
			copy.Set(append(copy.Instances(), pasted))
		}
		p.source = append(p.source, copy)
		p.sourceCoords[coord] = struct{}{}
	}
	sort.Slice(p.source, func(i, j int) bool {
		a, b := p.source[i].Coord, p.source[j].Coord
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return p, nil
}
