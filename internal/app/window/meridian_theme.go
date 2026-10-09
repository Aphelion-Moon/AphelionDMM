// APHELION EDIT ADDITION START - MERIDIAN THEME
package window

import (
	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/theme"
	"sdmm/internal/imguiext/style"
)

// SetTheme selects the interface theme by name (theme.Names) and applies it
// at once when the UI exists; otherwise setupImGui applies it.
func SetTheme(name string) {
	theme.Select(name)
	if _, err := imgui.CurrentContext(); err == nil {
		applyTheme()
	}
}

func applyTheme() {
	if theme.IsMeridian() {
		imgui.StyleColorsDark()
		theme.ApplyColors(imgui.CurrentStyle())
		style.UsePalette(true)
		return
	}
	(*Window)(nil).setDefaultTheme()
	style.UsePalette(false)
}

// PushThemeMetrics pushes the theme's spacing and radii for this frame's UI
// at the current interface scale and returns how many to pop.
func PushThemeMetrics() int { return theme.PushMetrics(pointSize) }

// APHELION EDIT ADDITION END
