package app

import (
	// APHELION EDIT ADDITION START - FILE DROP
	"sdmm/internal/aphelion/envresolve"
	// APHELION EDIT ADDITION END
	"os"
	"path/filepath"
	// APHELION EDIT ADDITION START - FILE DROP
	"strings"
	// APHELION EDIT ADDITION END
)

// Check program arguments to load dme/dmm files passed by.
func (a *app) checkProgramArgs() {
	// APHELION EDIT ADDITION START - FILE DROP
	if a.masterWindow != nil {
		a.masterWindow.SetDropHandler(a.loadDroppedPaths)
	}
	// APHELION EDIT ADDITION END

	// The first argument is always a path to the executable.
	if len(os.Args) < 2 {
		return
	}

	var envPath string
	var mapPaths []string

	for _, arg := range os.Args {
		switch filepath.Ext(arg) {
		case ".dme":
			envPath = arg
		case ".dmm":
			mapPaths = append(mapPaths, arg)
		// APHELION EDIT ADDITION START - FILE DROP
		case ".tgm":
			mapPaths = append(mapPaths, arg)
			// APHELION EDIT ADDITION END
		}
	}

	if len(envPath) > 0 {
		a.loadResource(envPath)
	}

	for _, mapPath := range mapPaths {
		a.loadResource(mapPath)
	}
}

// APHELION EDIT ADDITION START - FILE DROP

// loadDroppedPaths opens dropped .dme/.dmm/.tgm files, environments first.
// Maps dropped with an environment open after that environment is installed.
func (a *app) loadDroppedPaths(paths []string) {
	ordered := envresolve.OrderDropped(paths)
	if len(ordered) == 0 {
		return
	}
	if !strings.EqualFold(filepath.Ext(ordered[0]), ".dme") {
		for _, path := range ordered {
			a.loadResource(path)
		}
		return
	}
	environmentPath, err := filepath.Abs(ordered[0])
	if err != nil {
		return
	}
	var maps []string
	for _, path := range ordered[1:] {
		if !strings.EqualFold(filepath.Ext(path), ".dme") {
			maps = append(maps, path)
		}
	}
	a.loadEnvironmentV(environmentPath, func() {
		for _, path := range maps {
			a.loadResource(path)
		}
	})
}

// APHELION EDIT ADDITION END
