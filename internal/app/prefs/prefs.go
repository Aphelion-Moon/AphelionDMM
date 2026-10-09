package prefs

// APHELION EDIT ADDITION START - EDITABLE SHORTCUTS
import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/hotkeys"
	// APHELION EDIT ADDITION START - PATH MIGRATION
	"sdmm/internal/aphelion/repath"
	// APHELION EDIT ADDITION END
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
	// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
	Collaboration Collaboration
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PATH MIGRATION
	PathMigration *repath.Settings
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR

// Collaboration holds local-only collaboration preferences.
type Collaboration struct {
	// CursorColor is the chosen cursor palette index, or nil for the automatic
	// per-actor default. It is shared as ephemeral presence, never durably.
	CursorColor *int
}

// APHELION EDIT ADDITION END

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
	// APHELION EDIT ADDITION START - AREA PRESENTATION
	// AreaOverlayPercent is the alpha (0..100) applied to drawn areas.
	AreaOverlayPercent int
	// HideBaseArea hides the environment's base area from rendering.
	HideBaseArea bool
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - LIGHTING PREVIEW
	Lighting Lighting
	// APHELION EDIT ADDITION END
}

type Application struct {
	CheckForUpdates bool
	AutoUpdate      bool
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	BypassEnvironmentCache bool
	// APHELION EDIT ADDITION END
}

