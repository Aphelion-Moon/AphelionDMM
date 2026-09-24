// APHELION EDIT ADDITION START - PASTE PLACEMENT
package pmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
)

func (p *PaneMap) canConfirmPaste() bool {
	g, ok := tools.Selected().(*tools.ToolGrab)
	return activePane == p && p.focused && ok && g.Placing() && !p.editor.PastePlacementPending() && !imgui.CurrentIO().WantTextInput()
}

func (p *PaneMap) addPasteShortcuts() {
	p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#confirmPaste", FirstKey: glfw.KeyEnter, FirstKeyAlt: glfw.KeyKPEnter, IsEnabled: p.canConfirmPaste, Action: func() { tools.Tools()[tools.TNGrab].(*tools.ToolGrab).ConfirmPlacement() }})
}

func (p *PaneMap) showPastePlacementControls() {
	g, ok := tools.Selected().(*tools.ToolGrab)
	if !ok || !g.Placing() || !p.editor.HasPastePlacement() {
		return
	}
	if policy, available := p.editor.PastePolicy(); available {
		modes := []string{"Only Overwrite With Data", "Apply Over", "Replace, Including Blanks"}
		if imgui.BeginCombo("Paste mode", modes[policy.Mode]) {
			for index, name := range modes {
				if imgui.SelectableV(name, policy.Mode == editing.PasteMode(index), 0, imgui.Vec2{}) {
					policy.Mode = editing.PasteMode(index)
					p.editor.SetPastePolicy(policy)
				}
			}
			imgui.EndCombo()
		}
		for index, name := range []string{"Areas", "Turfs", "Objects", "Mobs"} {
			if index != 0 && imgui.ContentRegionAvail().X > 100 {
				imgui.SameLine()
			}
			enabled := policy.Channels&(1<<index) != 0
			if imgui.Checkbox(name, &enabled) {
				if enabled {
					policy.Channels |= 1 << index
				} else {
					policy.Channels &^= 1 << index
				}
				p.editor.SetPastePolicy(policy)
			}
		}
	}
	w.Layout{
		w.Disabled(!p.canConfirmPaste(), w.Button("Place (Enter)", func() { g.ConfirmPlacement() })),
		w.SameLine(), w.Disabled(!p.editor.CanCancelPastePlacement(), w.Button("Cancel (Esc)", g.CancelPlacement)),
	}.Build()
}

// APHELION EDIT ADDITION END
