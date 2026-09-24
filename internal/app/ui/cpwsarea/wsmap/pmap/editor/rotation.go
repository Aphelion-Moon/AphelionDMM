// APHELION EDIT ADDITION START - SELECTION ROTATION
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

// RotateSelection adapts a validated transform to the shared operation engine.
func (e *Editor) RotateSelection(area util.Bounds, z int, clockwise bool) (util.Bounds, error) {
	if z != e.pMap.ActiveLevel() {
		return area, fmt.Errorf("select an area on the visible level before rotating")
	}
	if e.collaborationErr != nil {
		return area, e.collaborationErr
	}
	if !e.CanStartMapEdit() {
		return area, fmt.Errorf("finish the current edit before rotating")
	}
	plan, err := editing.Rotate(e.dmm, area, z, clockwise, e.app.PathsFilter().IsVisiblePath)
	if err != nil {
		return area, err
	}
	label := "Rotate Selection Left"
	if clockwise {
		label = "Rotate Selection Right"
	}
	return e.commitSelectionTransform(plan, area, label)
}

// APHELION EDIT ADDITION END
