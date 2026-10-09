package ui

import (
	"fmt"

	"github.com/SpaiR/imgui-go"
)

// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR

// CursorColorApp is implemented by applications that can persist and share the
// local cursor colour. PanelApp is deliberately unchanged; the picker is shown
// only when the application also implements this interface.
type CursorColorApp interface {
	// CollaborationCursorColor returns the saved palette index, or nil when the
	// automatic per-actor colour is in use.
	CollaborationCursorColor() *int
	DoSetCollaborationCursorColor(int)
}

const cursorColorSwatchesPerRow = 6

// showCursorColorPicker draws a compact swatch grid for the local cursor colour.
func (panel *Panel) showCursorColorPicker() {
	app, ok := panel.app.(CursorColorApp)
	if !ok {
		return
	}
	selected := app.CollaborationCursorColor()
	imgui.Text("Your cursor colour")
	size := imgui.Vec2{X: 22, Y: 22}
	for index, entry := range PresencePalette {
		if index%cursorColorSwatchesPerRow != 0 {
			imgui.SameLine()
		}
		fill := imgui.Vec4{X: float32(entry.Color.R) / 255, Y: float32(entry.Color.G) / 255, Z: float32(entry.Color.B) / 255, W: 1}
		if imgui.ColorButton(fmt.Sprintf("%s##collaboration-cursor-color-%d", entry.Name, index), fill, imgui.ColorEditFlagsNoAlpha, size) {
			app.DoSetCollaborationCursorColor(index)
		}
		if selected != nil && *selected == index {
			// Mark the current choice with an outline drawn over the swatch.
			drawList := imgui.WindowDrawList()
			drawList.AddRectV(imgui.ItemRectMin(), imgui.ItemRectMax(), imgui.Packed(PresenceTextColor(entry.Color)), 0, imgui.DrawFlagsNone, 2)
		}
	}
	if selected == nil {
		imgui.TextDisabled("Automatic (pick a swatch to override)")
	}
}

// APHELION EDIT ADDITION END
