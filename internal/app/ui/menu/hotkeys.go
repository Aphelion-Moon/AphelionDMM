// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
package menu

import (
	"fmt"
	"strings"

	"sdmm/internal/aphelion/hotkeys"
	"sdmm/internal/app/ui/shortcut"

	"github.com/SpaiR/imgui-go"
)

func (m *Menu) openShortcutReference() { m.showHotkeys = true }

func (m *Menu) showShortcutReference() {
	imgui.SetNextWindowSizeV(imgui.Vec2{X: 720, Y: 560}, imgui.ConditionFirstUseEver)
	if imgui.BeginV("Keyboard Shortcuts", &m.showHotkeys, imgui.WindowFlagsNone) {
		imgui.TextWrapped("Map shortcuts apply to the focused map. Open a map to see its bindings. Shortcuts pause while editing a text field.")
		imgui.InputText("Filter", &m.hotkeyFilter)
		imgui.Separator()
		filter := strings.ToLower(strings.TrimSpace(m.hotkeyFilter))
		if conflicts := shortcut.SharedBindings(); len(conflicts) != 0 && imgui.CollapsingHeader(fmt.Sprintf("Shared bindings (%d)", len(conflicts))) {
			imgui.TextWrapped("Shared keys can belong to different panels. Check which panel has focus when a shortcut runs a different action.")
			for _, conflict := range conflicts {
				var descriptions []string
				for _, entry := range hotkeys.Reference([]hotkeys.Binding{conflict.First, conflict.Second}) {
					descriptions = append(descriptions, fmt.Sprintf("%s: %s (%s)", entry.Context, entry.Action, entry.Keys))
				}
				line := strings.Join(descriptions, " / ")
				if filter == "" || strings.Contains(strings.ToLower(line), filter) {
					imgui.BulletText(line)
				}
			}
		}
		context := ""
		for _, entry := range shortcut.Reference() {
			if filter != "" && !strings.Contains(strings.ToLower(entry.Context+" "+entry.Action+" "+entry.Keys), filter) {
				continue
			}
			if entry.Context != context {
				context = entry.Context
				imgui.Spacing()
				imgui.Text(context)
				imgui.Separator()
			}
			imgui.Text(entry.Keys + "    " + entry.Action)
		}
		if filter == "" || strings.Contains("mouse hold drag pick delete replace selection paste clipboard preview enter escape cancel rotate mirror", filter) {
			imgui.Spacing()
			imgui.Separator()
			imgui.Text("Mouse and held tools")
			imgui.TextWrapped("Paste (Ctrl/Cmd+V): move the placement preview with the cursor. [ / ] rotate left/right and H / V mirror the floating template around its bottom-left corner. Click or Enter places it as one undoable edit; Esc cancels. Invalid transforms leave the last preview unchanged; move or transform it successfully before confirming.")
			imgui.TextWrapped("Hold S: Pick. Hold D: Delete (Alt: whole tile). Hold R: Replace. Alt with Add/Fill: replace. Ctrl with Fill: borders only. Shift with Move: adjust pixel/step offsets.")
			imgui.TextWrapped("Grab (3): drag a rectangle to select, then drag inside it to move visible contents. Esc during a drag cancels its preview. [ and ] rotate left/right by 90 degrees around the bottom-left corner, replacing visible destination contents. Directions and pixel/step offsets rotate; other variables are preserved. Undo reverses a rotation.")
			imgui.TextWrapped("Alt+Arrow moves a finished Grab selection by the Selection Move Step in Editor preferences (default: one tile), replacing visible destination contents. Each nudge can be undone. Arrow keys pan the camera; Shift+Arrow pans five times faster.")
			imgui.TextWrapped("H mirrors visible selection contents left/right; V mirrors top/bottom. The rectangle stays in place. Directions and offsets on the reflected axis change; hidden objects stay in place. Each mirror can be undone.")
		}
	}
	imgui.End()
}

// APHELION EDIT ADDITION END
