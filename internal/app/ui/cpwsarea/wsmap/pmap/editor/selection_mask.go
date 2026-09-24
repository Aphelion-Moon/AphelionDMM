// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
package editor

import (
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

func (e *Editor) RotateSelectionMask(s editing.Selection, clockwise bool) (util.Bounds, error) {
	if !e.CanStartMapEdit() || s.Level() != e.pMap.ActiveLevel() {
		return s.Bounds(), fmt.Errorf("finish the current edit on the visible level")
	}
	reservation, err := e.reserveSelectionPlan(s, s.Rotate(clockwise))
	if err != nil {
		return s.Bounds(), err
	}
	defer reservation.Release()
	plan, err := editing.RotateMask(e.dmm, s, clockwise, e.app.PathsFilter().IsVisiblePath)
	if err != nil {
		return s.Bounds(), err
	}
	label := "Rotate Selection Left"
	if clockwise {
		label = "Rotate Selection Right"
	}
	return e.commitSelectionTransform(plan, s.Bounds(), label)
}
func (e *Editor) MirrorSelectionMask(s editing.Selection, axis editing.MirrorAxis) (util.Bounds, error) {
	if !e.CanStartMapEdit() || s.Level() != e.pMap.ActiveLevel() {
		return s.Bounds(), fmt.Errorf("finish the current edit on the visible level")
	}
	reservation, err := e.reserveSelectionPlan(s, s.Mirror(axis))
	if err != nil {
		return s.Bounds(), err
	}
	defer reservation.Release()
	plan, err := editing.MirrorMask(e.dmm, s, axis, e.app.PathsFilter().IsVisiblePath)
	if err != nil {
		return s.Bounds(), err
	}
	label := "Mirror Selection Horizontally"
	if axis == editing.MirrorVertical {
		label = "Mirror Selection Vertically"
	}
	return e.commitSelectionTransform(plan, s.Bounds(), label)
}

// Count the real changed union before copying its instances. Geometry ownership
// is separate from the substantially larger model/undo/display working set.
func (e *Editor) reserveSelectionPlan(source, destination editing.Selection) (*resources.Reservation, error) {
	reservation, err := e.editWorkBudget().Reserve(1024 + uint64(source.Len()+destination.Len())*96)
	if err != nil {
		return nil, err
	}
	seen := make(map[util.Point]bool, source.Len()+destination.Len())
	bytes := uint64(1024)
	var failure error
	visit := func(p util.Point) {
		if failure != nil || seen[p] {
			return
		}
		seen[p] = true
		state, ok := e.authoritativeTiles[model.Coord{X: p.X, Y: p.Y, Z: p.Z}]
		if !ok {
			failure = fmt.Errorf("selection leaves the map")
			return
		}
		bytes = addWorkBytes(bytes, localTileBytes(state))
	}
	source.Visit(visit)
	destination.Visit(visit)
	if bytes > ^uint64(0)/16 {
		bytes = ^uint64(0)
	} else {
		bytes *= 16
	}
	if failure == nil {
		failure = reservation.Resize(bytes)
	}
	if failure != nil {
		reservation.Release()
		return nil, failure
	}
	return reservation, nil
}

// APHELION EDIT ADDITION END
