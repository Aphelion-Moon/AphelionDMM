package cpenvironment

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
)

func (e *Environment) BindFilterEnvironment(environment *dmenv.Dme) {
	e.bindFilterEnvironment(environment)
}

func (e *Environment) invalidateFilterProfiles() {
	if e.filterProfileDialog != nil {
		e.filterProfileDialog.closing = true
	}
	e.cancelFilterCompile()
	e.filterProfiles = filterprofiles.Session{}
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

// Visibility commands share one worker and a bounded FIFO. Reset/profile Apply/
// Show All supersede older intent; cancellation fences publication but keeps the
// worker admitted until its completion is drained.
type visibilityCommand struct {
	kind             string
	profile          filterprofiles.Profile
	path             string
	scope            filterprofiles.Scope
	visible, persist bool
}

const maxPendingVisibilityCommands = 64

func (e *Environment) compileFilterProfile(profile filterprofiles.Profile, persist bool) {
	e.cancelFilterCompile()
	e.filterProfileChoice = profile.ID
	if err := e.enqueueVisibility(visibilityCommand{kind: "apply", profile: filterprofiles.Clone(profile), persist: persist}); err != nil {
		e.filterProfileStatus = err.Error()
	}
}

func (e *Environment) enqueueVisibility(command visibilityCommand) error {
	if e.filterProfileEnvironment == nil || e.app.PathsFilter() == nil {
		return errors.New("no environment visibility controller is available")
	}
	if len(e.filterCommands) >= maxPendingVisibilityCommands {
		return errors.New("visibility queue is full; wait for the pending changes and retry")
	}
	e.filterCommands = append(e.filterCommands, command)
	e.filterCompilePending = true
	e.filterProfileStatus = "Visibility change pending; the previous policy remains active."
	e.startVisibilityWorker()
	return nil
}

func (e *Environment) startVisibilityWorker() {
	if e.filterWorkerActive || len(e.filterCommands) == 0 {
		return
	}
	command := e.filterCommands[0]
	e.filterCommands[0] = visibilityCommand{}
	e.filterCommands = e.filterCommands[1:]
	e.filterWorkerActive = true
	generation, environment := e.filterCompileGeneration, e.filterProfileEnvironment
	captured := e.filterProfiles
	publish := window.RunLater
	if scheduler, ok := e.app.(interface{ RunLater(func()) }); ok {
		publish = scheduler.RunLater
	}
	go func() {
		next := captured.Fork()
		prepared := dm.NewPathsFilterEmpty()
		var err error
		switch command.kind {
		case "apply":
			err = next.Apply(command.profile, environment, prepared)
		case "visibility":
			err = next.SetVisibility(command.path, command.scope, command.visible, environment, prepared)
		case "show-all":
			err = next.ShowAll(environment, prepared)
		case "unhide":
			err = next.UnhideLast(environment, prepared)
		case "undo-visibility":
			err = next.UndoVisibility(environment, prepared)
		case "redo-visibility":
			err = next.RedoVisibility(environment, prepared)
		}
		publish(func() {
			e.filterWorkerActive = false
			valid := generation == e.filterCompileGeneration && environment == e.filterProfileEnvironment && environment == e.app.LoadedEnvironment()
			if valid {
				if err != nil {
					e.filterProfileStatus = err.Error()
				} else {
					e.app.PathsFilter().AdoptPreparedPolicy(prepared)
					e.filterProfiles = next
					if command.kind == "apply" {
						profile := command.profile
						if command.persist && e.filterProfileProjectKey != "" {
							if strings.HasPrefix(profile.ID, "session:") {
								delete(e.config().ActiveProfileIDs, e.filterProfileProjectKey)
							} else {
								e.config().ActiveProfileIDs[e.filterProfileProjectKey] = profile.ID
							}
						}
						e.filterProfileChoice = profile.ID
						e.filterProfileStatus = fmt.Sprintf("Applied %s.", profile.Name)
					} else if command.kind == "show-all" {
						e.filterProfileStatus = "All types shown until Reset or profile apply."
					} else if command.kind == "unhide" {
						e.filterProfileStatus = "Last hidden type shown."
					} else if command.kind == "undo-visibility" || command.kind == "redo-visibility" {
						e.filterProfileChoice = e.activeProfile().ID
						if e.filterProfileProjectKey != "" {
							if strings.HasPrefix(e.filterProfileChoice, "session:") {
								delete(e.config().ActiveProfileIDs, e.filterProfileProjectKey)
							} else {
								e.config().ActiveProfileIDs[e.filterProfileProjectKey] = e.filterProfileChoice
							}
						}
						e.filterProfileStatus = "Visibility restored: " + next.HistoryStatus().Label
					} else {
						action := "Shown"
						if !command.visible {
							action = "Hidden"
						}
						e.filterProfileStatus = fmt.Sprintf("%s %s type: %s", action, command.scope, command.path)
					}
				}
			}
			e.filterCompilePending = len(e.filterCommands) != 0
			e.startVisibilityWorker()
		})
	}()
}

func (e *Environment) cancelFilterCompile() {
	e.filterCompileGeneration++
	e.filterCommands = nil
	e.filterCompilePending = e.filterWorkerActive
	e.filterProfileChoice = e.activeProfile().ID
}

func (e *Environment) SetFilterVisibility(path string, scope filterprofiles.Scope, visible bool) (err error) {
	defer func() {
		if err != nil {
			e.filterProfileStatus = err.Error()
		}
	}()
	environment := e.app.LoadedEnvironment()
	if environment == nil || scope == filterprofiles.ScopeSubtree && environment.Objects[path] == nil {
		return fmt.Errorf("type %q is not present in the loaded environment", path)
	}
	if e.filterProfileEnvironment != environment {
		e.bindFilterEnvironment(environment)
	}
	return e.enqueueVisibility(visibilityCommand{kind: "visibility", path: path, scope: scope, visible: visible})
}

func (e *Environment) ShowAllFilterVisibility() error {
	e.cancelFilterCompile()
	return e.enqueueVisibility(visibilityCommand{kind: "show-all"})
}

func (e *Environment) UnhideLastFilterVisibility() error {
	return e.enqueueVisibility(visibilityCommand{kind: "unhide"})
}

func (e *Environment) FilterVisibilityStatus() string { return e.filterProfileStatus }
func (e *Environment) activeProfile() filterprofiles.Profile {
	if profile, ok := e.filterProfiles.Active(); ok {
		return profile
	}
	return filterprofiles.DefaultProfile()
}
