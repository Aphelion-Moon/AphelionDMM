package editing

import (
	"fmt"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// TransformSelection plans only the actual source/destination union. Destinations
// are read before any mutation, including when the two footprints overlap.
func TransformSelection(m *dmmap.Dmm, selection Selection, destination func(util.Point) util.Point, prefabTransform func(*dmmprefab.Prefab) (*dmmprefab.Prefab, error), visible func(string) bool) (Transform, error) {
	if m == nil || visible == nil || selection.Len() == 0 {
		return Transform{}, fmt.Errorf("no selection or map")
	}
	points := selection.Coordinates()
	destinations := make([]util.Point, len(points))
	union := append([]util.Point(nil), points...)
	for i, p := range points {
		d := destination(p)
		if !m.HasTile(p) || !m.HasTile(d) || p.Z != d.Z {
			return Transform{}, fmt.Errorf("transformed selection leaves the map or level")
		}
		destinations[i] = d
		union = append(union, d)
	}
	footprint, err := MaskSelection(union)
	if err != nil {
		return Transform{}, err
	}
	mask, err := MaskSelection(destinations)
	if err != nil {
		return Transform{}, err
	}
	result := Transform{Bounds: mask.Bounds()}
	indices := make(map[util.Point]int, footprint.Len())
	footprint.Visit(func(p util.Point) {
		tile := dmmap.Tile{Coord: p}
		for _, instance := range m.GetTile(p).Instances() {
			if !visible(instance.Prefab().Path()) {
				copy := instance.Copy()
				tile.Set(append(tile.Instances(), &copy))
			}
		}
		indices[p] = len(result.Tiles)
		result.Tiles = append(result.Tiles, tile)
	})
	prefabs := make(map[*dmmprefab.Prefab]*dmmprefab.Prefab)
	for i, p := range points {
		d := destinations[i]
		target := &result.Tiles[indices[d]]
		for _, instance := range m.GetTile(p).Instances() {
			if !visible(instance.Prefab().Path()) {
				continue
			}
			prefab, ok := prefabs[instance.Prefab()]
			if !ok {
				prefab = instance.Prefab()
				if prefabTransform != nil {
					prefab, err = prefabTransform(prefab)
					if err != nil {
						return Transform{}, err
					}
				}
				prefabs[instance.Prefab()] = prefab
			}
			copy := dmminstance.New(d, prefab)
			copy.SetStableID(instance.StableID())
			target.Set(append(target.Instances(), copy))
		}
	}
	return result, nil
}

func RotateMask(m *dmmap.Dmm, s Selection, clockwise bool, visible func(string) bool) (Transform, error) {
	a := s.Bounds()
	w, h := int(a.X2-a.X1+1), int(a.Y2-a.Y1+1)
	return TransformSelection(m, s, func(p util.Point) util.Point {
		x, y := p.X-int(a.X1), p.Y-int(a.Y1)
		dx, dy := y, w-1-x
		if !clockwise {
			dx, dy = h-1-y, x
		}
		return util.Point{X: int(a.X1) + dx, Y: int(a.Y1) + dy, Z: p.Z}
	}, func(p *dmmprefab.Prefab) (*dmmprefab.Prefab, error) { return rotatePrefab(p, clockwise) }, visible)
}
func MirrorMask(m *dmmap.Dmm, s Selection, axis MirrorAxis, visible func(string) bool) (Transform, error) {
	if axis != MirrorHorizontal && axis != MirrorVertical {
		return Transform{}, fmt.Errorf("unknown mirror axis")
	}
	a := s.Bounds()
	return TransformSelection(m, s, func(p util.Point) util.Point {
		if axis == MirrorHorizontal {
			p.X = int(a.X1+a.X2) - p.X
		} else {
			p.Y = int(a.Y1+a.Y2) - p.Y
		}
		return p
	}, func(p *dmmprefab.Prefab) (*dmmprefab.Prefab, error) { return mirrorPrefab(p, axis) }, visible)
}
