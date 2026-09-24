// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
package editor

import (
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func (e *Editor) BeginSelectionMove(area util.Bounds, z int) (*editing.Move, error) {
	return e.BeginSelectionMaskMove(editing.RectangleSelection(area, z))
}

func (e *Editor) BeginSelectionMaskMove(selection editing.Selection) (*editing.Move, error) {
	z := selection.Level()
	if e.localWork != nil || e.executor == nil || e.collaborationErr != nil || e.selectionMove != nil || len(e.pendingChanges) != 0 || z != e.pMap.ActiveLevel() {
		return nil, fmt.Errorf("finish the current edit and select the visible level before moving")
	}
	filter := e.app.PathsFilter().Copy()
	move, err := editing.NewMaskMove(e.dmm, selection, filter.IsVisiblePath,
		func(coord util.Point) error {
			// Move calls capture only for new backgrounds. A journal entry here
			// belongs to a separate edit and cannot be restored/released by this drag.
			if _, exists := e.pendingChanges[model.Coord{X: coord.X, Y: coord.Y, Z: coord.Z}]; exists {
				return fmt.Errorf("finish the existing tile edit before moving over (%d,%d,%d)", coord.X, coord.Y, coord.Z)
			}
			e.BeginTileChange(coord)
			return e.collaborationErr
		},
		func(tile *dmmap.Tile) { tile.InstancesRegenerate() },
		func(coord util.Point) { delete(e.pendingChanges, model.Coord{X: coord.X, Y: coord.Y, Z: coord.Z}) })
	if err != nil {
		return nil, err
	}
	e.selectionMove = move
	e.selectionMoveGeneration = e.attachmentGeneration
	return move, nil
}

func (e *Editor) PreviewSelectionMove(move *editing.Move, shift util.Point) (util.Bounds, error) {
	if move != e.selectionMove || e.selectionMoveGeneration != e.attachmentGeneration {
		return util.Bounds{}, fmt.Errorf("selection move belongs to an old map attachment")
	}
	if e.pMap.ActiveLevel() != move.Level() {
		e.FinishSelectionMove(move, true)
		return move.Bounds(), fmt.Errorf("selection move cancelled after switching levels")
	}
	coords, err := move.Preview(shift)
	if err != nil {
		return move.Bounds(), err
	}
	if len(coords) != 0 {
		e.UpdateCanvasByCoords(coords)
	}
	return move.Bounds(), nil
}

func (e *Editor) FinishSelectionMove(move *editing.Move, cancel bool) {
	if move != e.selectionMove {
		return
	}
	if e.selectionMoveGeneration != e.attachmentGeneration {
		move.Finish(false)
		e.selectionMove = nil
		return
	}
	cancelled := cancel || e.pMap.ActiveLevel() != move.Level() || e.collaborationErr != nil
	coords := move.Finish(cancelled)
	e.selectionMove = nil
	if len(coords) != 0 {
		e.pMap.Canvas().Render().UpdateBucketV(e.dmm, move.Level(), coords)
	}
	if cancelled {
		// Finish restored and released only this move's captures. Do not commit
		// unrelated edits that may have been captured while the preview was open.
		selectionApplied(e.selectionOutcome, false)
		if e.collaborationErr != nil && !cancel {
			e.reportCollaborationError("Unable to move selection", e.collaborationErr)
		}
		return
	}
	if move.IsPlacement() {
		e.CommitOperation("Paste Tiles")
	} else {
		e.CommitOperation("Move Grabbed Area")
	}
}

// APHELION EDIT ADDITION END
