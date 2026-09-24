package editing

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// PlacementPayload is immutable after preparation. Coordinates are source-local
// and sparse. A translation changes an anchor, never this payload.
type PlacementPayload struct {
	Tiles         []dmmap.Tile
	Intents       map[util.Point]TileIntent
	Width, Height int
}

// CompilePlacementPayload adapts existing clipboard/stamp coordinates without
// inventing cells in holes. IDs are owned by the live placement before this call.
func CompilePlacementPayload(ctx context.Context, source []dmmap.Tile, visible func(string) bool) (*PlacementPayload, error) {
	if len(source) == 0 || visible == nil {
		return nil, fmt.Errorf("paste requires a nonempty source")
	}
	minX, minY, maxX, maxY := source[0].Coord.X, source[0].Coord.Y, source[0].Coord.X, source[0].Coord.Y
	level := source[0].Coord.Z
	for _, tile := range source {
		minX, minY = min(minX, tile.Coord.X), min(minY, tile.Coord.Y)
		maxX, maxY = max(maxX, tile.Coord.X), max(maxY, tile.Coord.Y)
	}
	p := &PlacementPayload{Width: maxX - minX + 1, Height: maxY - minY + 1, Intents: make(map[util.Point]TileIntent, len(source)), Tiles: make([]dmmap.Tile, 0, len(source))}
	for _, tile := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if tile.Coord.Z != level || tile.Coord.X < 1 || tile.Coord.Y < 1 {
			return nil, fmt.Errorf("paste source must have positive coordinates on one level")
		}
		coord := util.Point{X: tile.Coord.X - minX, Y: tile.Coord.Y - minY}
		if _, ok := p.Intents[coord]; ok {
			return nil, fmt.Errorf("duplicate source coordinate")
		}
		var intent TileIntent
		for c, path := range []string{"/area", "/turf", "/obj", "/mob"} {
			if visible(path) {
				intent[c].Action = Clear
			}
		}
		local := dmmap.Tile{Coord: util.Point{X: coord.X + 1, Y: coord.Y + 1, Z: 1}}
		for _, instance := range tile.Instances() {
			if instance == nil || instance.Prefab() == nil || instance.Prefab().Vars() == nil {
				return nil, fmt.Errorf("invalid paste instance")
			}
			prefab := instance.Prefab()
			if !visible(prefab.Path()) {
				continue
			}
			state := model.PrefabState{StableID: model.StableID(instance.StableID()), Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
			for _, name := range prefab.Vars().Iterate() {
				value, ok := prefab.Vars().Value(name)
				if !ok {
					return nil, fmt.Errorf("missing source variable %q", name)
				}
				state.Vars[name] = value
			}
			channel := ChannelForPath(state.Path)
			intent[channel].Action = Set
			intent[channel].Data = append(intent[channel].Data, state)
			local.Set(append(local.Instances(), instance))
		}
		p.Intents[coord] = intent
		p.Tiles = append(p.Tiles, local)
	}
	return p, nil
}

func (p *PlacementPayload) Bounds(target util.Point) util.Bounds {
	return util.Bounds{X1: float32(target.X), Y1: float32(target.Y), X2: float32(target.X + p.Width - 1), Y2: float32(target.Y + p.Height - 1)}
}

func (p *PlacementPayload) ValidateTarget(target util.Point, maxX, maxY, level int) error {
	if target.Z != level || target.X < 1 || target.Y < 1 || target.X+p.Width-1 > maxX || target.Y+p.Height-1 > maxY {
		return fmt.Errorf("paste selection would leave the map or selected level")
	}
	return nil
}

// BuildPlacementChanges touches only the selected footprint. The lookup must
// refer to one owned revision; the executor revalidates all before-values.
func (p *PlacementPayload) BuildPlacementChanges(ctx context.Context, target util.Point, policy PastePolicy, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool)) ([]model.TileChange, error) {
	changes := make([]model.TileChange, 0, min(1024, len(p.Tiles)))
	for _, tile := range p.Tiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		local := util.Point{X: tile.Coord.X - 1, Y: tile.Coord.Y - 1}
		writes := false
		for c, intent := range p.Intents[local] {
			writes = writes || policy.Writes(Channel(c), intent)
		}
		if !writes {
			continue
		}
		coord := model.Coord{X: target.X + local.X, Y: target.Y + local.Y, Z: target.Z}
		before, ok := lookup(coord)
		if !ok {
			return nil, fmt.Errorf("paste destination %v is unavailable", coord)
		}
		after, err := ComposeTile(before, p.Intents[local], policy, visible)
		if err != nil {
			return nil, fmt.Errorf("at %v: %w", coord, err)
		}
		if !before.Equal(after) {
			changes = append(changes, model.TileChange{Coord: coord, Before: model.CloneTileState(before), After: after})
		}
	}
	return changes, nil
}

// Orientation is one of the eight symmetries of a rectangle. Its representation
// is a signed orthogonal matrix; keypress history never accumulates.
type Orientation struct{ A, B, C, D int8 }

func IdentityOrientation() Orientation { return Orientation{A: 1, D: 1} }

func (o Orientation) Transform(t PlacementTransform) Orientation {
	m := IdentityOrientation()
	switch t {
	case PlacementRotateRight:
		m = Orientation{B: 1, C: -1}
	case PlacementRotateLeft:
		m = Orientation{B: -1, C: 1}
	case PlacementMirrorHorizontal:
		m.A = -1
	case PlacementMirrorVertical:
		m.D = -1
	}
	return Orientation{A: m.A*o.A + m.B*o.C, B: m.A*o.B + m.B*o.D, C: m.C*o.A + m.D*o.C, D: m.C*o.B + m.D*o.D}
}

func (o Orientation) Prepare(ctx context.Context, source []dmmap.Tile) ([]dmmap.Tile, error) {
	// Choose a canonical bounded transform path from the original, so returning
	// to identity also restores inherited values rather than stacking overrides.
	for mirror := 0; mirror < 2; mirror++ {
		current := IdentityOrientation()
		var path []PlacementTransform
		if mirror != 0 {
			current = current.Transform(PlacementMirrorHorizontal)
			path = append(path, PlacementMirrorHorizontal)
		}
		for turns := 0; turns < 4; turns++ {
			if current == o {
				result := source
				for _, t := range path {
					var err error
					result, err = TransformPlacementTemplate(ctx, result, t)
					if err != nil {
						return nil, err
					}
				}
				return result, nil
			}
			current = current.Transform(PlacementRotateRight)
			path = append(path, PlacementRotateRight)
		}
	}
	return nil, fmt.Errorf("invalid placement orientation")
}
