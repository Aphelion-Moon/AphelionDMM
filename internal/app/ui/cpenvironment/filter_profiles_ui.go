package cpenvironment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	native "github.com/sqweek/dialog"

	"sdmm/internal/aphelion/configstore"
	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmenv"
)

const filterProfilesPopup = "Environment Filter Profiles"

func (e *Environment) showFilterProfiles() {
	if imgui.Button("Profiles...") {
		e.openFilterProfiles()
	}
	imgui.SameLine()
	if imgui.Button("Unhide Last") {
		if err := e.UnhideLastFilterVisibility(); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	if imgui.Button("Show All") {
		if err := e.ShowAllFilterVisibility(); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	history := e.filterProfiles.HistoryStatus()
	imgui.BeginDisabledV(history.Position <= 1 || e.filterCompilePending)
	if imgui.Button("Undo visibility") {
		if err := e.enqueueVisibility(visibilityCommand{kind: "undo-visibility"}); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	imgui.EndDisabled()
	imgui.SameLine()
	imgui.BeginDisabledV(history.Position >= history.Count || e.filterCompilePending)
	if imgui.Button("Redo visibility") {
		if err := e.enqueueVisibility(visibilityCommand{kind: "redo-visibility"}); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	imgui.EndDisabled()
	if history.Count > 0 {
		imgui.TextWrapped(fmt.Sprintf("Visibility %d/%d: %s", history.Position, history.Count, history.Label))
	}
	if history.Trimmed {
		imgui.TextWrapped("Oldest visibility history expired (256 states / 4 MiB description budget).")
	}
}

func (e *Environment) openFilterProfiles() {
	if e.filterProfileDialog != nil {
		return
	}
	active := e.activeProfile()
	e.filterProfileChoice, e.filterProfileName = active.ID, active.Name
	e.filterProfileDialog = &profileDialog{owner: e, environment: e.app.LoadedEnvironment()}
	dialog.Open(e.filterProfileDialog)
}

// The application dialog owner submits this even when the docked Environment
// panel is hidden. Closing only releases this dialog; admitted Apply work keeps
// its independent request/environment fencing.
type profileDialog struct {
	owner               *Environment
	environment         *dmenv.Dme
	closing, childPopup bool
}

func (*profileDialog) Name() string         { return filterProfilesPopup }
func (*profileDialog) HasCloseButton() bool { return true }
func (d *profileDialog) OnClose() {
	if d.owner.filterProfileDialog == d {
		d.owner.filterProfileDialog = nil
	}
}
func (d *profileDialog) Process() {
	if d.closing || d.environment != d.owner.app.LoadedEnvironment() {
		imgui.CloseCurrentPopup()
		return
	}
	d.owner.drawFilterProfiles()
	child := imgui.IsPopupOpenV("", imgui.PopupFlagsAnyPopupID)
	if !d.childPopup && !child && imgui.IsWindowFocused() && imgui.IsKeyPressed(int(glfw.KeyEscape)) {
		imgui.CloseCurrentPopup()
	}
	d.childPopup = child
}

func (e *Environment) drawFilterProfiles() {

	choices := append([]filterprofiles.Profile{filterprofiles.DefaultProfile()}, filterprofiles.Builtins()...)
	choices = append(choices, e.config().FilterProfiles.Profiles...)
	preview := e.filterProfileChoiceName()
	if imgui.BeginCombo("Profile", preview) {
		for _, profile := range choices {
			if imgui.SelectableV(profile.Name, profile.ID == e.filterProfileChoice, imgui.SelectableFlagsNone, imgui.Vec2{}) {
				e.filterProfileChoice, e.filterProfileName = profile.ID, profile.Name
			}
		}
		imgui.EndCombo()
	}
	imgui.InputText("Name", &e.filterProfileName)
	selected, selectedOK := e.lookupFilterProfile(e.filterProfileChoice)
	custom := selectedOK && !filterprofiles.IsBuiltin(selected) && !strings.HasPrefix(selected.ID, "session:")
	active := e.activeProfile()
	imgui.BeginDisabledV(e.filterCompilePending)
	if imgui.Button("Apply") && selectedOK {
		e.compileFilterProfile(selected, true)
	}
	imgui.EndDisabled()
	imgui.SameLine()
	// The pinned ImGui version overwrites its single alpha backup on nested
	// BeginDisabled calls. Keep sibling scopes so pending compilation cannot
	// permanently fade unrelated windows on every frame.
	readOnly := e.filterCompilePending || e.filterProfileConfigError != ""
	imgui.BeginDisabledV(readOnly || !custom || active.ID != selected.ID)
	if imgui.Button("Save Changes") && custom && active.ID == selected.ID {
		e.saveCurrent(selected.ID, selected.Name)
	}
	imgui.EndDisabled()
	imgui.SameLine()
	imgui.BeginDisabledV(readOnly)
	if imgui.Button("Save As") {
		e.saveCurrentAs()
	}
	if imgui.Button("Duplicate") && selectedOK {
		e.duplicateFilterProfile(selected)
	}
	imgui.SameLine()
	if imgui.Button("Rename") && custom {
		e.renameFilterProfile(selected)
	}
	imgui.SameLine()
	if imgui.Button("Delete") && custom {
		e.deleteFilterProfile(selected)
	}
	imgui.EndDisabled()
	if imgui.Button("Reset") {
		e.cancelFilterCompile()
		e.compileFilterProfile(active, false)
	}
	imgui.SameLine()
	if imgui.Button("Show All") {
		if err := e.ShowAllFilterVisibility(); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	imgui.SameLine()
	if imgui.Button("Unhide Last") {
		if err := e.UnhideLastFilterVisibility(); err != nil {
			e.filterProfileStatus = err.Error()
		}
	}
	if imgui.Button("Import...") {
		e.importFilterProfile()
	}
	imgui.SameLine()
	if imgui.Button("Export...") && selectedOK {
		e.exportFilterProfile(selected)
	}
	imgui.SameLine()
	if imgui.Button("Close") {
		imgui.CloseCurrentPopup()
	}
	if e.filterProfiles.OverridesDirty() {
		imgui.Text("Unsaved temporary overrides.")
	}
	if e.filterCompilePending {
		imgui.Text("Compiling; the previous policy remains active.")
		if imgui.Button("Cancel pending changes") {
			e.cancelFilterCompile()
			e.filterProfileStatus = "Pending visibility changes cancelled; applied visibility retained."
		}
	}
	if e.filterProfileConfigError != "" {
		imgui.Text("Profile config: " + e.filterProfileConfigError)
	}
	if e.filterProfileMissing != "" {
		imgui.TextWrapped(e.filterProfileMissing)
	}
	if e.filterProfileStatus != "" {
		imgui.Text(e.filterProfileStatus)
	}
	for _, warning := range e.filterProfiles.Warnings() {
		imgui.Text(fmt.Sprintf("Unresolved %s rule: %s", warning.Source, warning.Path))
	}
}

func (e *Environment) lookupFilterProfile(id string) (filterprofiles.Profile, bool) {
	if id == filterprofiles.DefaultProfile().ID {
		return filterprofiles.DefaultProfile(), true
	}
	return e.config().FilterProfiles.Find(id)
}

func (e *Environment) filterProfileChoiceName() string {
	if profile, ok := e.lookupFilterProfile(e.filterProfileChoice); ok {
		return profile.Name
	}
	return "Unavailable profile"
}

func (e *Environment) saveCurrent(id, name string) {
	if e.filterProfileConfigError != "" {
		return
	}
	profile, err := e.filterProfiles.SaveCurrent(id, name)
	if err == nil {
		err = e.config().FilterProfiles.Add(profile)
	}
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	e.compileFilterProfile(profile, true)
}

func (e *Environment) saveCurrentAs() {
	id, err := filterprofiles.NewID()
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	name := strings.TrimSpace(e.filterProfileName)
	if name == "" {
		name = e.activeProfile().Name + " copy"
	}
	e.saveCurrent(id, name)
}

func (e *Environment) duplicateFilterProfile(source filterprofiles.Profile) {
	if e.filterProfileConfigError != "" {
		return
	}
	id, err := filterprofiles.NewID()
	name := strings.TrimSpace(e.filterProfileName)
	if name == "" || name == source.Name {
		name = source.Name + " copy"
	}
	var duplicate filterprofiles.Profile
	if err == nil {
		duplicate, err = filterprofiles.Duplicate(source, id, name)
	}
	if err == nil {
		err = e.config().FilterProfiles.Add(duplicate)
	}
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	e.filterProfileChoice, e.filterProfileName = duplicate.ID, duplicate.Name
	e.filterProfileStatus = "Profile duplicated."
}

func (e *Environment) renameFilterProfile(profile filterprofiles.Profile) {
	if e.filterProfileConfigError != "" {
		return
	}
	if e.filterCompilePending && e.filterProfileChoice == profile.ID {
		e.cancelFilterCompile()
	}
	if err := e.config().FilterProfiles.Rename(profile.ID, strings.TrimSpace(e.filterProfileName)); err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	renamed, _ := e.config().FilterProfiles.Find(profile.ID)
	e.filterProfileName = renamed.Name
	if e.activeProfile().ID == renamed.ID {
		if err := e.filterProfiles.RenameActive(renamed.ID, renamed.Name); err != nil {
			e.filterProfileStatus = err.Error()
			return
		}
	}
	e.filterProfileStatus = "Profile renamed."
}

func (e *Environment) deleteFilterProfile(profile filterprofiles.Profile) {
	if e.filterProfileConfigError != "" {
		return
	}
	if e.filterCompilePending && e.filterProfileChoice == profile.ID {
		e.cancelFilterCompile()
	}
	if err := e.config().FilterProfiles.Delete(profile.ID); err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	e.filterProfileChoice = filterprofiles.DefaultProfile().ID
	if e.activeProfile().ID == profile.ID {
		delete(e.config().ActiveProfileIDs, e.filterProfileProjectKey)
		e.compileFilterProfile(filterprofiles.DefaultProfile(), false)
	} else {
		e.filterProfileStatus = "Profile deleted."
	}
}

func (e *Environment) importFilterProfile() {
	if e.filterProfileConfigError != "" {
		return
	}
	path, err := native.File().Title("Import Filter Profile").Filter("Filter profile", "json").Load()
	if err != nil {
		if !errors.Is(err, native.ErrCancelled) {
			e.filterProfileStatus = err.Error()
		}
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	if info.Size() > filterprofiles.MaxDocumentSize {
		e.filterProfileStatus = "Filter profile file is too large."
		return
	}
	file, err := os.Open(path)
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(file, filterprofiles.MaxDocumentSize+1))
	closeErr := file.Close()
	if readErr != nil {
		e.filterProfileStatus = readErr.Error()
		return
	}
	if closeErr != nil {
		e.filterProfileStatus = closeErr.Error()
		return
	}
	profile, err := filterprofiles.Decode(data)
	if err == nil {
		profile.ID, err = filterprofiles.NewID()
	}
	if err == nil {
		profile.Name = strings.TrimSpace(profile.Name) + " imported"
		err = e.config().FilterProfiles.Add(profile)
	}
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	e.filterProfileChoice, e.filterProfileName = profile.ID, profile.Name
	e.filterProfileStatus = "Profile imported."
}

func (e *Environment) exportFilterProfile(profile filterprofiles.Profile) {
	path, err := native.File().Title("Export Filter Profile").Filter("Filter profile", "json").SetStartFile(profile.Name + ".json").Save()
	if err != nil {
		if !errors.Is(err, native.ErrCancelled) {
			e.filterProfileStatus = err.Error()
		}
		return
	}
	data, err := filterprofiles.Encode(profile)
	if err == nil {
		err = configstore.Write(path, data)
	}
	if err != nil {
		e.filterProfileStatus = err.Error()
		return
	}
	e.filterProfileStatus = "Profile exported."
}
