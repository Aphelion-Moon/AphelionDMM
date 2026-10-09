// APHELION EDIT ADDITION START - MAPPING HELPER PANEL
package layout

import "github.com/SpaiR/imgui-go"

// tabHighlighter is a node whose dock tab can ask for attention, such as the
// mapping helpers tab when the selected object has helpers.
type tabHighlighter interface {
	TabHighlight() bool
}

// Inactive tabs move this far toward the accent; active tabs half as far.
const tabTint = 0.45

// pushTabHighlight tints node's tab when it asks for it and returns the number
// of colours pushed. Docked tabs take their colours from the window's style
// at Begin (ImGuiWindowDockStyle), so the caller pops right after Begin.
func pushTabHighlight(node layoutNode) int {
	h, ok := node.(tabHighlighter)
	if !ok || !h.TabHighlight() {
		return 0
	}
	style := imgui.CurrentStyle()
	accent := style.Color(imgui.StyleColorCheckMark)
	colors := []struct {
		id     imgui.StyleColorID
		amount float32
	}{
		{imgui.StyleColorTab, tabTint},
		{imgui.StyleColorTabUnfocused, tabTint},
		{imgui.StyleColorTabHovered, tabTint},
		{imgui.StyleColorTabActive, tabTint / 2},
		{imgui.StyleColorTabUnfocusedActive, tabTint / 2},
	}
	for _, c := range colors {
		imgui.PushStyleColor(c.id, mix(style.Color(c.id), accent, c.amount))
	}
	return len(colors)
}

func mix(a, b imgui.Vec4, t float32) imgui.Vec4 {
	return imgui.Vec4{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t, Z: a.Z + (b.Z-a.Z)*t, W: a.W}
}

// APHELION EDIT ADDITION END
