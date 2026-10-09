// Package editing contains map editing plans independent of concrete UI widgets.
package editing

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"

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
	// Areas have no facing, and a plain atom only inherits /atom's default dir.
	// Rotate orientation only when the instance, or a declared directional
	// helper, actually owns it; everything else is returned untouched.
	scope := orientationScopeOf(prefab, lookup)
	if scope.none {
		return prefab, nil
	}
	if raw, ok := vars.Value("dir"); ok && scope.dir {
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
		if rotated != direction {
			path, vars = setDirection(path, vars, rotated, lookup)
		}
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
		x, hasX := orientationOffset(sourceVars, pair[0], scope)
		y, hasY := orientationOffset(sourceVars, pair[1], scope)
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
		vars = setOffset(vars, pair[0], strconv.FormatFloat(nx, 'f', -1, 64))
		vars = setOffset(vars, pair[1], strconv.FormatFloat(ny, 'f', -1, 64))
	}
	if path == prefab.Path() && vars == sourceVars {
		return prefab, nil
	}
	return dmmprefab.New(dmmprefab.IdNone, path, vars), nil
}

// orientationScope says which inherited orientation values belong to a prefab.
type orientationScope struct {
	none   bool // Areas never carry a facing or an offset.
	dir    bool // dir is explicit, declared by a helper, or a type-owned override.
	helper bool // Inherited offsets come from a declared directional helper.
}

func orientationScopeOf(prefab *dmmprefab.Prefab, lookups []PrefabLookup) orientationScope {
	path, vars := prefab.Path(), prefab.Vars()
	if path == "/area" || strings.HasPrefix(path, "/area/") {
		return orientationScope{none: true}
	}
	_, helper := directionalFamily(path, lookups)
	_, explicit := vars.ExplicitValue("dir")
	// A type-declared dir on a single-direction sprite turns nothing visible;
	// writing it would only add an inert edit to the map. Machinery is exempt:
	// its dir often picks a pipe or conveyor side even when the map sprite has
	// one direction (thermomachines set their icon at runtime).
	owned := typeOwnsDir(prefab, lookups) && (DirMayBeFunctional(path) || spriteTurns(vars))
	return orientationScope{dir: explicit || helper || owned, helper: helper}
}

var spriteDirections atomic.Pointer[func(icon, state string) (int, bool)]

// SetSpriteDirections installs the loaded environment's DMI direction source
// (nil clears it). It must be safe to call from any goroutine: paste
// transforms rotate on workers.
func SetSpriteDirections(dirs func(icon, state string) (int, bool)) {
	if dirs == nil {
		spriteDirections.Store(nil)
		return
	}
	spriteDirections.Store(&dirs)
}

// DirMayBeFunctional reports types whose dir can matter in game even when the
// map sprite has one direction, so the sprite is no evidence that dir is inert.
func DirMayBeFunctional(path string) bool {
	for _, root := range []string{"/obj/machinery", "/mob"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// spriteTurns reports whether turning changes the sprite. Without a source, or
// for a sprite it cannot read, it answers true and rotation behaves as before.
func spriteTurns(vars *dmvars.Variables) bool {
	source := spriteDirections.Load()
	if source == nil {
		return true
	}
	dirs, ok := (*source)(vars.TextV("icon", ""), vars.TextV("icon_state", ""))
	return !ok || dirs > 1
}

// typeOwnsDir reports whether a type's own definition, rather than the root
// default every atom inherits, sets dir.
func typeOwnsDir(prefab *dmmprefab.Prefab, lookups []PrefabLookup) bool {
	if len(lookups) != 0 && lookups[0] != nil {
		// Walk the declared type path: /atom owns the universal default.
		for path := prefab.Path(); path != ""; path = path[:max(strings.LastIndexByte(path, '/'), 0)] {
			if vars := lookups[0](path); vars != nil {
				if _, ok := vars.ExplicitValue("dir"); ok {
					return path != "/atom"
				}
			}
		}
		return false
	}
	// Without an environment, the topmost declaration is the root default.
	for chain := prefab.Vars().Parent(); chain != nil; chain = chain.Parent() {
		if _, ok := chain.ExplicitValue("dir"); ok {
			return chain.Parent() != nil
		}
	}
	return false
}

// orientationOffset reads an offset the instance owns. Declared helpers also
// own the offsets they supply, which move with the helper's facing.
func orientationOffset(vars *dmvars.Variables, name string, scope orientationScope) (string, bool) {
	if scope.helper {
		return vars.Value(name)
	}
	return vars.ExplicitValue(name)
}

func setOffset(vars *dmvars.Variables, name, value string) *dmvars.Variables {
	if current, ok := vars.Value(name); ok && current == value {
		return vars
	}
	return setOrientation(vars, name, value)
}

// reparentExplicit keeps only the instance's own overrides on a new helper type.
func reparentExplicit(vars, target *dmvars.Variables) *dmvars.Variables {
	explicit := &dmvars.MutableVariables{}
	for _, name := range vars.Iterate() {
		value, _ := vars.ExplicitValue(name)
		explicit.Put(name, value)
	}
	result := explicit.ToImmutable()
	result.LinkParent(target)
	return result
}

// setDirection stores a turned dir, switching to the matching helper when one
// is declared. An instance that wrote dir keeps it explicit even when it equals
// the inherited default, so it stays recognisably turnable and round-trips.
func setDirection(path string, vars *dmvars.Variables, direction int, lookup []PrefabLookup) (string, *dmvars.Variables) {
	_, explicit := vars.ExplicitValue("dir")
	if targetPath, targetVars := directionalVariant(path, direction, lookup); targetVars != nil {
		return targetPath, setOrientation(reparentExplicit(vars, targetVars), "dir", strconv.Itoa(direction))
	}
	if explicit {
		return path, dmvars.Set(vars, "dir", strconv.Itoa(direction))
	}
	return path, setOrientation(vars, "dir", strconv.Itoa(direction))
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
