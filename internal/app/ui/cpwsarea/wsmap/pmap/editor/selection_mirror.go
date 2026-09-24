// APHELION EDIT ADDITION START - SELECTION MIRROR
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

// MirrorSelection reflects selected visible contents as one undoable operation.
func (e *Editor) MirrorSelection(area util.Bounds, z int, axis editing.MirrorAxis) (util.Bounds, error) {
	if z != e.pMap.ActiveLevel() {
		return area, fmt.Errorf("select an area on the visible level before mirroring")
	}
	if e.collaborationErr != nil {
		return area, e.collaborationErr
	}
	if !e.CanStartMapEdit() {
		return area, fmt.Errorf("finish the current edit before mirroring")
	}
	if selection := editing.RectangleSelection(area, z); selection.Len() > directLocalTiles {
		return e.MirrorSelectionMask(selection, axis)
	}
	plan, err := editing.Mirror(e.dmm, area, z, axis, e.app.PathsFilter().IsVisiblePath)
	if err != nil {
		return area, err
	}
	label := "Mirror Selection Horizontally"
	if axis == editing.MirrorVertical {
		label = "Mirror Selection Vertically"
	}
	return e.commitSelectionTransform(plan, area, label)
}

// APHELION EDIT ADDITION END
