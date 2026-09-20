// APHELION EDIT ADDITION START - SELECTION NUDGE
package tools

import (
	"fmt"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

// Nudge moves a finished selection a whole number of tiles through the preview and
// operation path as a mouse drag. Pixel/step offsets and directions are unchanged.
func (t *ToolGrab) Nudge(shift util.Point) error {
	if !t.HasSelectedArea() || !t.Stale() {
		return fmt.Errorf("finish selecting or moving the area before nudging")
	}
	if shift.Z != 0 || (shift.X == 0) == (shift.Y == 0) ||
		shift.X < -editing.MaxSelectionMoveStep || shift.X > editing.MaxSelectionMoveStep ||
		shift.Y < -editing.MaxSelectionMoveStep || shift.Y > editing.MaxSelectionMoveStep {
		return fmt.Errorf("nudge must move 1 through %d tiles along one axis", editing.MaxSelectionMoveStep)
	}
	return t.trackSelectionTransform(t.fillArea, true, func() (util.Bounds, error) {
		move, err := ed.BeginSelectionMove(t.fillArea, t.fillStart.Z)
		if err != nil {
			return t.fillArea, err
		}
		if _, err := ed.PreviewSelectionMove(move, shift); err != nil {
			ed.FinishSelectionMove(move, true)
			return move.Bounds(), err
		}
		ed.FinishSelectionMove(move, false)
		return move.Bounds(), nil
	})
}

// APHELION EDIT ADDITION END
