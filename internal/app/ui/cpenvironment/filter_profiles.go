package cpenvironment

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmenv"
)

func (e *Environment) BindFilterEnvironment(environment *dmenv.Dme) {
	e.bindFilterEnvironment(environment)
}

func (e *Environment) invalidateFilterProfiles() {
	e.filterCompileGeneration++
	e.filterCompilePending = false
	e.filterProfileEnvironment = nil
	e.filterProfileProjectKey = ""
}

func (e *Environment) bindFilterEnvironment(environment *dmenv.Dme) {
	if e.filterProfileEnvironment == environment {
		return
	}
	e.invalidateFilterProfiles()
	e.filterProfileMissing = ""
	if environment == nil {
		return
	}
	key := ""
	if environment.RootFile != "" {
		absolute, err := filepath.Abs(environment.RootFile)
		key = absolute
		if err != nil {
			key = filepath.Clean(environment.RootFile)
		} else {
			key = filepath.Clean(key)
		}
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	e.filterProfileEnvironment = environment
	e.filterProfileProjectKey = key
	profile := filterprofiles.DefaultProfile()
	activeID := e.config().ActiveProfileIDs[key]
	if activeID != "" {
		if stored, ok := e.config().FilterProfiles.Find(activeID); ok {
			profile = stored
		} else {
			e.filterProfileMissing = fmt.Sprintf("Saved filter profile %q is unavailable; using current visibility.", activeID)
			e.filterProfileChoice = profile.ID
			e.compileFilterProfile(profile, false)
			return
		}
	}
	e.filterProfileChoice = profile.ID
	e.compileFilterProfile(profile, false)
}

func (e *Environment) compileFilterProfile(profile filterprofiles.Profile, persist bool) {
	if e.filterCompilePending {
		return
	}
	environment := e.filterProfileEnvironment
	if environment == nil {
		e.filterProfileStatus = "No environment is loaded."
		return
	}
	e.filterCompileGeneration++
	generation := e.filterCompileGeneration
	profile = filterprofiles.Clone(profile)
	e.filterCompilePending = true
	e.filterProfileChoice = profile.ID
	e.filterProfileStatus = "Compiling; the previous visibility policy remains active."
	publish := window.RunLater
	if scheduler, ok := e.app.(interface{ RunLater(func()) }); ok {
		publish = scheduler.RunLater
	}
	go func() {
		compiled, err := filterprofiles.Compile(profile, filterprofiles.Overrides{}, environment)
		publish(func() {
			if generation != e.filterCompileGeneration ||
				environment != e.filterProfileEnvironment ||
				environment != e.app.LoadedEnvironment() {
				return
			}
			e.filterCompilePending = false
			if err != nil {
				e.filterProfileStatus = err.Error()
				e.filterProfileChoice = e.activeProfile().ID
				return
			}
			if err := e.filterProfiles.ApplyCompiled(profile, compiled, e.app.PathsFilter()); err != nil {
				e.filterProfileStatus = err.Error()
				e.filterProfileChoice = e.activeProfile().ID
				return
			}
			if persist && e.filterProfileProjectKey != "" {
				if strings.HasPrefix(profile.ID, "session:") {
					delete(e.config().ActiveProfileIDs, e.filterProfileProjectKey)
				} else {
					e.config().ActiveProfileIDs[e.filterProfileProjectKey] = profile.ID
				}
			}
			e.filterProfileChoice = profile.ID
			e.filterProfileStatus = fmt.Sprintf("Applied %s.", profile.Name)
		})
	}()
}

func (e *Environment) cancelFilterCompile() {
	e.filterCompileGeneration++
	e.filterCompilePending = false
	e.filterProfileChoice = e.activeProfile().ID
}

func (e *Environment) SetFilterVisibility(path string, scope filterprofiles.Scope, visible bool) error {
	environment := e.app.LoadedEnvironment()
	if environment == nil || scope == filterprofiles.ScopeSubtree && environment.Objects[path] == nil {
		return fmt.Errorf("type %q is not present in the loaded environment", path)
	}
	if e.filterProfileEnvironment != environment {
		e.bindFilterEnvironment(environment)
	}
	if e.filterCompilePending {
		return errors.New("filter profile is compiling; try again when it finishes")
	}
	if err := e.filterProfiles.SetVisibility(path, scope, visible, environment, e.app.PathsFilter()); err != nil {
		e.filterProfileStatus = err.Error()
		return err
	}
	action := "Shown"
	if !visible {
		action = "Hidden"
	}
	e.filterProfileStatus = fmt.Sprintf("%s %s.", action, path)
	return nil
}

func (e *Environment) ShowAllFilterVisibility() error {
	e.cancelFilterCompile()
	err := e.filterProfiles.ShowAll(e.app.LoadedEnvironment(), e.app.PathsFilter())
	if err != nil {
		e.filterProfileStatus = err.Error()
	} else {
		e.filterProfileStatus = "All types shown until Reset or profile apply."
	}
	return err
}

func (e *Environment) activeProfile() filterprofiles.Profile {
	if profile, ok := e.filterProfiles.Active(); ok {
		return profile
	}
	return filterprofiles.DefaultProfile()
}
