package editing

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type movePayloadDestination struct {
	local util.Point
	state model.TileState
}

func rotatedMoveBounds(area util.Bounds, turns uint8) util.Bounds {
	if turns&1 != 0 {
		area.X2, area.Y2 = area.X1+area.Y2-area.Y1, area.Y1+area.X2-area.X1
	}
	return area
}

func (move *SelectionMove) Turns() uint8 { return move.turns }
func (move *SelectionMove) DestinationSelection() Selection {
	return move.destination.Translate(move.shift)
}

// Rotate changes only the preview pose. Source membership remains immutable for
// validation and history, including when the rotated mask overlaps its source.
func (move *SelectionMove) Rotate(clockwise bool, maxX, maxY, level int) (util.Bounds, error) {
	if move.closed {
		return move.bounds, fmt.Errorf("selection move has ended")
	}
	turns := (move.turns + 1) % 4
	if !clockwise {
		turns = (move.turns + 3) % 4
	}
	next := rotatedMoveBounds(move.origin, turns).Plus(float32(move.shift.X), float32(move.shift.Y))
	if level != move.level || next.X1 < 1 || next.Y1 < 1 || next.X2 > float32(maxX) || next.Y2 > float32(maxY) {
		return move.bounds, fmt.Errorf("rotated selection would leave the map; move it inward first")
	}
	destination := move.selection
	if turns != 0 {
		destination = move.destination.Rotate(clockwise)
	}
	move.turns, move.destination, move.bounds = turns, destination, next
	return next, nil
}

// SourceTile returns immutable source geometry and visible values independently
// of any destination rotation. Source defaults and validation must use these.
func (p *MovePayload) SourceTile(index int) (util.Point, []model.PrefabState) {
	tile := p.tiles[index]
	return tile.local, tile.state.Prefabs
}

func (p *MovePayload) DestinationContains(coord, shift util.Point) bool {
	if coord.Z != p.level || shift.Z != 0 {
		return false
	}
	x, y := coord.X-shift.X-int(p.origin.X1), coord.Y-shift.Y-int(p.origin.Y1)
	w, h := int(p.origin.X2-p.origin.X1+1), int(p.origin.Y2-p.origin.Y1+1)
	switch p.turns {
	case 1:
		x, y = w-1-y, x
	case 2:
		x, y = w-1-x, h-1-y
	case 3:
		x, y = y, h-1-x
	}
	return p.selection.Contains(util.Point{X: int(p.origin.X1) + x, Y: int(p.origin.Y1) + y, Z: p.level})
}

// Rotated derives an immutable destination from the original source, never from
// a preceding orientation. Returning to zero turns restores the exact source.
func (p *MovePayload) Rotated(ctx context.Context, turns uint8, parent PrefabLookup) (*MovePayload, error) {
	if p.base != nil {
		p = p.base
	}
	turns %= 4
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if turns == 0 {
		return p, nil
	}
	out := *p
	out.base, out.turns = p, turns
	out.destination = make([]movePayloadDestination, len(p.tiles))
	cache := make(map[string]*dmmprefab.Prefab)
	width, height := int(p.origin.X2-p.origin.X1+1), int(p.origin.Y2-p.origin.Y1+1)
	for index, tile := range p.tiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		x, y, w, h := tile.local.X-1, tile.local.Y-1, width, height
		for range turns {
			x, y, w, h = y, w-1-x, h, w
		}
		destination := movePayloadDestination{local: util.Point{X: x + 1, Y: y + 1, Z: 1},
			state: model.TileState{Prefabs: make([]model.PrefabState, 0, len(tile.state.Prefabs))}}
		for _, state := range tile.state.Prefabs {
			variables := &dmvars.MutableVariables{}
			for name, value := range state.Vars {
				variables.Put(name, value)
			}
			vars := variables.ToImmutable()
			if parent != nil {
				vars.LinkParent(parent(state.Path))
			}
			prefab := dmmprefab.New(dmmprefab.IdNone, state.Path, vars)
			key := prefab.ContentKey()
			result, found := cache[key]
			if !found {
				result = prefab
				for range turns {
					var err error
					result, err = rotatePrefab(result, true, parent)
					if err != nil {
						return nil, fmt.Errorf("%s: %w", state.Path, err)
					}
				}
				cache[key] = result
			}
			state.Path = result.Path()
			state.Vars = make(map[string]string, result.Vars().Len())
			for _, name := range result.Vars().Iterate() {
				state.Vars[name], _ = result.Vars().ExplicitValue(name)
			}
			destination.state.Prefabs = append(destination.state.Prefabs, state)
		}
		out.destination[index] = destination
	}
	sort.Slice(out.destination, func(i, j int) bool {
		a, b := out.destination[i].local, out.destination[j].local
		return a.Y < b.Y || (a.Y == b.Y && a.X < b.X)
	})
	return &out, nil
}
