// APHELION EDIT ADDITION START - PASTE PLACEMENT
package tools

import (
	"fmt"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

type grabPlacement struct {
	owner      editor
	move       *editing.Move
	controller pastePlacementController
	last       util.Point
	err        error
	valid      bool
}

type pastePlacementController interface {
	UpdatePastePlacement(util.Point) (util.Bounds, bool, error)
	ConfirmPastePlacement() bool
	CancelPastePlacement()
	PastePlacementClosed() bool
}

func (t *ToolGrab) Placing() bool { return t.placement != nil }
func (t *ToolGrab) PlacementError() error {
	if t.placement == nil {
		return nil
	}
	return t.placement.err
}

func (t *ToolGrab) StartPlacement(owner editor, move *editing.Move, coord util.Point) {
	t.Reset()
	t.placement = &grabPlacement{owner: owner, move: move}
	t.UpdatePlacement(coord)
}

func (t *ToolGrab) StartPreparedPlacement(owner editor, coord util.Point) bool {
	controller, ok := owner.(pastePlacementController)
	if !ok {
		return false
	}
	t.Reset()
	t.placement = &grabPlacement{owner: owner, controller: controller}
	t.UpdatePlacement(coord)
	return true
}

func (t *ToolGrab) UpdatePlacement(coord util.Point) {
	p := t.placement
	if p == nil {
		return
	}
	if p.controller != nil {
		if p.controller.PastePlacementClosed() || ed != p.owner {
			t.Reset()
			return
		}
		p.last = coord
		area, ready, err := p.controller.UpdatePastePlacement(coord)
		p.err, p.valid = err, ready
		if ready {
			t.fillStart = util.Point{X: int(area.X1), Y: int(area.Y1), Z: coord.Z}
			t.fillArea, t.fillAreaInit = area, area
		}
		return
	}
	if p.move.Closed() {
		t.Reset()
		return
	}
	if p.last == coord && p.controller == nil {
		return
	}
	if p.controller == nil {
		p.last, p.valid = coord, false
	}
	if coord.Z != p.move.Level() {
		p.err = fmt.Errorf("paste target must be on the selected level")
		return
	}
	area, err := p.owner.PreviewSelectionMove(p.move, util.Point{X: coord.X - 1, Y: coord.Y - 1})
	p.err = err
	if err != nil {
		return
	}
	p.valid = true
	t.fillStart = util.Point{X: int(area.X1), Y: int(area.Y1), Z: p.move.Level()}
	t.fillArea, t.fillAreaInit = area, area
}

func (t *ToolGrab) ConfirmPlacement() bool {
	p := t.placement
	if p == nil || !p.valid || ed != p.owner || p.move != nil && p.move.Closed() || p.controller != nil && p.controller.PastePlacementClosed() {
		return false
	}
	// The observer is bound to the originating editor. A later selection has a
	// different history token and cannot be cleared by this operation's outcome.
	t.placement = nil
	t.mode = tSelectModeMoveArea
	if p.move != nil {
		t.stopMoveArea()
	}
	history := editing.NewSelectionHistory(t.fillArea)
	t.selectionHistory = history
	changed := func(applied bool) {
		if !applied && ed == p.owner && t.selectionHistory == history && !t.Placing() && !t.dragging {
			t.Reset()
		}
	}
	action := func() error {
		if p.controller != nil {
			if !p.controller.ConfirmPastePlacement() {
				return fmt.Errorf("paste proposal is no longer ready")
			}
			t.initTiles = nil // Materialize selected coordinates only when consumed.
			return nil
		}
		p.owner.FinishSelectionMove(p.move, false)
		return nil
	}
	if observer, ok := p.owner.(selectionTransformObserver); ok {
		_ = observer.TrackSelectionTransform(changed, action)
	} else {
		_ = action()
	}
	return true
}

func (t *ToolGrab) CancelPlacement() {
	if t.Placing() {
		if t.placement.controller != nil {
			t.placement.controller.CancelPastePlacement()
			t.placement = nil
			t.Reset()
			return
		}
		t.Reset()
	}
}

func (t *ToolGrab) processPlacement() {
	if !t.Placing() {
		return
	}
	if t.placement.controller != nil {
		if t.placement.controller.PastePlacementClosed() || ed != t.placement.owner {
			t.Reset()
			return
		}
	} else if t.placement.move == nil || t.placement.move.Closed() || ed != t.placement.owner {
		t.Reset()
		return
	}
	if cs == nil || imgui.CurrentIO().WantTextInput() {
		return
	}
	if canvas, ok := cc.(interface{ Active() bool }); ok && !canvas.Active() {
		return
	}
	coord := cs.HoveredTile()
	// Leaving the canvas keeps the last target available to toolbar controls.
	if coord.Z > 0 {
		t.UpdatePlacement(coord)
	}
}

func (t *ToolGrab) clickPlacement(coord util.Point) {
	if canvas, ok := cc.(interface{ Active() bool }); ok && !canvas.Active() {
		return
	}
	if imgui.CurrentIO().WantTextInput() {
		return
	}
	for _, key := range []glfw.Key{glfw.KeyLeftControl, glfw.KeyRightControl, glfw.KeyLeftSuper, glfw.KeyRightSuper, glfw.KeyLeftAlt, glfw.KeyRightAlt, glfw.KeyLeftShift, glfw.KeyRightShift} {
		if imgui.IsKeyDown(int(key)) {
			return
		}
	}
	t.UpdatePlacement(coord)
	t.ConfirmPlacement()
}

// APHELION EDIT ADDITION END
