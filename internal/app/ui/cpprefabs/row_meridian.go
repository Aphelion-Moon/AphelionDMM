// APHELION EDIT ADDITION START - MERIDIAN THEME
package cpprefabs

import (
	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/theme"
	"sdmm/internal/app/ui/uikit"
)

// prefabTextLines is the text height of a prefab row: one line under
// Meridian (name, then the edits in monospace), two under Classic.
func prefabTextLines() int {
	if theme.IsMeridian() {
		return 1
	}
	return 2
}

// drawPrefabRowInline draws the name and, after it, the variable edits in
// monospace, end-truncated to the panel with the full text as a tooltip.
// Text is drawn directly and the row reserves exactly one line of
// max(icon, text) height, so the list clipper's stride stays exact.
func drawPrefabRowInline(iconSize float32, name, description string) {
	style := imgui.CurrentStyle()
	start := imgui.CursorScreenPos()
	lineHeight := imgui.TextLineHeight()
	rowHeight := max(iconSize, lineHeight)
	y := start.Y + (rowHeight-lineHeight)/2
	list := imgui.WindowDrawList()
	list.AddText(imgui.Vec2{X: start.X, Y: y}, imgui.PackedColorFromVec4(style.Color(imgui.StyleColorText)), name)
	width := imgui.ContentRegionAvail().X
	truncated := false
	if description != "" {
		x := start.X + imgui.CalcTextSize(name, false, 0).X + style.ItemSpacing().X
		uikit.Mono(func() {
			shown := uikit.EndTruncate(description, width-(x-start.X), func(s string) float32 { return imgui.CalcTextSize(s, false, 0).X })
			truncated = shown != description
			list.AddText(imgui.Vec2{X: x, Y: y + (lineHeight-imgui.TextLineHeight())/2}, imgui.PackedColorFromVec4(style.Color(imgui.StyleColorTextDisabled)), shown)
		})
	}
	imgui.Dummy(imgui.Vec2{X: max(1, width), Y: rowHeight})
	if truncated && imgui.IsItemHovered() {
		imgui.SetTooltip(description)
	}
}

// APHELION EDIT ADDITION END
