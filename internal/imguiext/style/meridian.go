// APHELION EDIT ADDITION START - MERIDIAN THEME
package style

import (
	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/theme"
)

// classic keeps the inherited named colours so the Classic theme can restore
// them.
var classic = struct {
	gold, goldLighter, goldDarker, red, redLighter, redDarker, green1, green1Lighter, green1Darker, green3 imgui.Vec4
}{ColorGold, ColorGoldLighter, ColorGoldDarker, ColorRed, ColorRedLighter, ColorRedDarker, ColorGreen1, ColorGreen1Lighter, ColorGreen1Darker, ColorGreen3}

var meridian = true

// UsePalette switches the named colours between the Meridian spectrum and
// the inherited palette. Named colours are used as text (gold headings, red
// errors, green modified variables); buttons use separate tints that keep
// their labels readable.
func UsePalette(useMeridian bool) {
	meridian = useMeridian
	if useMeridian {
		p := theme.Meridian
		ColorGold, ColorGoldLighter, ColorGoldDarker = p.Yellow, theme.Mix(p.Yellow, p.Text, 0.3), theme.Mix(p.Yellow, p.Bg0, 0.25)
		ColorRed, ColorRedLighter, ColorRedDarker = p.Red, theme.Mix(p.Red, p.Text, 0.3), theme.Mix(p.Red, p.Bg0, 0.25)
		ColorGreen1, ColorGreen1Lighter, ColorGreen1Darker = p.Green, theme.Mix(p.Green, p.Text, 0.3), theme.Mix(p.Green, p.Bg0, 0.25)
		ColorGreen3 = p.Green
		return
	}
	c := classic
	ColorGold, ColorGoldLighter, ColorGoldDarker = c.gold, c.goldLighter, c.goldDarker
	ColorRed, ColorRedLighter, ColorRedDarker = c.red, c.redLighter, c.redDarker
	ColorGreen1, ColorGreen1Lighter, ColorGreen1Darker = c.green1, c.green1Lighter, c.green1Darker
	ColorGreen3 = c.green3
}

func classicTint(normal, hover, active imgui.Vec4) theme.ButtonTint {
	return theme.ButtonTint{Normal: normal, Hover: hover, Active: active}
}

func buttonGreen() theme.ButtonTint {
	if meridian {
		return theme.Positive
	}
	c := classic
	return classicTint(c.green1, c.green1Lighter, c.green1Darker)
}

func buttonRed() theme.ButtonTint {
	if meridian {
		return theme.Negative
	}
	c := classic
	return classicTint(c.red, c.redLighter, c.redDarker)
}

// buttonGold is the active tool's highlight: the accent under Meridian.
func buttonGold() theme.ButtonTint {
	if meridian {
		return theme.Primary
	}
	c := classic
	return classicTint(c.gold, c.goldLighter, c.goldDarker)
}

// ButtonSelected marks the chosen segment of a toggle group, such as
// Instance / Prefab. Classic keeps its green.
type ButtonSelected struct{}

func (ButtonSelected) NormalColor() imgui.Vec4 { return selectedTint().Normal }
func (ButtonSelected) ActiveColor() imgui.Vec4 { return selectedTint().Active }
func (ButtonSelected) HoverColor() imgui.Vec4  { return selectedTint().Hover }

func selectedTint() theme.ButtonTint {
	if meridian {
		return theme.SelectedButton
	}
	return buttonGreen()
}

// APHELION EDIT ADDITION END
