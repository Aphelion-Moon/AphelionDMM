package editing

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
)

// MovePayload is an immutable, sparse snapshot of the visible contents selected
// for a move. It preserves their collaboration identities; unlike clipboard
// placement, a move does not create copies.
type MovePayload struct {
	selection Selection
	origin    util.Bounds
	level     int
	tiles     []movePayloadTile
}

// SelectionMove owns only selection geometry and a cheap pose. It never owns or
// mutates live map tiles; an editor separately supplies an immutable payload.
type SelectionMove struct {
	selection Selection
	origin    util.Bounds
	bounds    util.Bounds
	level     int
	shift     util.Point
	closed    bool
}

func NewSelectionMove(selection Selection) (*SelectionMove, error) {
	area := selection.Bounds()
	if selection.Len() == 0 || selection.Level() < 1 || !wholeTileBounds(area) {
		return nil, fmt.Errorf("move requires a nonempty whole-tile selection")
	}
	return &SelectionMove{selection: selection, origin: area, bounds: area, level: selection.Level()}, nil
}

func (move *SelectionMove) Update(shift util.Point, maxX, maxY, level int) (util.Bounds, bool, error) {
	if move.closed {
		return move.bounds, false, fmt.Errorf("selection move has ended")
	}
	next := move.origin.Plus(float32(shift.X), float32(shift.Y))
	if shift.Z != 0 || level != move.level || next.X1 < 1 || next.Y1 < 1 || next.X2 > float32(maxX) || next.Y2 > float32(maxY) {
		return move.bounds, false, fmt.Errorf("moved selection would leave the map or selected level")
	}
	if shift == move.shift {
		return move.bounds, false, nil
	}
	move.shift, move.bounds = shift, next
	return move.bounds, true, nil
}

func (move *SelectionMove) Bounds() util.Bounds  { return move.bounds }
func (move *SelectionMove) Level() int           { return move.level }
func (move *SelectionMove) Shift() util.Point    { return move.shift }
func (move *SelectionMove) Closed() bool         { return move.closed }
func (move *SelectionMove) Selection() Selection { return move.selection }
func (move *SelectionMove) Finish()              { move.closed = true }

type movePayloadTile struct {
	source   model.Coord
	local    util.Point
	original model.TileState
	state    model.TileState
}

// CompileMovePayload copies only selected tiles from one committed authority
// view. The lookup must remain read-only for this call's duration.
func CompileMovePayload(ctx context.Context, selection Selection, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool)) (*MovePayload, error) {
	area, level := selection.Bounds(), selection.Level()
	if selection.Len() == 0 || visible == nil || lookup == nil || level < 1 || !wholeTileBounds(area) {
		return nil, fmt.Errorf("move requires a nonempty whole-tile selection")
	}
	payload := &MovePayload{selection: selection, origin: area, level: level, tiles: make([]movePayloadTile, 0, min(selection.Len(), 1024))}
	seenIDs := make(map[model.StableID]struct{})
	var visitErr error
	visited := 0
	selection.Visit(func(point util.Point) {
		if visitErr != nil {
			return
		}
		if visited&255 == 0 {
			if err := ctx.Err(); err != nil {
				visitErr = err
				return
			}
		}
		visited++
		if point.Z != level || point.X < 1 || point.Y < 1 {
			visitErr = fmt.Errorf("move selection contains an invalid coordinate")
			return
		}
		coord := model.Coord{X: point.X, Y: point.Y, Z: point.Z}
		before, ok := lookup(coord)
		if !ok {
			visitErr = fmt.Errorf("move source (%d,%d,%d) is unavailable", point.X, point.Y, point.Z)
			return
		}
		state, err := visiblePrefabs(before, visible)
		if err != nil {
			visitErr = fmt.Errorf("move source (%d,%d,%d): %w", point.X, point.Y, point.Z, err)
			return
		}
		for _, prefab := range state.Prefabs {
			if err := prefab.StableID.Validate(); err != nil {
				visitErr = fmt.Errorf("move source (%d,%d,%d): %w", point.X, point.Y, point.Z, err)
				return
			}
			if _, duplicate := seenIDs[prefab.StableID]; duplicate {
				visitErr = fmt.Errorf("move source contains duplicate instance identity %q", prefab.StableID)
				return
			}
			seenIDs[prefab.StableID] = struct{}{}
		}
		payload.tiles = append(payload.tiles, movePayloadTile{
			source:   coord,
			local:    util.Point{X: point.X - int(area.X1) + 1, Y: point.Y - int(area.Y1) + 1, Z: 1},
			original: model.CloneTileState(before),
			state:    state,
		})
	})
	if visitErr != nil {
		return nil, visitErr
	}
	sort.Slice(payload.tiles, func(i, j int) bool {
		a, b := payload.tiles[i].local, payload.tiles[j].local
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return payload, nil
}

func wholeTileBounds(area util.Bounds) bool {
	for _, value := range []float32{area.X1, area.Y1, area.X2, area.Y2} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || float64(value) != math.Trunc(float64(value)) {
			return false
		}
	}
	return area.X1 >= 1 && area.Y1 >= 1 && area.X1 <= area.X2 && area.Y1 <= area.Y2
}

func visiblePrefabs(state model.TileState, visible func(string) bool) (model.TileState, error) {
	filtered := model.TileState{Prefabs: make([]model.PrefabState, 0, len(state.Prefabs))}
	for _, prefab := range state.Prefabs {
		if prefab.Path == "" {
			return model.TileState{}, fmt.Errorf("instance has an empty path")
		}
		if visible(prefab.Path) {
			filtered.Prefabs = append(filtered.Prefabs, model.CloneTileState(model.TileState{Prefabs: []model.PrefabState{prefab}}).Prefabs[0])
		}
	}
	return filtered, nil
}

func (p *MovePayload) TileCount() int { return len(p.tiles) }

// Tile returns the source-relative coordinate and visible contents of one
// immutable payload tile. Callers must treat returned variable maps as read-only.
func (p *MovePayload) Tile(index int) (util.Point, []model.PrefabState) {
	tile := p.tiles[index]
	return tile.local, tile.state.Prefabs
}

// OriginalSource returns the complete immutable source state captured when the
// move began, including hidden items that are retained rather than moved.
func (p *MovePayload) OriginalSource(coord model.Coord) (model.TileState, bool) {
	index := sort.Search(len(p.tiles), func(index int) bool {
		a := p.tiles[index].source
		return a.Z > coord.Z || (a.Z == coord.Z && (a.Y > coord.Y || (a.Y == coord.Y && a.X >= coord.X)))
	})
	if index >= len(p.tiles) || p.tiles[index].source != coord {
		return model.TileState{}, false
	}
	return p.tiles[index].original, true
}

func (p *MovePayload) Bounds(shift util.Point) util.Bounds {
	return p.origin.Plus(float32(shift.X), float32(shift.Y))
}

func (p *MovePayload) ValidateTarget(shift util.Point, maxX, maxY, level int) error {
	next := p.Bounds(shift)
	if shift.Z != 0 || level != p.level || next.X1 < 1 || next.Y1 < 1 || next.X2 > float32(maxX) || next.Y2 > float32(maxY) {
		return fmt.Errorf("moved selection would leave the map or selected level")
	}
	return nil
}

// Suppresses reports membership in the exact source/destination union without
// materializing that union during pointer motion.
func (p *MovePayload) Suppresses(coord, shift util.Point) bool {
	return p.selection.Contains(coord) || p.selection.Contains(coord.Minus(shift))
}

// ValidateSource checks only the selected visible payload. Hidden source values
// are retained from the current committed state and may change during a drag.
func (p *MovePayload) ValidateSource(visible func(string) bool, lookup func(model.Coord) (model.TileState, bool)) error {
	return p.validateSourceContext(context.Background(), visible, lookup)
}

// BuildMoveChanges composes the exact sparse source/destination union against
// one current authority view. Visible destination contents are replaced, hidden
// contents are retained, and moved source identities are preserved.
func (p *MovePayload) BuildMoveChanges(ctx context.Context, shift util.Point, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool)) ([]model.TileChange, error) {
	return p.buildMoveChanges(ctx, shift, visible, lookup, nil)
}

// MoveDefaults supplies the current map's base area and turf used to refill
// vacated tiles after visible defaults move with their source content.
type MoveDefaults struct {
	Area model.PrefabState
	Turf model.PrefabState
}

func (p *MovePayload) BuildMoveChangesWithDefaults(ctx context.Context, shift util.Point, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool), defaults MoveDefaults) ([]model.TileChange, error) {
	return p.buildMoveChanges(ctx, shift, visible, lookup, &defaults)
}

func (p *MovePayload) buildMoveChanges(ctx context.Context, shift util.Point, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool), defaults *MoveDefaults) ([]model.TileChange, error) {
	if shift.Z != 0 || visible == nil || lookup == nil {
		return nil, fmt.Errorf("move composition is unavailable")
	}
	if err := p.validateSourceContext(ctx, visible, lookup); err != nil {
		return nil, err
	}
	union := make(map[model.Coord]struct{}, len(p.tiles)*2)
	incoming := make(map[model.Coord][]model.PrefabState, len(p.tiles))
	for index, tile := range p.tiles {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		source := tile.source
		destination := model.Coord{X: source.X + shift.X, Y: source.Y + shift.Y, Z: source.Z}
		union[source] = struct{}{}
		union[destination] = struct{}{}
		if len(tile.state.Prefabs) != 0 {
			incoming[destination] = tile.state.Prefabs
		}
	}
	coords := make([]model.Coord, 0, len(union))
	for coord := range union {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		a, b := coords[i], coords[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	changes := make([]model.TileChange, 0, len(coords))
	for index, coord := range coords {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		before, ok := lookup(coord)
		if !ok {
			return nil, fmt.Errorf("move destination (%d,%d,%d) is unavailable", coord.X, coord.Y, coord.Z)
		}
		after := model.TileState{Prefabs: make([]model.PrefabState, 0, len(before.Prefabs)+len(incoming[coord]))}
		for _, prefab := range before.Prefabs {
			if !visible(prefab.Path) {
				after.Prefabs = append(after.Prefabs, model.CloneTileState(model.TileState{Prefabs: []model.PrefabState{prefab}}).Prefabs[0])
			}
		}
		for _, prefab := range incoming[coord] {
			after.Prefabs = append(after.Prefabs, model.CloneTileState(model.TileState{Prefabs: []model.PrefabState{prefab}}).Prefabs[0])
		}
		if defaults != nil {
			if err := ensureMoveDefault(&after, "/area", defaults.Area); err != nil {
				return nil, fmt.Errorf("regenerate base area at (%d,%d,%d): %w", coord.X, coord.Y, coord.Z, err)
			}
			if err := ensureMoveDefault(&after, "/turf", defaults.Turf); err != nil {
				return nil, fmt.Errorf("regenerate base turf at (%d,%d,%d): %w", coord.X, coord.Y, coord.Z, err)
			}
		}
		if !before.Equal(after) {
			changes = append(changes, model.TileChange{Coord: coord, Before: model.CloneTileState(before), After: after})
		}
	}
	return changes, nil
}

func ensureMoveDefault(state *model.TileState, path string, prototype model.PrefabState) error {
	if path == "" || prototype.Path == "" {
		return fmt.Errorf("base prefab path is empty")
	}
	for _, prefab := range state.Prefabs {
		if strings.HasPrefix(prefab.Path, path) {
			return nil
		}
	}
	stableID, err := model.NewStableID()
	if err != nil {
		return err
	}
	prefab := model.CloneTileState(model.TileState{Prefabs: []model.PrefabState{prototype}}).Prefabs[0]
	prefab.StableID = stableID
	state.Prefabs = append(state.Prefabs, prefab)
	return nil
}

func (p *MovePayload) validateSourceContext(ctx context.Context, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool)) error {
	if visible == nil || lookup == nil {
		return fmt.Errorf("move source validation is unavailable")
	}
	for index, tile := range p.tiles {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		current, ok := lookup(tile.source)
		if !ok {
			return fmt.Errorf("move source (%d,%d,%d) is unavailable", tile.source.X, tile.source.Y, tile.source.Z)
		}
		filtered, err := visiblePrefabs(current, visible)
		if err != nil {
			return err
		}
		if !filtered.Equal(tile.state) {
			return fmt.Errorf("move source changed at (%d,%d,%d)", tile.source.X, tile.source.Y, tile.source.Z)
		}
	}
	return nil
}
