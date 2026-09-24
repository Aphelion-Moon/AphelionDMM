// APHELION EDIT ADDITION START - PASTE PLACEMENT
package pmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	w "sdmm/internal/imguiext/widget"
)

func (p *PaneMap) canConfirmPaste() bool {
	g, ok := tools.Selected().(*tools.ToolGrab)
	return activePane == p && p.focused && ok && g.Placing() && g.PlacementError() == nil && !imgui.CurrentIO().WantTextInput()
}

func (p *PaneMap) addPasteShortcuts() {
	p.shortcuts.Add(shortcut.Shortcut{Name: "pmap#confirmPaste", FirstKey: glfw.KeyEnter, FirstKeyAlt: glfw.KeyKPEnter, IsEnabled: p.canConfirmPaste, Action: func() { tools.Tools()[tools.TNGrab].(*tools.ToolGrab).ConfirmPlacement() }})
}

func (p *PaneMap) showPastePlacementControls() {
	g, ok := tools.Selected().(*tools.ToolGrab)
	if !ok || !g.Placing() || !p.editor.HasPastePlacement() {
		return
	}
	w.TextWrapped("Paste: move the cursor; [ / ] rotate, H / V mirror. Click or Enter places; Esc cancels.").Build()
	if progress := p.editor.PastePlacementProgress(); progress != "" {
		w.TextWrapped(progress).Build()
	}
	if err := g.PlacementError(); err != nil {
		w.TextWrapped(err.Error()).Build()
	}
	w.Layout{
		w.Disabled(!p.canConfirmPaste(), w.Button("Place (Enter)", func() { g.ConfirmPlacement() })),
		w.SameLine(), w.Button("Cancel (Esc)", g.CancelPlacement),
	}.Build()
}

// APHELION EDIT ADDITION END
