// Package editing contains map editing plans independent of concrete UI widgets.
package editing

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// Rotation is a fully validated edit plan. The caller captures every before-state
// before applying any tile, then submits the whole plan as one operation.
type Rotation = Transform

// Rotate turns visible contents by 90 degrees around the selection's bottom-left
// anchor. Hidden instances stay put; visible destination contents are replaced.
// It reads current tiles, never a previous gesture's potentially stale copies.
func Rotate(m *dmmap.Dmm, area util.Bounds, z int, clockwise bool, visible func(string) bool, lookup ...PrefabLookup) (Rotation, error) {
	if m == nil || visible == nil {
		return Rotation{}, fmt.Errorf("no map or visibility filter")
	}
	for _, value := range []float32{area.X1, area.Y1, area.X2, area.Y2} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value != float32(math.Trunc(float64(value))) {
			return Rotation{}, fmt.Errorf("selection must contain whole tiles")
		}
	}
	x1, y1, x2, y2 := int(area.X1), int(area.Y1), int(area.X2), int(area.Y2)
	if x1 > x2 || y1 > y2 || !m.HasTile(util.Point{X: x1, Y: y1, Z: z}) || !m.HasTile(util.Point{X: x2, Y: y2, Z: z}) {
		return Rotation{}, fmt.Errorf("selection is outside the map")
	}
	width, height := x2-x1+1, y2-y1+1
	if height > m.MaxX-x1+1 || width > m.MaxY-y1+1 {
		return Rotation{}, fmt.Errorf("rotated selection would leave the map; move it inward first")
	}
	result := Rotation{Bounds: util.Bounds{X1: area.X1, Y1: area.Y1, X2: float32(x1 + height - 1), Y2: float32(y1 + width - 1)}}
	indices := make(map[util.Point]int)
	// Deterministic union of source and destination rectangles, once per tile.
	for y := y1; y <= max(y2, y1+width-1); y++ {
		// A long, thin selection must cost O(selected cells), not O(long side²).
		rowEnd := x1 - 1
		if y <= y2 {
			rowEnd = x2
		}
		if y < y1+width {
			rowEnd = max(rowEnd, x1+height-1)
		}
		for x := x1; x <= rowEnd; x++ {
			coord := util.Point{X: x, Y: y, Z: z}
			tile := dmmap.Tile{Coord: coord}
			var retained dmmap.Instances
			for _, i := range m.GetTile(coord).Instances() {
				if !visible(i.Prefab().Path()) {
					copy := i.Copy()
					retained = append(retained, &copy)
				}
			}
			tile.Set(retained)
			indices[coord] = len(result.Tiles)
			result.Tiles = append(result.Tiles, tile)
		}
	}
	// Prefabs are immutable and commonly shared by many cells: transform each once.
	prefabs := make(map[*dmmprefab.Prefab]*dmmprefab.Prefab)
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			dx, dy := y-y1, width-1-(x-x1)
			if !clockwise {
				dx, dy = height-1-(y-y1), x-x1
			}
			coord := util.Point{X: x1 + dx, Y: y1 + dy, Z: z}
			destination := &result.Tiles[indices[coord]]
			for _, i := range m.GetTile(util.Point{X: x, Y: y, Z: z}).Instances() {
				if !visible(i.Prefab().Path()) {
					continue
				}
				prefab, exists := prefabs[i.Prefab()]
				if !exists {
					var err error
					prefab, err = rotatePrefab(i.Prefab(), clockwise, lookup...)
					if err != nil {
						return Rotation{}, fmt.Errorf("%s at (%d,%d,%d): %w", i.Prefab().Path(), x, y, z, err)
					}
					prefabs[i.Prefab()] = prefab
				}
				moved := dmminstance.New(coord, prefab)
				moved.SetStableID(i.StableID())
				destination.Set(append(destination.Instances(), moved))
			}
		}
	}
	return result, nil
}

func rotatePrefab(prefab *dmmprefab.Prefab, clockwise bool, lookup ...PrefabLookup) (*dmmprefab.Prefab, error) {
	sourceVars := prefab.Vars()
	vars, path := sourceVars, prefab.Path()
	if vars == nil {
		return nil, fmt.Errorf("missing variables")
	}
	if raw, ok := vars.Value("dir"); ok {
		direction, ok := parseDirection(raw)
		if !ok {
			return nil, fmt.Errorf("cannot rotate dir = %s", raw)
		}
		rotated := 0
		for _, pair := range [][2]int{{dm.DirNorth, dm.DirEast}, {dm.DirEast, dm.DirSouth}, {dm.DirSouth, dm.DirWest}, {dm.DirWest, dm.DirNorth}} {
			from, to := pair[0], pair[1]
			if !clockwise {
				from, to = to, from
			}
			if direction&from != 0 {
				rotated |= to
			}
		}
		if targetPath, targetVars := directionalVariant(path, rotated, lookup); targetVars != nil {
			path = targetPath
			explicit := &dmvars.MutableVariables{}
			for _, name := range vars.Iterate() {
				value, _ := vars.ExplicitValue(name)
				explicit.Put(name, value)
			}
			vars = explicit.ToImmutable()
			vars.LinkParent(targetVars)
		}
		vars = setOrientation(vars, "dir", strconv.Itoa(rotated))
	}
	for _, pair := range [][2]string{{"pixel_x", "pixel_y"}, {"step_x", "step_y"}, {"pixel_w", "pixel_z"}} {
		if pair[0] == "pixel_x" && strings.HasPrefix(path, prefab.Path()+"/directional/") {
			_, explicitX := sourceVars.ExplicitValue(pair[0])
			_, explicitY := sourceVars.ExplicitValue(pair[1])
			if !explicitX && !explicitY {
				// Base types adopt their declared wall-mounting defaults.
				// Explicit map offsets still rotate as a pair below.
				continue
			}
		}
		x, hasX := sourceVars.Value(pair[0])
		y, hasY := sourceVars.Value(pair[1])
		if !hasX && !hasY {
			continue
		}
		if !hasX {
			x = "0"
		}
		if !hasY {
			y = "0"
		}
		xv, xe := strconv.ParseFloat(x, 64)
		yv, ye := strconv.ParseFloat(y, 64)
		if xe != nil || ye != nil || math.IsNaN(xv) || math.IsNaN(yv) || math.IsInf(xv, 0) || math.IsInf(yv, 0) {
			return nil, fmt.Errorf("cannot rotate %s/%s expressions", pair[0], pair[1])
		}
		nx, ny := yv, -xv
		if !clockwise {
			nx, ny = -yv, xv
		}
		if nx == 0 {
			nx = 0
		}
		if ny == 0 {
			ny = 0
		} // Normalize negative zero.
		vars = setOrientation(vars, pair[0], strconv.FormatFloat(nx, 'f', -1, 64))
		vars = setOrientation(vars, pair[1], strconv.FormatFloat(ny, 'f', -1, 64))
	}
	return dmmprefab.New(dmmprefab.IdNone, path, vars), nil
}

func setOrientation(vars *dmvars.Variables, name, value string) *dmvars.Variables {
	if parent := vars.Parent(); parent != nil {
		if inherited, ok := parent.Value(name); ok && inherited == value {
			return dmvars.Delete(vars, name)
		}
	}
	return dmvars.Set(vars, name, value)
}

func parseDirection(raw string) (int, bool) {
	if value, ok := map[string]int{"NORTH": 1, "SOUTH": 2, "EAST": 4, "WEST": 8, "NORTHEAST": 5, "NORTHWEST": 9, "SOUTHEAST": 6, "SOUTHWEST": 10}[strings.TrimSpace(raw)]; ok {
		return value, true
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	return value, err == nil && value >= 0 && value <= 15
}
