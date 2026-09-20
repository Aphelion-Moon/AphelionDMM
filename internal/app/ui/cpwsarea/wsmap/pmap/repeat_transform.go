// APHELION EDIT ADDITION START - REPEAT TRANSFORM
package pmap

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
)

func (p *PaneMap) addRepeatTransformShortcut() {
	p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#repeatTransform", FirstKey: glfw.KeyF4, IsEnabled: p.canRepeatTransform, Action: p.repeatTransform})
}

func (p *PaneMap) canRepeatTransform() bool {
	action, ok := p.editor.LastTransform()
	if !ok || !p.canTransformSelection() {
		return false
	}
	if tools.Selected().(*tools.ToolGrab).Placing() {
		return action.Orientation != 0
	}
	if !p.editor.CanStartMapEdit() {
		return false
	}
	return action.Orientation != 0 || p.canNudgeSelectionBy(action.Shift)
}

func (p *PaneMap) repeatTransform() {
	if !p.canRepeatTransform() {
		return
	}
	action, _ := p.editor.LastTransform()
	switch action.Orientation {
	case editing.PlacementRotateRight:
		p.rotateSelection(true)
	case editing.PlacementRotateLeft:
		p.rotateSelection(false)
	case editing.PlacementMirrorHorizontal:
		p.mirrorSelection(editing.MirrorHorizontal)
	case editing.PlacementMirrorVertical:
		p.mirrorSelection(editing.MirrorVertical)
	default:
		p.nudgeSelectionBy(action.Shift)
	}
}

func (p *PaneMap) showRepeatTransformButton() {
	label := "Repeat last transform"
	if action, ok := p.editor.LastTransform(); ok {
		label = "Repeat: " + action.Label()
	}
	w.Disabled(!p.canRepeatTransform(), w.Button(label+" ("+shortcut.Label("pmap#repeatTransform")+")", p.repeatTransform).
		Tooltip("Apply the remembered transform to the current visible selection. Each accepted repeat is one undoable edit. Paste rotations and mirrors remain a preview until placed.")).Build()
}

// APHELION EDIT ADDITION END
