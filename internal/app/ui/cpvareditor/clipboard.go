// APHELION EDIT ADDITION START - VARIABLE CLIPBOARD
package cpvareditor

import (
	"fmt"
	"strings"

	"sdmm/internal/aphelion/varclip"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

// normalizeValue applies the editor's input conveniences: empty means null and
// an obviously unclosed string or list is closed.
func (v *VarEditor) normalizeValue(varValue string) string {
	if len(varValue) == 0 {
		return dmvars.NullValue
	}
	return v.correctVarIssue(varValue)
}

// acceptValue refuses a value the map format cannot store unchanged. Such a
// value would otherwise make every later save of the map fail validation.
func (v *VarEditor) acceptValue(varName, varValue string) bool {
	value := v.normalizeValue(varValue)
	// The type's own default is removed from the map rather than written.
	if value == v.initialVarValue(varName) && v.environmentObject() != nil {
		return true
	}
	if err := dmmdata.CheckVariableValue(varName, value); err != nil {
		v.notice = "Not applied: " + err.Error()
		v.noticeIsError = true
		log.Warn().Err(err).Msg("variable value rejected")
		return false
	}
	if v.noticeIsError {
		v.notice, v.noticeIsError = "", false
	}
	return true
}

func (v *VarEditor) showNotice() {
	if v.notice == "" {
		return
	}
	if v.noticeIsError {
		imgui.PushStyleColor(imgui.StyleColorText, style.ColorRed)
	}
	imgui.TextWrapped(v.notice)
	if v.noticeIsError {
		imgui.PopStyleColor()
	}
	w.Button(icon.Clear+"##dismiss_var_notice", func() { v.notice, v.noticeIsError = "", false }).
		Tooltip("Dismiss").
		Round(true).
		Build()
}

// showClipboardControls copies every changed variable of the edited object, or
// pastes the copied set onto it as one undoable edit.
func (v *VarEditor) showClipboardControls() {
	changed := v.changedEntries()
	imgui.BeginDisabledV(len(changed) == 0)
	w.Button(fmt.Sprintf("%s Copy changed (%d)", icon.ContentCopy, len(changed)), func() {
		v.varClip.Set(v.prefab.Path(), changed)
		v.notice, v.noticeIsError = fmt.Sprintf("Copied %d variables from %s.", len(changed), v.prefab.Path()), false
	}).Tooltip("Copy every variable that differs from the type's default").Build()
	imgui.EndDisabled()

	if v.varClip.Empty() {
		return
	}
	imgui.SameLine()
	apply, skipped := v.pastePlan(v.varClip.Entries())
	imgui.BeginDisabledV(len(apply) == 0 || v.environmentObject() == nil)
	w.Button(fmt.Sprintf("%s Paste (%d)", icon.ContentPaste, len(apply)), func() {
		v.pasteEntries(apply, skipped)
	}).Tooltip(v.pasteTooltip(apply, skipped)).Build()
	imgui.EndDisabled()
	imgui.SameLine()
	w.Button(icon.Clear+"##clear_var_clipboard", v.varClip.Clear).Tooltip("Clear copied variables").Round(true).Build()
}

func (v *VarEditor) pasteTooltip(apply []varclip.Entry, skipped []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Paste variables copied from %s", v.varClip.Source())
	for _, entry := range apply {
		fmt.Fprintf(&sb, "\n  %s = %s", entry.Name, entry.Value)
	}
	if len(skipped) != 0 {
		fmt.Fprintf(&sb, "\nNot defined or not editable here: %s", strings.Join(skipped, ", "))
	}
	return sb.String()
}

// showVarClipboardButtons draws the per-variable copy button, and a paste
// button when a different value of the same variable has been copied.
func (v *VarEditor) showVarClipboardButtons(varName, varValue string) {
	w.Button(icon.ContentCopy+"##copy_var_"+varName, func() {
		v.varClip.Set(v.prefab.Path(), []varclip.Entry{{Name: varName, Value: varValue}})
		v.notice, v.noticeIsError = fmt.Sprintf("Copied %s = %s", varName, varValue), false
	}).Tooltip("Copy this variable; select another object to paste it").Style(style.ButtonFrame{}).Build()
	imgui.SameLine()
	if copied, ok := v.varClip.Value(varName); ok && copied != varValue {
		w.Button(icon.ContentPaste+"##paste_var_"+varName, func() {
			v.setCurrentVariable(varName, copied)
		}).Tooltip("Paste " + varName + " = " + copied).Style(style.ButtonFrame{}).Build()
		imgui.SameLine()
	}
}

func (v *VarEditor) changedEntries() []varclip.Entry {
	if v.prefab == nil {
		return nil
	}
	names := v.currentVars().Iterate()
	if v.environmentObject() == nil {
		// Without type metadata every explicit value is the object's own.
		entries := make([]varclip.Entry, 0, len(names))
		for _, name := range names {
			entries = append(entries, varclip.Entry{Name: name, Value: v.currentVars().ValueV(name, dmvars.NullValue)})
		}
		return entries
	}
	return varclip.Modified(names,
		func(name string) string { return v.currentVars().ValueV(name, dmvars.NullValue) },
		v.initialVarValue)
}

func (v *VarEditor) pastePlan(entries []varclip.Entry) ([]varclip.Entry, []string) {
	object := v.environmentObject()
	if object == nil {
		return nil, nil
	}
	return varclip.Plan(entries, varclip.Target{
		Defined: func(name string) bool {
			_, ok := object.Vars.Value(name)
			return ok
		},
		ReadOnly: func(name string) bool { return object.Flags(name).Any() },
		Current:  func(name string) string { return v.currentVars().ValueV(name, dmvars.NullValue) },
	})
}

// pasteEntries applies every value as a single edit, so one undo reverts it.
func (v *VarEditor) pasteEntries(entries []varclip.Entry, skipped []string) {
	currentEditor := v.app.CurrentEditor()
	if currentEditor == nil || v.environmentObject() == nil || currentEditor.HasPastePlacement() {
		return
	}
	for _, entry := range entries {
		if !v.acceptValue(entry.Name, entry.Value) {
			return
		}
	}
	if len(entries) == 0 {
		return
	}
	const label = "Paste Variables"
	if v.sessionEditMode == emInstance {
		if !currentEditor.TryBeginInstanceChange(v.instance) {
			currentEditor.CommitOperation(label)
			return
		}
		origPrefab := v.instance.Prefab()
		newPrefab, isNew := dmmap.PrefabStorage.GetV(origPrefab.Path(), v.withEntries(origPrefab.Vars(), entries))
		if isNew {
			if origPrefab.Id() == v.sessionPrefabId {
				dmmap.PrefabStorage.Delete(origPrefab)
			}
			v.sessionPrefabId = newPrefab.Id()
		}
		v.instance.SetPrefab(newPrefab)
		currentEditor.UpdateCanvasByCoords([]util.Point{v.instance.Coord()})
		currentEditor.CommitOperation(label)
		v.app.DoSelectPrefab(newPrefab)
		v.prefab = newPrefab
	} else {
		newPrefab := dmmap.PrefabStorage.Get(v.prefab.Path(), v.withEntries(v.prefab.Vars(), entries))
		currentEditor.ReplacePrefab(v.prefab, newPrefab)
		currentEditor.CommitOperation(label)
		v.app.DoSelectPrefab(newPrefab)
		v.prefab = newPrefab
	}
	v.notice = fmt.Sprintf("Pasted %d variables onto %s.", len(entries), v.prefab.Path())
	if len(skipped) != 0 {
		v.notice += " Skipped (not defined or not editable here): " + strings.Join(skipped, ", ")
	}
	v.noticeIsError = false
	log.Printf("pasted %d variables onto [%s], skipped %v", len(entries), v.prefab.Path(), skipped)
}

func (v *VarEditor) withEntries(vars *dmvars.Variables, entries []varclip.Entry) *dmvars.Variables {
	for _, entry := range entries {
		value := v.normalizeValue(entry.Value)
		if v.initialVarValue(entry.Name) == value {
			vars = dmvars.Delete(vars, entry.Name)
		} else {
			vars = dmvars.Set(vars, entry.Name, value)
		}
	}
	return vars
}

// APHELION EDIT ADDITION END
