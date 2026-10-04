// APHELION EDIT ADDITION START - PASTE TRANSFORMS
package editor

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

func (e *Editor) TransformPastePlacement(move *editing.Move, transform editing.PlacementTransform, shift util.Point) (util.Bounds, error) {
	if move == nil || move != e.selectionMove || e.selectionMoveGeneration != e.attachmentGeneration || !move.IsPlacement() {
		return util.Bounds{}, fmt.Errorf("paste belongs to an old map attachment")
	}
	if e.pMap.ActiveLevel() != move.Level() {
		e.FinishSelectionMove(move, true)
		return move.Bounds(), fmt.Errorf("paste cancelled after switching levels")
	}
	if e.collaborationErr != nil {
		return move.Bounds(), e.collaborationErr
	}
	coords, err := move.TransformPlacement(transform, shift, e.RotationLookup())
	if err != nil {
		return move.Bounds(), err
	}
	if len(coords) != 0 {
		e.UpdateCanvasByCoords(coords)
	}
	return move.Bounds(), nil
}

// APHELION EDIT ADDITION END
