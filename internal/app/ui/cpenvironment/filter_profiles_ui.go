package cpenvironment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SpaiR/imgui-go"
	native "github.com/sqweek/dialog"

	"sdmm/internal/aphelion/configstore"
	"sdmm/internal/aphelion/filterprofiles"
)

const filterProfilesPopup = "Environment Filter Profiles"

func (e *Environment) showFilterProfiles() {
	if imgui.Button("Profiles...") {
		active := e.activeProfile()
		e.filterProfileChoice, e.filterProfileName = active.ID, active.Name
		imgui.OpenPopup(filterProfilesPopup)
	}
	if !imgui.BeginPopupModalV(filterProfilesPopup, nil, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoSavedSettings) {
		return
	}
	defer imgui.EndPopup()

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
	imgui.BeginDisabledV(e.filterCompilePending || e.filterProfileConfigError != "")
	imgui.BeginDisabledV(!custom || active.ID != selected.ID)
	if imgui.Button("Save Changes") && custom && active.ID == selected.ID {
		e.saveCurrent(selected.ID, selected.Name)
	}
	imgui.EndDisabled()
	imgui.SameLine()
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
		e.cancelFilterCompile()
		if err := e.filterProfiles.ShowAll(e.app.LoadedEnvironment(), e.app.PathsFilter()); err != nil {
			e.filterProfileStatus = err.Error()
		} else {
			e.filterProfileStatus = "All types shown until Reset or profile apply."
		}
	}
	imgui.SameLine()
	if imgui.Button("Unhide Last") {
		e.cancelFilterCompile()
		if err := e.filterProfiles.UnhideLast(e.app.LoadedEnvironment(), e.app.PathsFilter()); err != nil {
			e.filterProfileStatus = err.Error()
		} else {
			e.filterProfileStatus = "Last hidden type shown."
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
