// APHELION EDIT ADDITION START - SELECTION NUDGE
package pmap

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmmap"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/go-gl/glfw/v3.3/glfw"
)

var selectionNudges = []struct {
	name  string
	key   glfw.Key
	shift util.Point
}{
	{"Left", glfw.KeyLeft, util.Point{X: -1}},
	{"Right", glfw.KeyRight, util.Point{X: 1}},
	{"Up", glfw.KeyUp, util.Point{Y: 1}},
	{"Down", glfw.KeyDown, util.Point{Y: -1}},
}

func (p *PaneMap) addSelectionNudgeShortcuts() {
	for _, direction := range selectionNudges {
		p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#nudgeSelection" + direction.name,
			FirstKey: glfw.KeyLeftAlt, FirstKeyAlt: glfw.KeyRightAlt, SecondKey: direction.key,
			Action: func() { p.nudgeSelection(direction.shift) }, IsEnabled: func() bool { return p.canNudgeSelection(direction.shift) }})
	}
	// Shifted camera movement was previously an implicit bare-arrow modifier.
	// Register it explicitly now that shortcut matching requires exact modifiers.
	for _, pan := range selectionNudges {
		p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#panCameraFast" + pan.name,
			FirstKey: glfw.KeyLeftShift, FirstKeyAlt: glfw.KeyRightShift, SecondKey: pan.key,
			Action: func() {
				// Speed belongs to the action, even when its custom chord omits Shift.
				step := float32(dmmap.WorldIconSize) * 5
				p.translateCanvas(-float32(pan.shift.X)*step, float32(pan.shift.Y)*step)
			}})
	}
}

func (p *PaneMap) canNudgeSelection(shift util.Point) bool {
	return p.canNudgeSelectionBy(p.selectionNudgeShift(shift))
}

func (p *PaneMap) canNudgeSelectionBy(shift util.Point) bool {
	if !p.canRotateSelection() {
		return false
	}
	area := tools.Selected().(*tools.ToolGrab).Bounds().Plus(float32(shift.X), float32(shift.Y))
	return area.X1 >= 1 && area.Y1 >= 1 && area.X2 <= float32(p.dmm.MaxX) && area.Y2 <= float32(p.dmm.MaxY)
}

func (p *PaneMap) nudgeSelection(shift util.Point) {
	p.nudgeSelectionBy(p.selectionNudgeShift(shift))
}

func (p *PaneMap) nudgeSelectionBy(shift util.Point) {
	if !p.canNudgeSelectionBy(shift) {
		return
	}
	if err := p.editor.TrackRepeatTransform(editing.RepeatTransform{Shift: shift}, false, func() error { return tools.Selected().(*tools.ToolGrab).Nudge(shift) }); err != nil {
		util.ShowErrorDialog("Unable to move selection: " + err.Error())
	}
}

func (p *PaneMap) showSelectionNudgeButtons() {
	step := editing.NormalizeSelectionMoveStep(p.app.Prefs().Editor.SelectionMoveStep)
	w.Text(fmt.Sprintf("Move %d tile(s):", step)).Build()
	for _, direction := range selectionNudges {
		w.SameLine().Build()
		w.Disabled(!p.canNudgeSelection(direction.shift),
			w.Button(direction.name, func() { p.nudgeSelection(direction.shift) }).Tooltip(fmt.Sprintf("%s: move visible selection contents %d tile(s). Configure Selection Move Step in Editor preferences.", shortcut.Label("pmap#nudgeSelection"+direction.name), step))).Build()
	}
}

func (p *PaneMap) selectionNudgeShift(direction util.Point) util.Point {
	step := editing.NormalizeSelectionMoveStep(p.app.Prefs().Editor.SelectionMoveStep)
	return util.Point{X: direction.X * step, Y: direction.Y * step}
}

// APHELION EDIT ADDITION END
