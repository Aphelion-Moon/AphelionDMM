package tools

import (
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// Tool is a basic interface for tools in the panel.
type Tool interface {
	Name() string
	// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
	ActionContext(ActionInput) ActionContext
	setActionContext(ActionContext)
	captureActionContext()
	clearActionContext()
	// APHELION EDIT ADDITION END

	IgnoreBounds() bool
	Stale() bool
	AltBehaviour() bool
	setAltBehaviour(bool)

	// OnDeselect gees when the current tool is deselected.
	OnDeselect()

	// Goes every app cycle to handle stuff like pushing overlays etc.
	process()
	// Goes when user clicks on the map.
	onStart(coord util.Point)
	// Goes when user clicked and, while holding the mouse button, move the mouse.
	onMove(coord util.Point)
	// Goes when user releases the mouse button.
	onStop(coord util.Point)
}

// Tool is a basic interface for tools in the panel.
type tool struct {
	altBehaviour bool
	// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
	actionContext   ActionContext
	gestureContext  ActionContext
	gestureCaptured bool
	// APHELION EDIT ADDITION END
}

func (tool) IgnoreBounds() bool {
	return false
}

func (tool) Stale() bool {
	return true
}

func (t *tool) AltBehaviour() bool {
	return t.altBehaviour
}

func (t *tool) setAltBehaviour(altBehaviour bool) {
	t.altBehaviour = altBehaviour
}

// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
func (t *tool) setActionContext(context ActionContext) {
	t.actionContext = context
	t.altBehaviour = context.Alternate
}

func (t *tool) captureActionContext() {
	t.gestureContext = t.actionContext
	t.gestureCaptured = true
}

func (t *tool) clearActionContext() {
	t.gestureContext = ActionContext{}
	t.gestureCaptured = false
}

// APHELION EDIT ADDITION END

func (tool) process() {
}

//nolint:unused
func (tool) onStart(util.Point) {
}

func (tool) onMove(util.Point) {
}

func (tool) onStop(util.Point) {
}

func (tool) OnDeselect() {
}

// A basic behaviour add.
// Adds object above and tile with a replacement.
// Mirrors that behaviour in the alt mode.
func (t *tool) basicPrefabAdd(tile *dmmap.Tile, prefab *dmmprefab.Prefab) {
	// APHELION EDIT ADDITION START - BRUSH CAPTURE
	if !ed.TryBeginTileChange(tile.Coord) {
		return
	}
	// APHELION EDIT ADDITION END
	if !t.altBehaviour {
		if dm.IsPath(prefab.Path(), "/area") {
			tile.InstancesRemoveByPath("/area")
		} else if dm.IsPath(prefab.Path(), "/turf") {
			tile.InstancesRemoveByPath("/turf")
		}
	} else if dm.IsPath(prefab.Path(), "/obj") {
		tile.InstancesRemoveByPath("/obj")
	}

	tile.InstancesAdd(prefab)
	tile.InstancesRegenerate()
}
