// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
package menu

import (
	"fmt"
	"strings"

	"sdmm/internal/aphelion/hotkeys"
	// APHELION EDIT ADDITION START - LIVE TOOL ACTION HELP
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/shortcut"

	"github.com/SpaiR/imgui-go"
)

func (m *Menu) openShortcutReference() { m.showHotkeys = true }

func (m *Menu) showShortcutReference() {
	imgui.SetNextWindowSizeV(imgui.Vec2{X: 720, Y: 560}, imgui.ConditionFirstUseEver)
	if imgui.BeginV("Keyboard Shortcuts", &m.showHotkeys, imgui.WindowFlagsNone) {
		imgui.TextWrapped("Map shortcuts apply to the focused map. Open a map to see its bindings. Shortcuts pause while editing a text field or dialog. Open menus handle their own listed actions.")
		m.showCurrentToolActionHelp()
		imgui.InputText("Filter", &m.hotkeyFilter)
		imgui.Separator()
		filter := strings.ToLower(strings.TrimSpace(m.hotkeyFilter))
		m.showShortcutEditor(filter)
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
			imgui.TextWrapped("Configured tool and transform bindings are listed above. Hold S/D/R for Pick/Delete/Replace. The current tool's action and mouse-modifier behavior appear at the top of this window.")
			imgui.TextWrapped("Placement previews are confirmed by clicking or the configured confirm action, and canceled with the configured cancel action. Invalid transforms keep the last valid preview.")
			imgui.TextWrapped("Grab selects tiles or moves visible contents. Its configured Replace/Add/Subtract/Intersect operation combines candidate membership with the retained selection; Ctrl and Alt apply the effective membership and area rules shown in the current-action description.")
			imgui.TextWrapped("Alt-Pick hides the exact type throughout the local filter without deleting map contents. Delete removes eligible visible instances or the permitted tile scope; filtered types and required defaults stay protected.")
			imgui.TextWrapped("Selection transforms affect visible contents in selected cells. Hidden objects stay in place. Accepted transforms remain undoable; floating paste transforms stay previews until placement.")
		}
	}
	imgui.End()
}

// APHELION EDIT ADDITION START - LIVE TOOL ACTION HELP
func (m *Menu) showCurrentToolActionHelp() {
	context := tools.CurrentActionContext()
	imgui.Separator()
	imgui.Text("Current map action")
	if context.PersistentTool != "" {
		label := "Persistent tool: " + context.PersistentTool
		if context.IsHeld && context.HeldTool != "" {
			label += " · Held: " + context.HeldTool
		}
		imgui.Text(label)
	}
	if context.GestureTool != "" {
		imgui.Text("Gesture owner: " + context.GestureTool)
	}
	imgui.TextWrapped(context.Action)
	if context.ShortcutAction != "" {
		if keys := shortcut.Label(context.ShortcutAction); keys != "" {
			imgui.Text("Select tool: " + keys)
		}
	}
	if context.ModifierHelp != "" {
		imgui.TextWrapped("Modifiers: " + context.ModifierHelp)
	}
	if context.Scope != "" {
		imgui.TextWrapped("Scope: " + context.Scope)
	}
	if context.Target != "" {
		imgui.TextWrapped("Target: " + context.Target)
	}
	if context.Reason != "" || !context.Available {
		reason := context.Reason
		if reason == "" {
			reason = "Action is unavailable"
		}
		imgui.TextWrapped("Unavailable: " + reason)
	}
}

// APHELION EDIT ADDITION END

func (m *Menu) showShortcutEditor(filter string) {
	if imgui.CollapsingHeader("Customize bindings") {
		imgui.TextWrapped("Choose an action below. Use Ctrl+K or Ctrl+Shift+K; separate alternatives with a semicolon. Use 1/Numpad 1 for aliases, KPAdd for numpad +, and Slash for /. Ctrl, Cmd, Alt and Shift accept either side; LeftCtrl and RightCtrl select one side. An empty binding disables the action.")
		imgui.TextWrapped("Changes apply immediately and save with preferences. Held tools, Space camera dragging and Esc cancellation remain fixed. Key names follow GLFW key codes; keyboard layouts and operating-system shortcuts may affect availability.")
		if imgui.Button("Reset all to defaults") {
			shortcut.ResetAllBindings()
			m.hotkeyDraft = hotkeys.Format(shortcut.BindingsFor(m.hotkeyAction))
			m.hotkeyError = ""
			m.hotkeyAllowShared = false
		}
		imgui.BeginChildV("ShortcutActions", imgui.Vec2{Y: 160}, true, imgui.WindowFlagsNone)
		for _, action := range shortcut.Defaults() {
			entry := hotkeys.Reference([]hotkeys.Binding{{Name: action.Name, Keys: action.Chords[0]}})[0]
			label := entry.Context + ": " + entry.Action
			if filter != "" && !strings.Contains(strings.ToLower(label+" "+shortcut.Label(action.Name)), filter) {
				continue
			}
			if imgui.Selectable(label + "##" + action.Name) {
				m.hotkeyAction = action.Name
				m.hotkeyDraft = hotkeys.Format(shortcut.BindingsFor(action.Name))
				m.hotkeyError = ""
				m.hotkeyAllowShared = false
			}
		}
		imgui.EndChild()
	}
	if m.hotkeyAction == "" {
		return
	}
	entry := hotkeys.Reference([]hotkeys.Binding{{Name: m.hotkeyAction}})[0]
	imgui.Separator()
	imgui.Text("Editing " + entry.Context + ": " + entry.Action)
	if imgui.InputText("Bindings", &m.hotkeyDraft) {
		m.hotkeyError = ""
		m.hotkeyAllowShared = false
	}
	chords, err := hotkeys.Parse(m.hotkeyDraft)
	if err != nil {
		imgui.TextWrapped(err.Error())
	} else {
		if conflicts := shortcut.PreviewConflicts(m.hotkeyAction, chords); len(conflicts) != 0 {
			imgui.TextWrapped("These bindings are also registered for other actions. Focus and enabled state decide which one runs:")
			for _, conflict := range conflicts {
				other := conflict.First
				if other.Name == m.hotkeyAction {
					other = conflict.Second
				}
				entry := hotkeys.Reference([]hotkeys.Binding{other})[0]
				imgui.BulletText(entry.Context + ": " + entry.Action + " (" + entry.Keys + ")")
			}
			imgui.Checkbox("Allow shared binding", &m.hotkeyAllowShared)
		}
	}
	if imgui.Button("Apply binding") {
		m.applyShortcutEdit()
	}
	imgui.SameLine()
	if imgui.Button("Reset action") {
		shortcut.ResetBindings(m.hotkeyAction)
		m.hotkeyDraft = hotkeys.Format(shortcut.BindingsFor(m.hotkeyAction))
		m.hotkeyError = ""
		m.hotkeyAllowShared = false
	}
	imgui.SameLine()
	if imgui.Button("Done") {
		m.hotkeyAction = ""
	}
	if m.hotkeyError != "" {
		imgui.TextWrapped(m.hotkeyError)
	}
	imgui.Separator()
}

func (m *Menu) applyShortcutEdit() {
	chords, err := hotkeys.Parse(m.hotkeyDraft)
	if err == nil && !m.hotkeyAllowShared && len(shortcut.PreviewConflicts(m.hotkeyAction, chords)) != 0 {
		err = fmt.Errorf("choose different keys or enable Allow shared binding")
	}
	if err == nil {
		err = shortcut.SetBindings(m.hotkeyAction, chords)
	}
	m.hotkeyError = ""
	if err != nil {
		m.hotkeyError = err.Error()
	}
}

// APHELION EDIT ADDITION END
