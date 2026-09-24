// APHELION EDIT ADDITION START - PASTE PLACEMENT
package editor

import (
	"fmt"

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/util"
)

func (e *Editor) HasPastePlacement() bool {
	return e.paste != nil || e.selectionMove != nil && e.selectionMove.IsPlacement()
}

func (e *Editor) startPastePlacement() {
	if e.HasPastePlacement() {
		return
	}
	data := e.app.Clipboard().Buffer()
	if len(data.Buffer) == 0 {
		return
	}
	if err := e.startPlacement(data); err != nil {
		e.reportCollaborationError("Unable to paste", err)
	}
}

// startPlacement shares preview ownership between clipboard paste and stamps.
func (e *Editor) startPlacement(data dmmclip.PasteData) error {
	if !e.CanStartMapEdit() {
		return fmt.Errorf("finish or cancel the current edit before pasting")
	}
	if len(data.Buffer) == 0 {
		return fmt.Errorf("clipboard is empty")
	}
	filter := data.Filter.Copy()
	// Clipboard writes replace their slice, so this immutable source remains
	// stable without copying a potentially complete level on the UI thread.
	return e.startPlacementPrepared(func(target util.Point) error {
		return e.beginPasteProposal(data.Buffer, filter.IsVisiblePath, target)
	}, nil)
}

func (e *Editor) startPlacementFromFactory(tileCount int, factory pasteSourceFactory, release func()) error {
	return e.startPlacementPrepared(func(target util.Point) error {
		return e.beginPasteProposalFromFactory(tileCount, factory, release, target)
	}, release)
}

func (e *Editor) startPlacementPrepared(begin func(util.Point) error, release func()) error {
	if !e.CanStartMapEdit() {
		if release != nil {
			release()
		}
		return fmt.Errorf("finish or cancel the current edit before pasting")
	}
	coord := e.pMap.CanvasState().LastHoveredTile()
	coord.Z = e.pMap.ActiveLevel()
	if err := begin(coord); err != nil {
		if release != nil {
			release()
		}
		return err
	}
	g := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	if !g.StartPreparedPlacement(e, coord) {
		e.discardPasteWithoutRestore()
		return fmt.Errorf("grab tool does not support prepared paste placement")
	}
	return nil
}

// APHELION EDIT ADDITION END
