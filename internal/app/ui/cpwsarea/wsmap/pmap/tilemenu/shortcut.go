package tilemenu

import (
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	"github.com/SpaiR/imgui-go"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/shortcut"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func (t *TileMenu) addShortcuts() {
	t.shortcuts.Add(shortcut.Shortcut{
		Name:     "tileMenu#close",
		FirstKey: glfw.KeyEscape,
		// APHELION EDIT CHANGE - SHORTCUT FOCUS - ORIGINAL: Action: t.close,
		Action:    func() { t.close(); imgui.CloseCurrentPopup() },
		IsEnabled: func() bool { return t.opened },
	})
}
