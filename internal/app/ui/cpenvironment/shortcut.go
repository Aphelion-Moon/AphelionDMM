package cpenvironment

import (
	"sdmm/internal/app/ui/shortcut"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func (e *Environment) addShortcuts() {
	// APHELION EDIT CHANGE - ENVIRONMENT TYPES FILTER - ORIGINAL: Action:   e.doToggleTypesFilter,
	// The binding id is kept because user rebindings are stored by name.
	e.shortcuts.Add(shortcut.Shortcut{
		Name:     "cpenvironment#doToggleTypesFilter",
		FirstKey: glfw.KeyF,
		Action:   e.doToggleSelectedVisibility,
	})
}
