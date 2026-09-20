// APHELION EDIT ADDITION START - SELECTION ROTATION
package pmap

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func (p *PaneMap) canRotateSelection() bool {
	grab, ok := tools.Selected().(*tools.ToolGrab)
	return (activePane == p || (activePane == nil && lastActivePane == p)) && ok && grab.HasSelectedArea() && grab.Stale() && grab.SelectionLevel() == p.activeLevel
}

func (p *PaneMap) rotateSelection(clockwise bool) {
	if !p.canTransformSelection() {
		return
	}
	g := tools.Selected().(*tools.ToolGrab)
	transform := editing.PlacementRotateLeft
	if clockwise {
		transform = editing.PlacementRotateRight
	}
	action := editing.RepeatTransform{Orientation: transform}
	if g.Placing() {
		_ = p.editor.TrackRepeatTransform(action, true, func() error { return g.TransformPlacement(transform) }) // Errors remain visible in placement controls.
		return
	}
	if err := p.editor.TrackRepeatTransform(action, false, func() error { return g.Rotate(clockwise, p.editor.RotateSelection) }); err != nil {
		util.ShowErrorDialog("Unable to rotate selection: " + err.Error())
	}
}

// APHELION EDIT ADDITION END
