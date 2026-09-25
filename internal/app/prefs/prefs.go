package prefs

// APHELION EDIT ADDITION START - EDITABLE SHORTCUTS
import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/hotkeys"
)

// APHELION EDIT ADDITION END

type Prefs struct {
	Editor      Editor
	Controls    Controls
	Interface   Interface
	Application Application
	// APHELION EDIT ADDITION START - EDITABLE SHORTCUTS
	Shortcuts *hotkeys.Settings
	Mapper    *editing.MapperSettings
	// APHELION EDIT ADDITION END
}

type Interface struct {
	Scale int
	Fps   int
}

type Controls struct {
	AltScrollBehaviour   bool
	QuickEditContextMenu bool
	QuickEditMapPane     bool
}

type Editor struct {
	SaveFormat        string
	CodeEditor        string
	NudgeMode         string
	SanitizeVariables bool
	// APHELION EDIT ADDITION START - SELECTION GRID STEP
	SelectionMoveStep int
	// APHELION EDIT ADDITION END
}

type Application struct {
	CheckForUpdates bool
	AutoUpdate      bool
}
