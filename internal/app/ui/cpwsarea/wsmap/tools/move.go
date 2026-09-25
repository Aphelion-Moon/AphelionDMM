package tools

import (
	// APHELION EDIT ADDITION START - HELD ROTATION
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	// APHELION EDIT REMOVAL START - SHARED TOOL FEEDBACK
	// "sdmm/internal/imguiext"
	// APHELION EDIT REMOVAL END
	"sdmm/internal/util"
	"strconv"

	"github.com/SpaiR/imgui-go"
)

// ToolMove can be used move a single object.
type ToolMove struct {
	tool
	instance        *dmminstance.Instance
	lastTile        *dmmap.Tile
	lastMouseCoords imgui.Vec2
	lastOffsets     [2]int
	// APHELION EDIT ADDITION START - HELD ROTATION
	held editing.HeldPrefab
	// APHELION EDIT ADDITION END
}

func (ToolMove) Name() string {
	return TNMove
}

func newMove() *ToolMove {
	return &ToolMove{}
}

func (t *ToolMove) Stale() bool {
	return t.instance == nil
}

func (ToolMove) AltBehaviour() bool {
	return false
}

func (t *ToolMove) onStart(util.Point) {
	// APHELION EDIT ADDITION START - INSTANCE MOVE CAPTURE
	if !ed.CanStartMapEdit() {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if hoveredInstance := ed.HoveredInstance(); hoveredInstance != nil {
	if hoveredInstance := t.actionContext.targetInstance; hoveredInstance != nil {
		// APHELION EDIT ADDITION START - COMPOSITION ROOT MOVE
		setCompositionRootGesture(hoveredInstance, true)
		// APHELION EDIT ADDITION END
		// APHELION EDIT ADDITION START - INSTANCE MOVE CAPTURE
		if !ed.TryBeginTileChange(hoveredInstance.Coord()) {
			// APHELION EDIT ADDITION START - COMPOSITION ROOT MOVE
			setCompositionRootGesture(nil, false)
			// APHELION EDIT ADDITION END
			ed.CommitOperation("Moved Prefab") // Report the capture fault without starting a gesture.
			return
		}
		// APHELION EDIT ADDITION END
		ed.InstanceSelect(hoveredInstance)
		t.instance = hoveredInstance
		// APHELION EDIT ADDITION START - HELD ROTATION
		t.held = editing.HeldPrefab{}
		t.held.SetSource(hoveredInstance.Prefab())
		// APHELION EDIT ADDITION END
		t.lastMouseCoords = imgui.MousePos()
		vars := t.instance.Prefab().Vars()
		switch ed.Prefs().Editor.NudgeMode {
		case prefs.SaveNudgeModePixel:
			t.lastOffsets = [2]int{vars.IntV("pixel_x", 0), vars.IntV("pixel_y", 0)}
		case prefs.SaveNudgeModeStep:
			t.lastOffsets = [2]int{vars.IntV("step_x", 0), vars.IntV("step_y", 0)}
		case prefs.SaveNudgeModePixelAlt:
			t.lastOffsets = [2]int{vars.IntV("pixel_w", 0), vars.IntV("pixel_z", 0)}
		}
	}
}

// APHELION EDIT ADDITION START - COMPOSITION ROOT MOVE
func setCompositionRootGesture(instance *dmminstance.Instance, active bool) {
	if owner, ok := ed.(interface {
		SetCompositionRootGesture(*dmminstance.Instance, bool)
	}); ok {
		owner.SetCompositionRootGesture(instance, active)
	}
}

// APHELION EDIT ADDITION END

func (t *ToolMove) process() {
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if t.instance == nil || !imguiext.IsShiftDown() {
	if t.instance == nil || !t.actionContext.Modifiers.Shift {
		return
	}
	xAxis := "pixel_x"
	yAxis := "pixel_y"
	if ed.Prefs().Editor.NudgeMode == prefs.SaveNudgeModeStep {
		xAxis = "step_x"
		yAxis = "step_y"
	} else if ed.Prefs().Editor.NudgeMode == prefs.SaveNudgeModePixelAlt {
		xAxis = "pixel_w"
		yAxis = "pixel_z"
	}
	origPrefab := t.instance.Prefab()
	mouseCoords := imgui.MousePos()
	offsetX := (mouseCoords.X - t.lastMouseCoords.X) / ed.ZoomLevel()
	offsetY := (t.lastMouseCoords.Y - mouseCoords.Y) / ed.ZoomLevel()
	// APHELION EDIT ADDITION START - EFFECTIVE OFFSET GUARD
	newX, newY := t.lastOffsets[0]+int(offsetX), t.lastOffsets[1]+int(offsetY)
	if origPrefab.Vars().IntV(xAxis, 0) == newX && origPrefab.Vars().IntV(yAxis, 0) == newY {
		return
	}
	if !ed.TryBeginTileChange(t.instance.Coord()) {
		return
	}
	// APHELION EDIT ADDITION END

	newVars := dmvars.Set(origPrefab.Vars(), xAxis, strconv.Itoa(t.lastOffsets[0]+int(offsetX)))
	newVars = dmvars.Set(newVars, yAxis, strconv.Itoa(t.lastOffsets[1]+int(offsetY)))
	t.instance.SetPrefab(dmmprefab.New(dmmprefab.IdNone, origPrefab.Path(), newVars))

	ed.UpdateCanvasByCoords([]util.Point{t.instance.Coord()})
}

func (t *ToolMove) onMove(coord util.Point) {
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if t.instance == nil || imguiext.IsShiftDown() {
	if t.instance == nil || t.actionContext.Modifiers.Shift {
		return
	}

	// APHELION EDIT CHANGE - INSTANCE MOVE IDENTITY - ORIGINAL: prefab := t.instance.Prefab()
	sourceCoord := t.instance.Coord()
	// APHELION EDIT ADDITION START - INSTANCE MOVE IDENTITY
	if sourceCoord == coord {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - INSTANCE MOVE CAPTURE
	// Capture both tiles before deleting or regenerating anything. A failed
	// destination must leave an earlier valid preview intact for recovery.
	if !ed.TryBeginTileChange(sourceCoord) || !ed.TryBeginTileChange(coord) {
		return
	}
	// APHELION EDIT ADDITION END
	if t.lastTile != nil {
		t.lastTile.InstancesRegenerate() //should stop some issues
	}
	t.lastTile = ed.Dmm().GetTile(coord)
	ed.InstanceDelete(t.instance)
	/* APHELION EDIT REMOVAL START - INSTANCE MOVE IDENTITY
	t.lastTile.InstancesAdd(prefab)
	t.lastTile.InstancesRegenerate()
	for _, found := range t.lastTile.Instances() {
		if found.Prefab().Id() == prefab.Id() {
			t.instance = found
			break
		}
	}
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - INSTANCE MOVE IDENTITY
	// Moving the actual instance preserves stable/local IDs and the properties
	// panel's reference, even when an identical prefab already occupies the tile.
	t.instance.SetCoord(coord)
	t.lastTile.Set(append(t.lastTile.Instances(), t.instance))
	t.lastTile.InstancesRegenerate()
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - INSTANCE MOVE IDENTITY - ORIGINAL: ed.UpdateCanvasByCoords([]util.Point{coord})
	ed.UpdateCanvasByCoords([]util.Point{sourceCoord, coord})
}

func (t *ToolMove) onStop(util.Point) {
	// APHELION EDIT ADDITION START - COMPOSITION ROOT MOVE
	defer setCompositionRootGesture(nil, false)
	// APHELION EDIT ADDITION END
	if t.instance == nil {
		return
	}
	//remove other turfs if we moved a turf
	// APHELION EDIT CHANGE - INSTANCE MOVE CAPTURE - ORIGINAL: if t.lastTile != nil {
	if t.lastTile != nil && ed.TryBeginTileChange(t.instance.Coord()) {
		if dm.IsPath(t.instance.Prefab().Path(), "/turf") {
			for _, found := range t.lastTile.Instances() {
				if dm.IsPath(found.Prefab().Path(), "/turf") && found != t.instance {
					ed.InstanceDelete(found)
				}
			}
		}
	}
	t.instance = nil
	// APHELION EDIT ADDITION START - HELD ROTATION
	t.held = editing.HeldPrefab{}
	// APHELION EDIT ADDITION END
	t.lastTile = nil
	ed.CommitOperation("Moved Prefab")
}
