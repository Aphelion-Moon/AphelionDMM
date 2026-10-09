package editing

import (
	"fmt"
	"math"
	"strconv"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

type MirrorAxis uint8

const (
	MirrorHorizontal MirrorAxis = iota + 1 // Exchange left/right, negating X offsets.
	MirrorVertical                         // Exchange top/bottom, negating Y offsets.
)

// Mirror reflects visible contents inside the existing selection rectangle.
// Hidden instances retain their coordinates and relative order. Visible source
// order and stable IDs survive the transform, including unknown types/variables.
func Mirror(m *dmmap.Dmm, area util.Bounds, z int, axis MirrorAxis, visible func(string) bool, lookup ...PrefabLookup) (Transform, error) {
	if m == nil || visible == nil {
		return Transform{}, fmt.Errorf("no map or visibility filter")
	}
	if axis != MirrorHorizontal && axis != MirrorVertical {
		return Transform{}, fmt.Errorf("unknown mirror axis")
	}
	for _, value := range []float32{area.X1, area.Y1, area.X2, area.Y2} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value != float32(math.Trunc(float64(value))) {
			return Transform{}, fmt.Errorf("selection must contain whole tiles")
		}
	}
	x1, y1, x2, y2 := int(area.X1), int(area.Y1), int(area.X2), int(area.Y2)
	if x1 > x2 || y1 > y2 || !m.HasTile(util.Point{X: x1, Y: y1, Z: z}) || !m.HasTile(util.Point{X: x2, Y: y2, Z: z}) {
		return Transform{}, fmt.Errorf("selection is outside the map")
	}
	width, height := x2-x1+1, y2-y1+1
	result := Transform{Bounds: area, Tiles: make([]dmmap.Tile, width*height)}
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			coord := util.Point{X: x, Y: y, Z: z}
			target := &result.Tiles[(y-y1)*width+x-x1]
			target.Coord = coord
			for _, instance := range m.GetTile(coord).Instances() {
				if !visible(instance.Prefab().Path()) {
					copy := instance.Copy()
					target.Set(append(target.Instances(), &copy))
				}
			}
		}
	}
	// Work is linear in selected cells/instances, with one orientation conversion
	// per shared immutable prefab. No bounding-square scan or full-map copy.
	prefabs := make(map[*dmmprefab.Prefab]*dmmprefab.Prefab)
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			dx, dy := x, y
			if axis == MirrorHorizontal {
				dx = x2 - (x - x1)
			} else {
				dy = y2 - (y - y1)
			}
			coord := util.Point{X: dx, Y: dy, Z: z}
			target := &result.Tiles[(dy-y1)*width+dx-x1]
			for _, instance := range m.GetTile(util.Point{X: x, Y: y, Z: z}).Instances() {
				if !visible(instance.Prefab().Path()) {
					continue
				}
				prefab, exists := prefabs[instance.Prefab()]
				if !exists {
					var err error
					prefab, err = mirrorPrefab(instance.Prefab(), axis, lookup...)
					if err != nil {
						return Transform{}, fmt.Errorf("%s at (%d,%d,%d): %w", instance.Prefab().Path(), x, y, z, err)
					}
					prefabs[instance.Prefab()] = prefab
				}
				moved := dmminstance.New(coord, prefab)
				moved.SetStableID(instance.StableID())
				target.Set(append(target.Instances(), moved))
			}
		}
	}
	return result, nil
}

func mirrorPrefab(prefab *dmmprefab.Prefab, axis MirrorAxis, lookup ...PrefabLookup) (*dmmprefab.Prefab, error) {
	sourceVars := prefab.Vars()
	vars, path := sourceVars, prefab.Path()
	if vars == nil {
		return nil, fmt.Errorf("missing variables")
	}
	scope := orientationScopeOf(prefab, lookup)
	if scope.none {
		return prefab, nil
	}
	if raw, exists := vars.Value("dir"); exists && scope.dir {
		direction, ok := parseDirection(raw)
		if !ok {
			return nil, fmt.Errorf("cannot mirror dir = %s", raw)
		}
		first, second := dm.DirEast, dm.DirWest
		if axis == MirrorVertical {
			first, second = dm.DirNorth, dm.DirSouth
		}
		mirrored := direction &^ (first | second)
		if direction&first != 0 {
			mirrored |= second
		}
		if direction&second != 0 {
			mirrored |= first
		}
		if mirrored != direction {
			path, vars = setDirection(path, vars, mirrored, lookup)
		}
	}
	offsets := []string{"pixel_x", "step_x"}
	if axis == MirrorVertical {
		offsets = []string{"pixel_y", "step_y"}
	}
	for _, name := range offsets {
		if raw, exists := orientationOffset(sourceVars, name, scope); exists {
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("cannot mirror %s expression", name)
			}
			value = -value
			if value == 0 {
				value = 0 // Normalize negative zero.
			}
			text := strconv.FormatFloat(value, 'f', -1, 64)
			if _, explicit := vars.ExplicitValue(name); !explicit && vars != sourceVars {
				// The new helper supplies this axis; do not restate it.
				if inherited, ok := vars.Value(name); ok && inherited == text {
					continue
				}
			}
			vars = setOffset(vars, name, text)
		}
	}
	if path == prefab.Path() && vars == sourceVars {
		return prefab, nil
	}
	return dmmprefab.New(dmmprefab.IdNone, path, vars), nil
}
