// APHELION EDIT ADDITION START - HELD ROTATION
package tools

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

func CanRotateHeld() bool {
	if ed == nil {
		return false
	}
	if grab, ok := Selected().(*ToolGrab); ok && grab.Placing() {
		return true
	}
	if move, ok := Selected().(*ToolMove); ok && !move.Stale() {
		return true
	}
	if add, ok := Selected().(*ToolAdd); ok {
		_, exists := add.HeldPrefab()
		return exists
	}
	return false
}
func RotateHeld(clockwise bool) error {
	if ed == nil {
		return fmt.Errorf("no active map")
	}
	if grab, ok := Selected().(*ToolGrab); ok && grab.Placing() {
		transform := editing.PlacementRotateLeft
		if clockwise {
			transform = editing.PlacementRotateRight
		}
		return grab.TransformPlacement(transform)
	}
	if move, ok := Selected().(*ToolMove); ok && !move.Stale() {
		return move.rotateHeld(clockwise)
	}
	if add, ok := Selected().(*ToolAdd); ok {
		if _, exists := add.HeldPrefab(); exists {
			if err := add.held.Rotate(clockwise); err != nil {
				return err
			}
			add.showHeld()
			return nil
		}
	}
	return fmt.Errorf("no held payload")
}
func (t *ToolAdd) HeldPrefab() (*dmmprefab.Prefab, bool) {
	p, ok := ed.SelectedPrefab()
	if !ok {
		t.held = editing.HeldPrefab{}
		return nil, false
	}
	t.held.SetSource(p)
	return t.held.Value(), true
}
func (t *ToolAdd) showHeld() {
	if owner, ok := ed.(interface {
		PreviewHeldPrefab(*dmmprefab.Prefab, util.Point, bool)
	}); ok && cs != nil {
		prefab, _ := t.HeldPrefab()
		owner.PreviewHeldPrefab(prefab, cs.HoveredTile(), t.AltBehaviour())
	}
}
func (t *ToolAdd) OnDeselect() {
	t.held = editing.HeldPrefab{}
	if owner, ok := ed.(interface {
		PreviewHeldPrefab(*dmmprefab.Prefab, util.Point, bool)
	}); ok {
		owner.PreviewHeldPrefab(nil, util.Point{}, false)
	}
}
func (t *ToolMove) rotateHeld(clockwise bool) error {
	if t.instance == nil {
		return fmt.Errorf("no held instance")
	}
	current := t.instance.Prefab()
	// Offset dragging is a new source pose. Consecutive key turns share the
	// original until another input changes the prefab values.
	if current != t.held.Value() {
		t.held.SetSource(current)
	}
	if !ed.TryBeginTileChange(t.instance.Coord()) {
		return fmt.Errorf("unable to capture held instance")
	}
	if err := t.held.Rotate(clockwise); err != nil {
		return err
	}
	t.instance.SetPrefab(t.held.Value())
	t.lastMouseCoords = imgui.MousePos()
	vars := t.instance.Prefab().Vars()
	x, y := "pixel_x", "pixel_y"
	switch ed.Prefs().Editor.NudgeMode {
	case prefs.SaveNudgeModeStep:
		x, y = "step_x", "step_y"
	case prefs.SaveNudgeModePixelAlt:
		x, y = "pixel_w", "pixel_z"
	}
	t.lastOffsets = [2]int{vars.IntV(x, 0), vars.IntV(y, 0)}
	ed.UpdateCanvasByCoords([]util.Point{t.instance.Coord()})
	return nil
}

// APHELION EDIT ADDITION END
