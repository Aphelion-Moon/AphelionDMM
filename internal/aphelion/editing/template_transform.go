package editing

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// TransformPlacementTemplate rotates or mirrors a detached clipboard template
// without reading or mutating the displayed map. It is safe to run on the
// placement worker because prefab values are immutable.
func TransformPlacementTemplate(ctx context.Context, source []dmmap.Tile, transform PlacementTransform) ([]dmmap.Tile, error) {
	if transform < PlacementRotateRight || transform > PlacementMirrorVertical {
		return nil, fmt.Errorf("unknown paste transform")
	}
	if len(source) == 0 {
		return nil, fmt.Errorf("paste template is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	minX, minY, maxX, maxY := source[0].Coord.X, source[0].Coord.Y, source[0].Coord.X, source[0].Coord.Y
	level := source[0].Coord.Z
	seen := make(map[util.Point]struct{}, len(source))
	for _, tile := range source {
		coord := tile.Coord
		if coord.X < 1 || coord.Y < 1 || coord.Z != level {
			return nil, fmt.Errorf("clipboard must contain positive coordinates on one level")
		}
		if _, exists := seen[coord]; exists {
			return nil, fmt.Errorf("clipboard contains duplicate tiles")
		}
		seen[coord] = struct{}{}
		minX, minY = min(minX, coord.X), min(minY, coord.Y)
		maxX, maxY = max(maxX, coord.X), max(maxY, coord.Y)
	}
	result := make([]dmmap.Tile, 0, len(source))
	prefabs := make(map[*dmmprefab.Prefab]*dmmprefab.Prefab)
	for _, tile := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		x, y := tile.Coord.X-minX+1, tile.Coord.Y-minY+1
		oldWidth := maxX - minX + 1
		oldHeight := maxY - minY + 1
		switch transform {
		case PlacementRotateRight:
			x, y = y, oldWidth-x+1
		case PlacementRotateLeft:
			x, y = oldHeight-y+1, x
		case PlacementMirrorHorizontal:
			x = oldWidth - x + 1
		case PlacementMirrorVertical:
			y = oldHeight - y + 1
		}
		coord := util.Point{X: x, Y: y, Z: level}
		transformed := dmmap.Tile{Coord: coord}
		for _, instance := range tile.Instances() {
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				return nil, fmt.Errorf("clipboard contains an invalid instance")
			}
			prefab, exists := prefabs[instance.Prefab()]
			if !exists {
				var err error
				switch transform {
				case PlacementRotateRight, PlacementRotateLeft:
					prefab, err = rotatePrefab(instance.Prefab(), transform == PlacementRotateRight)
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
		result = append(result, transformed)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Coord.Y != result[j].Coord.Y {
			return result[i].Coord.Y < result[j].Coord.Y
		}
		return result[i].Coord.X < result[j].Coord.X
	})
	return result, nil
}
