// APHELION EDIT ADDITION START - SELECTION MIRROR
package pmap

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func (p *PaneMap) addSelectionMirrorShortcuts() {
	p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#mirrorSelectionHorizontal", FirstKey: glfw.KeyH,
		Action: func() { p.mirrorSelection(editing.MirrorHorizontal) }, IsEnabled: p.canTransformSelection})
	p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#mirrorSelectionVertical", FirstKey: glfw.KeyV,
		Action: func() { p.mirrorSelection(editing.MirrorVertical) }, IsEnabled: p.canTransformSelection})
}

func (p *PaneMap) mirrorSelection(axis editing.MirrorAxis) {
	if !p.canTransformSelection() {
		return
	}
	if g := tools.Selected().(*tools.ToolGrab); g.Placing() {
		transform := editing.PlacementMirrorHorizontal
		if axis == editing.MirrorVertical {
			transform = editing.PlacementMirrorVertical
		}
		_ = g.TransformPlacement(transform)
		return
	}
	if err := tools.Selected().(*tools.ToolGrab).Mirror(axis, p.editor.MirrorSelection); err != nil {
		util.ShowErrorDialog("Unable to mirror selection: " + err.Error())
	}
}

func (p *PaneMap) showSelectionMirrorButtons() {
	w.Layout{
		w.Button("Mirror Horizontal "+shortcut.Label("pmap#mirrorSelectionHorizontal"), func() { p.mirrorSelection(editing.MirrorHorizontal) }).Tooltip("Reflect visible contents left/right inside the selection"),
		w.SameLine(),
		w.Button("Mirror Vertical "+shortcut.Label("pmap#mirrorSelectionVertical"), func() { p.mirrorSelection(editing.MirrorVertical) }).Tooltip("Reflect visible contents top/bottom inside the selection"),
	}.Build()
}

// APHELION EDIT ADDITION END
