package editing

import (
	"fmt"
	"sort"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

type PlacementTransform uint8

const (
	PlacementRotateRight PlacementTransform = iota + 1
	PlacementRotateLeft
	PlacementMirrorHorizontal
	PlacementMirrorVertical
)

// TransformPlacement transforms the sparse clipboard template at the requested
// target, without creating an operation. Geometry and orientations are validated
// before capture. Failed preflight leaves the old template and preview intact.
func (move *Move) TransformPlacement(transform PlacementTransform, shift util.Point, lookup ...PrefabLookup) ([]util.Point, error) {
	if move.closed || !move.placement {
		return nil, fmt.Errorf("no open paste placement")
	}
	if transform < PlacementRotateRight || transform > PlacementMirrorVertical {
		return nil, fmt.Errorf("unknown paste transform")
	}
	width, height := int(move.origin.X2), int(move.origin.Y2)
	candidate := *move
	if transform == PlacementRotateRight || transform == PlacementRotateLeft {
		candidate.origin.X2, candidate.origin.Y2 = float32(height), float32(width)
	}
	next := candidate.origin.Plus(float32(shift.X), float32(shift.Y))
	if shift.Z != 0 || next.X1 < 1 || next.Y1 < 1 || next.X2 > float32(move.m.MaxX) || next.Y2 > float32(move.m.MaxY) {
		return nil, fmt.Errorf("transformed paste would leave the map; move it inward first")
	}
	candidate.source = make([]dmmap.Tile, 0, len(move.source))
	candidate.sourceCoords = make(map[util.Point]struct{}, len(move.source))
	prefabs := make(map[*dmmprefab.Prefab]*dmmprefab.Prefab)
	for _, tile := range move.source {
		x, y := tile.Coord.X, tile.Coord.Y
		switch transform {
		case PlacementRotateRight:
			x, y = y, width-x+1
		case PlacementRotateLeft:
			x, y = height-y+1, x
		case PlacementMirrorHorizontal:
			x = width - x + 1
		case PlacementMirrorVertical:
			y = height - y + 1
		}
		coord := util.Point{X: x, Y: y, Z: move.z}
		transformed := dmmap.Tile{Coord: coord}
		for _, instance := range tile.Instances() {
			prefab, exists := prefabs[instance.Prefab()]
			if !exists {
				var err error
				switch transform {
				case PlacementRotateRight, PlacementRotateLeft:
					prefab, err = rotatePrefab(instance.Prefab(), transform == PlacementRotateRight, lookup...)
				case PlacementMirrorHorizontal:
					prefab, err = mirrorPrefab(instance.Prefab(), MirrorHorizontal)
				case PlacementMirrorVertical:
					prefab, err = mirrorPrefab(instance.Prefab(), MirrorVertical)
				}
				if err != nil {
					return nil, fmt.Errorf("%s: %w", instance.Prefab().Path(), err)
				}
				prefabs[instance.Prefab()] = prefab
			}
			copy := dmminstance.New(coord, prefab)
			copy.SetStableID(instance.StableID())
			transformed.Set(append(transformed.Instances(), copy))
		}
		candidate.source = append(candidate.source, transformed)
		candidate.sourceCoords[coord] = struct{}{}
	}
	sort.Slice(candidate.source, func(i, j int) bool {
		a, b := candidate.source[i].Coord, candidate.source[j].Coord
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	// An orientation change must redraw even at the same target. Preview captures
	// new destinations before restoring old ones and releases partial acquisitions
	// on failure; the shared background journal is unchanged on that path.
	candidate.previewed = false
	coords, err := candidate.Preview(shift)
	if err != nil {
		return nil, err
	}
	*move = candidate
	return coords, nil
}
