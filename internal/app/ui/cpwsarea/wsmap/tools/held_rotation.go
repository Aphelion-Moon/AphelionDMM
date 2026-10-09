// APHELION EDIT ADDITION START - HELD ROTATION
package tools

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
func heldInteractionTool() Tool {
	if active && startedTool != nil {
		return startedTool
	}
	return Selected()
}

// APHELION EDIT ADDITION END

func rotationLookup() editing.PrefabLookup {
	if owner, ok := ed.(interface{ RotationLookup() editing.PrefabLookup }); ok {
		return owner.RotationLookup()
	}
	return nil
}

func CanRotateHeld() bool {
	if ed == nil {
		return false
	}
	tool := heldInteractionTool()
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if grab, ok := Selected().(*ToolGrab); ok && grab.Placing() {
	if grab, ok := tool.(*ToolGrab); ok && (grab.Placing() || grab.canRotateMove()) {
		return true
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if move, ok := Selected().(*ToolMove); ok && !move.Stale() {
	if move, ok := tool.(*ToolMove); ok && !move.Stale() {
		return true
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if add, ok := Selected().(*ToolAdd); ok {
	if palette, ok := tool.(interface {
		HeldPrefab() (*dmmprefab.Prefab, bool)
	}); ok {
		_, exists := palette.HeldPrefab()
		return exists
	}
	return false
}
func RotateHeld(clockwise bool) error {
	if ed == nil {
		return fmt.Errorf("no active map")
	}
	tool := heldInteractionTool()
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if grab, ok := Selected().(*ToolGrab); ok && grab.Placing() {
	if grab, ok := tool.(*ToolGrab); ok && (grab.Placing() || grab.canRotateMove()) {
		if grab.canRotateMove() {
			return grab.rotateMove(clockwise)
		}
		transform := editing.PlacementRotateLeft
		if clockwise {
			transform = editing.PlacementRotateRight
		}
		return grab.TransformPlacement(transform)
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if move, ok := Selected().(*ToolMove); ok && !move.Stale() {
	if move, ok := tool.(*ToolMove); ok && !move.Stale() {
		return move.rotateHeld(clockwise)
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if add, ok := Selected().(*ToolAdd); ok {
	switch palette := tool.(type) {
	case *ToolAdd:
		if source, exists := palette.HeldPrefab(); exists {
			if err := rotateHeldValue(&palette.held, source, clockwise); err != nil {
				return err
			}
			if palette.shapeStroke != nil {
				palette.shapePrefab = palette.held.Value()
				palette.shapeContext.prefab = palette.shapePrefab
				palette.shapeContext.Target = palette.shapePrefab.Path()
			}
			palette.showHeld()
			return nil
		}
	case *ToolFill:
		if source, exists := palette.HeldPrefab(); exists {
			if err := rotateHeldValue(&palette.held, source, clockwise); err != nil {
				return err
			}
			if palette.dragging {
				palette.prefab = palette.held.Value()
				palette.gestureContext.prefab = palette.prefab
				palette.gestureContext.Target = palette.prefab.Path()
			}
			return nil
		}
	case *ToolReplace:
		if source, exists := palette.HeldPrefab(); exists {
			return rotateHeldValue(&palette.held, source, clockwise)
		}
	}
	return fmt.Errorf("no held payload")
}
func rotateHeldValue(held *editing.HeldPrefab, source *dmmprefab.Prefab, clockwise bool) error {
	if held.Value() != source {
		held.SetSource(source)
	}
	if err := held.Rotate(clockwise, rotationLookup()); err != nil {
		return err
	}
	publishHeldSelection(held)
	return nil
}

// publishHeldSelection makes the rotated palette value the global selection, so
// the environment tree, prefab list and variables show the resulting helper path.
func publishHeldSelection(held *editing.HeldPrefab) {
	owner, ok := ed.(interface{ SelectHeldPrefab(*dmmprefab.Prefab) })
	if !ok || held.Value() == nil {
		return
	}
	owner.SelectHeldPrefab(dmmap.PrefabStorage.Put(held.Value()))
}

func selectedHeldPrefab(held *editing.HeldPrefab) (*dmmprefab.Prefab, bool) {
	p, ok := ed.SelectedPrefab()
	if !ok {
		*held = editing.HeldPrefab{}
		return nil, false
	}
	held.SetSource(p)
	return held.Value(), true
}

func (t *ToolAdd) HeldPrefab() (*dmmprefab.Prefab, bool) {
	if t.shapeStroke != nil {
		return t.shapePrefab, t.shapePrefab != nil && !t.shapeReleased
	}
	return selectedHeldPrefab(&t.held)
}

func (t *ToolFill) HeldPrefab() (*dmmprefab.Prefab, bool) {
	if t.dragging {
		return t.prefab, t.prefab != nil && !t.random
	}
	if owner, ok := ed.(randomFillOwner); ok {
		if settings := owner.RandomFillSettings(); settings != nil && settings.RandomFill {
			return nil, false
		}
	}
	return selectedHeldPrefab(&t.held)
}

func (t *ToolReplace) HeldPrefab() (*dmmprefab.Prefab, bool) {
	return selectedHeldPrefab(&t.held)
}

func (t *ToolReplace) OnDeselect() { t.held = editing.HeldPrefab{} }
func (t *ToolAdd) showHeld() {
	if owner, ok := ed.(interface {
		PreviewHeldPrefab(*dmmprefab.Prefab, util.Point, bool)
	}); ok && cs != nil {
		prefab, _ := t.HeldPrefab()
		owner.PreviewHeldPrefab(prefab, cs.HoveredTile(), t.AltBehaviour())
	}
}
func (t *ToolAdd) OnDeselect() {
	t.shapeStroke = nil
	t.shapeContext = ActionContext{}
	t.shapeReleased = false
	t.shapePrefab = nil
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
	if err := t.held.Rotate(clockwise, rotationLookup()); err != nil {
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
