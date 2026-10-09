package menu

import (
	// APHELION EDIT ADDITION START - IN-GAME LOOK
	"sdmm/internal/aphelion/ingame"
	"sdmm/internal/aphelion/lighting/maplight"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/platform"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func (m *Menu) addShortcuts() {
	// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
	m.shortcuts.Add(shortcut.Shortcut{Name: "menu#showHotkeys", FirstKey: glfw.KeyF1, Action: m.openShortcutReference})
	// APHELION EDIT ADDITION END
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoNewWorkspace",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyN,
		Action:      m.app.DoNewWorkspace,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoOpen",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyO,
		Action:      m.app.DoOpen,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoSave",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyS,
		Action:      m.app.DoSave,
		IsEnabled:   m.app.HasActiveMap,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:         "menu#DoSaveAll",
		FirstKey:     platform.KeyModLeft(),
		FirstKeyAlt:  platform.KeyModRight(),
		SecondKey:    glfw.KeyLeftShift,
		SecondKeyAlt: glfw.KeyRightShift,
		ThirdKey:     glfw.KeyS,
		Action:       m.app.DoSaveAll,
		IsEnabled:    m.app.HasActiveMap,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoClose",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyW,
		Action:      m.app.DoClose,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:         "menu#DoCloseAll",
		FirstKey:     platform.KeyModLeft(),
		FirstKeyAlt:  platform.KeyModRight(),
		SecondKey:    glfw.KeyLeftShift,
		SecondKeyAlt: glfw.KeyRightShift,
		ThirdKey:     glfw.KeyW,
		Action:       m.app.DoCloseAll,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoExit",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyQ,
		Action:      m.app.DoExit,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoUndo",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyZ,
		Action:      m.app.DoUndo,
		IsEnabled:   m.app.CommandStorage().HasUndo,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:         "menu#DoRedo",
		FirstKey:     platform.KeyModLeft(),
		FirstKeyAlt:  platform.KeyModRight(),
		SecondKey:    glfw.KeyLeftShift,
		SecondKeyAlt: glfw.KeyRightShift,
		ThirdKey:     glfw.KeyZ,
		Action:       m.app.DoRedo,
		IsEnabled:    m.app.CommandStorage().HasRedo,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoRedo",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyY,
		Action:      m.app.DoRedo,
		IsEnabled:   m.app.CommandStorage().HasRedo,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoCopy",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyC,
		Action:      m.app.DoCopy,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoPaste",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyV,
		Action:      m.app.DoPaste,
		IsEnabled:   m.app.Clipboard().HasData,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoCut",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyX,
		Action:      m.app.DoCut,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:     "menu#DoDelete",
		FirstKey: glfw.KeyDelete,
		Action:   m.app.DoDelete,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoSearch",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyF,
		Action:      m.app.DoSearch,
		IsEnabled:   m.app.HasActiveMap,
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#DoOpenJumpWindow",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyG,
		Action:      m.app.DoOpenJumpWindow,
		IsEnabled:   m.app.HasActiveMap,
	})

	m.shortcuts.Add(shortcut.Shortcut{
		Name:         "menu#DoMultiZRendering",
		FirstKey:     platform.KeyModLeft(),
		FirstKeyAlt:  platform.KeyModRight(),
		SecondKey:    glfw.Key0,
		SecondKeyAlt: glfw.KeyKP0,
		Action:       m.app.DoMultiZRendering,
	})

	// APHELION EDIT ADDITION START - IN-GAME LOOK
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#ToggleLightingPreview",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyL,
		Action:      func() { maplight.SetEnabled(!maplight.Current().Enabled) },
	})
	m.shortcuts.Add(shortcut.Shortcut{
		Name:        "menu#ToggleInGameLook",
		FirstKey:    platform.KeyModLeft(),
		FirstKeyAlt: platform.KeyModRight(),
		SecondKey:   glfw.KeyI,
		Action:      func() { ingame.SetEnabled(!ingame.Enabled()) },
	})
	// APHELION EDIT ADDITION END

	m.shortcuts.Add(shortcut.Shortcut{
		Name:     "menu#DoResetLayout",
		FirstKey: glfw.KeyF5,
		Action:   m.app.DoResetLayout,
	})

	m.shortcuts.SetVisible(true)
}
