package editing

import (
	"fmt"
	"math"
	"sort"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// Move owns one drag preview. Background copies cannot outlive that gesture.
// The editor captures before-states before the first preview touches each tile.
type Move struct {
	m              *dmmap.Dmm
	origin, bounds util.Bounds
	z              int
	source         []dmmap.Tile
	background     map[util.Point]dmmap.Tile
	visible        func(string) bool
	capture        func(util.Point) error
	regenerate     func(*dmmap.Tile)
	release        func(util.Point)
	closed         bool
	placement      bool
	sourceCoords   map[util.Point]struct{}
	lastShift      util.Point
	previewed      bool
}

// release, when non-nil, drops a captured before-state once the move no longer
// owns it: after failed preflight, restoration of a passed-over tile, or cancel.
func NewMove(m *dmmap.Dmm, area util.Bounds, z int, visible func(string) bool, capture func(util.Point) error, regenerate func(*dmmap.Tile), release func(util.Point)) (*Move, error) {
	return NewMaskMove(m, RectangleSelection(area, z), visible, capture, regenerate, release)
}

func NewMaskMove(m *dmmap.Dmm, selection Selection, visible func(string) bool, capture func(util.Point) error, regenerate func(*dmmap.Tile), release func(util.Point)) (*Move, error) {
	area, z := selection.Bounds(), selection.Level()
	for _, value := range []float32{area.X1, area.Y1, area.X2, area.Y2} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || float64(value) != math.Trunc(float64(value)) {
			return nil, fmt.Errorf("selection must contain whole tiles")
		}
	}
	if m == nil || visible == nil || capture == nil || area.X1 > area.X2 || area.Y1 > area.Y2 ||
		!m.HasTile(util.Point{X: int(area.X1), Y: int(area.Y1), Z: z}) || !m.HasTile(util.Point{X: int(area.X2), Y: int(area.Y2), Z: z}) {
		return nil, fmt.Errorf("selection is outside the map")
	}
	move := &Move{m: m, origin: area, bounds: area, z: z, background: make(map[util.Point]dmmap.Tile), visible: visible, capture: capture, regenerate: regenerate, release: release}
	move.sourceCoords = make(map[util.Point]struct{}, selection.Len())
	for _, coord := range selection.Coordinates() {
		if err := capture(coord); err != nil {
			for captured := range move.background {
				move.releaseRestored(captured)
			}
			return nil, err
		}
		tile := m.GetTile(coord).Copy()
		move.source = append(move.source, tile)
		move.background[coord] = tile
		move.sourceCoords[coord] = struct{}{}
	}
	return move, nil
}

func (move *Move) Bounds() util.Bounds { return move.bounds }
func (move *Move) Level() int          { return move.z }
func (move *Move) IsPlacement() bool   { return move.placement }
func (move *Move) Closed() bool        { return move.closed }

func (move *Move) Preview(shift util.Point) ([]util.Point, error) {
	if move.closed {
		return nil, fmt.Errorf("selection move has ended")
	}
	next := move.origin.Plus(float32(shift.X), float32(shift.Y))
	if shift.Z != 0 || next.X1 < 1 || next.Y1 < 1 || next.X2 > float32(move.m.MaxX) || next.Y2 > float32(move.m.MaxY) {
		return nil, fmt.Errorf("moved selection would leave the map or selected level")
	}
	if move.placement && move.previewed && shift == move.lastShift {
		return nil, nil
	}
	// Snapshot new destinations before changing the previous preview. Existing
	// background entries already hold their original, unmodified contents.
	var acquired []util.Point
	for _, source := range move.source {
		coord := source.Coord.Plus(shift)
		if _, exists := move.background[coord]; exists {
			continue
		}
		if err := move.capture(coord); err != nil {
			for _, captured := range acquired {
				move.releaseRestored(captured)
			}
			return nil, err
		}
		move.background[coord] = move.m.GetTile(coord).Copy()
		acquired = append(acquired, coord)
	}
	if !move.placement && shift == (util.Point{}) {
		coords := move.restore()
		for coord := range move.background {
			if _, source := move.sourceCoords[coord]; !source {
				move.releaseRestored(coord)
			}
		}
		move.bounds = move.origin
		return coords, nil
	}
	// Still-owned tiles are rebuilt from their immutable backgrounds below. Do
	// not deep-copy their visible contents just to discard those copies. Restore
	// passed-over tiles in full; every visited coordinate still needs invalidation.
	coords := make([]util.Point, 0, len(move.background))
	for coord, before := range move.background {
		coords = append(coords, coord)
		if !move.ownsDestination(coord, shift, next) {
			move.m.GetTile(coord).Set(before.Instances().DeepCopy())
			continue
		}
		var hidden dmmap.Instances
		for _, instance := range before.Instances() {
			if !move.visible(instance.Prefab().Path()) {
				copy := instance.Copy()
				hidden = append(hidden, &copy)
			}
		}
		move.m.GetTile(coord).Set(hidden)
	}
	sortMoveCoords(coords)
	for _, source := range move.source {
		coord := source.Coord.Plus(shift)
		tile := move.m.GetTile(coord)
		for _, instance := range source.Instances() {
			if !move.visible(instance.Prefab().Path()) {
				continue
			}
			moved := dmminstance.New(coord, instance.Prefab())
			moved.SetStableID(instance.StableID())
			tile.Set(append(tile.Instances(), moved))
		}
	}
	for coord := range move.background {
		if !move.ownsDestination(coord, shift, next) {
			move.releaseRestored(coord)
		} else if move.regenerate != nil {
			move.regenerate(move.m.GetTile(coord))
		}
	}
	move.bounds = next
	move.lastShift, move.previewed = shift, true
	return coords, nil
}

func (move *Move) ownsDestination(coord, shift util.Point, next util.Bounds) bool {
	if move.placement {
		_, exists := move.sourceCoords[coord.Minus(shift)]
		return exists
	}
	_, source := move.sourceCoords[coord]
	_, destination := move.sourceCoords[coord.Minus(shift)]
	return source || destination
}

func (move *Move) releaseRestored(coord util.Point) {
	delete(move.background, coord)
	if move.release != nil {
		move.release(coord)
	}
}

func (move *Move) restore() []util.Point {
	coords := make([]util.Point, 0, len(move.background))
	for coord, before := range move.background {
		move.m.GetTile(coord).Set(before.Instances().DeepCopy())
		coords = append(coords, coord)
	}
	sortMoveCoords(coords)
	return coords
}

func sortMoveCoords(coords []util.Point) {
	sort.Slice(coords, func(i, j int) bool {
		if coords[i].Y != coords[j].Y {
			return coords[i].Y < coords[j].Y
		}
		return coords[i].X < coords[j].X
	})
}

// Finish returns restored coordinates on cancellation. Successful release keeps
// the current preview for the editor to submit as one operation.
func (move *Move) Finish(cancel bool) []util.Point {
	if move.closed {
		return nil
	}
	var coords []util.Point
	if cancel {
		coords = move.restore()
		move.bounds = move.origin
		for coord := range move.background {
			move.releaseRestored(coord)
		}
	}
	move.closed = true
	move.source = nil
	move.sourceCoords = nil
	move.background = nil
	return coords
}
