// APHELION EDIT ADDITION START - SELECTION NUDGE
package pmap

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
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
	for _, pan := range []struct {
		name   string
		key    glfw.Key
		action func()
	}{
		{"Left", glfw.KeyLeft, p.doMoveCameraLeft}, {"Right", glfw.KeyRight, p.doMoveCameraRight},
		{"Up", glfw.KeyUp, p.doMoveCameraUp}, {"Down", glfw.KeyDown, p.doMoveCameraDown},
	} {
		p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#panCameraFast" + pan.name,
			FirstKey: glfw.KeyLeftShift, FirstKeyAlt: glfw.KeyRightShift, SecondKey: pan.key, Action: pan.action})
	}
}

func (p *PaneMap) canNudgeSelection(shift util.Point) bool {
	if !p.canRotateSelection() {
		return false
	}
	shift = p.selectionNudgeShift(shift)
	area := tools.Selected().(*tools.ToolGrab).Bounds().Plus(float32(shift.X), float32(shift.Y))
	return area.X1 >= 1 && area.Y1 >= 1 && area.X2 <= float32(p.dmm.MaxX) && area.Y2 <= float32(p.dmm.MaxY)
}

func (p *PaneMap) nudgeSelection(shift util.Point) {
	if !p.canNudgeSelection(shift) {
		return
	}
	if err := tools.Selected().(*tools.ToolGrab).Nudge(p.selectionNudgeShift(shift)); err != nil {
		util.ShowErrorDialog("Unable to move selection: " + err.Error())
	}
}

func (p *PaneMap) showSelectionNudgeButtons() {
	step := editing.NormalizeSelectionMoveStep(p.app.Prefs().Editor.SelectionMoveStep)
	w.Text(fmt.Sprintf("Move %d tile(s):", step)).Build()
	for _, direction := range selectionNudges {
		w.SameLine().Build()
		w.Disabled(!p.canNudgeSelection(direction.shift),
			w.Button(direction.name, func() { p.nudgeSelection(direction.shift) }).Tooltip(fmt.Sprintf("Alt+%s: move visible selection contents %d tile(s). Configure Selection Move Step in Editor preferences.", direction.name, step))).Build()
	}
}

func (p *PaneMap) selectionNudgeShift(direction util.Point) util.Point {
	step := editing.NormalizeSelectionMoveStep(p.app.Prefs().Editor.SelectionMoveStep)
	return util.Point{X: direction.X * step, Y: direction.Y * step}
}

// APHELION EDIT ADDITION END
