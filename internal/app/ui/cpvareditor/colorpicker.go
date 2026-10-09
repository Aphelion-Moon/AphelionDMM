// APHELION EDIT ADDITION START - COLOR PICKER
package cpvareditor

import (
	"fmt"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/colorvar"
	"sdmm/internal/app/ui/uikit"
	"sdmm/internal/dmapi/dmvars"
)

// colorPick is the open colour dialog: the variable it edits and the colour
// chosen so far. Nothing is written until OK.
type colorPick struct {
	varName string
	current [3]float32
	style   colorvar.Color // alpha digits and letter case of the original value
}

const colorPopup = "Edit Colors##vareditor_color"

// showColorSwatch draws the variable's colour; a click opens a Paint-style
// colour dialog. Values that are not hex strings (null, matrices) show an
// empty swatch, and a pick replaces them.
func (v *VarEditor) showColorSwatch(varName, value string) {
	c, parsed := colorvar.Parse(value)
	swatch := imgui.Vec4{X: 0, Y: 0, Z: 0, W: 0}
	if parsed {
		f := c.Floats()
		swatch = imgui.Vec4{X: f[0], Y: f[1], Z: f[2], W: 1}
	}
	size := imgui.FrameHeight()
	if imgui.ColorButton("##color_swatch_"+varName, swatch, imgui.ColorEditFlagsNoTooltip|imgui.ColorEditFlagsAlphaPreview, imgui.Vec2{X: size, Y: size}) {
		v.colorPick = newColorPick(varName, value)
		imgui.OpenPopup(colorPopup)
	}
	if imgui.IsItemHovered() {
		if parsed || value == dmvars.NullValue {
			imgui.SetTooltip("Pick a colour")
		} else {
			imgui.SetTooltip("Pick a colour. The current value is not a hex colour; a pick replaces it.")
		}
	}
	if v.colorPick.varName == varName {
		v.showColorPopup()
	}
}

func newColorPick(varName, value string) colorPick {
	pick := colorPick{varName: varName, current: [3]float32{1, 1, 1}, style: colorvar.Color{A: 255}}
	if c, ok := colorvar.Parse(value); ok {
		pick.current, pick.style = c.Floats(), c
	}
	return pick
}

// value is the chosen colour as DM source text.
func (p colorPick) value() string { return colorvar.Format(colorvar.FromFloats(p.current, p.style)) }

func (v *VarEditor) showColorPopup() {
	if !imgui.BeginPopup(colorPopup) {
		return
	}
	defer imgui.EndPopup()
	pick := &v.colorPick
	swatch := imgui.FrameHeight() * 0.9

	imgui.BeginGroup()
	uikit.SectionLabel("Basic colors")
	for i, c := range colorvar.BasicColors {
		if i%8 != 0 {
			imgui.SameLine()
		}
		v.colorSwatchButton(fmt.Sprintf("##basic_%d", i), c, swatch)
	}
	imgui.Spacing()
	uikit.SectionLabel("Custom colors")
	recent := v.config().RecentColors
	for i := 0; i < colorvar.MaxRecent; i++ {
		if i%8 != 0 {
			imgui.SameLine()
		}
		if i < len(recent) {
			if c, ok := colorvar.Parse(recent[i]); ok {
				v.colorSwatchButton(fmt.Sprintf("##recent_%d", i), c, swatch)
				continue
			}
		}
		imgui.BeginDisabled()
		imgui.ColorButton(fmt.Sprintf("##recent_empty_%d", i), imgui.Vec4{W: 0}, imgui.ColorEditFlagsNoTooltip|imgui.ColorEditFlagsAlphaPreview, imgui.Vec2{X: swatch, Y: swatch})
		imgui.EndDisabled()
	}
	if imgui.Button("Add to Custom Colors") {
		v.config().RecentColors = colorvar.Remember(v.config().RecentColors, pick.value())
	}
	imgui.EndGroup()

	imgui.SameLine()
	imgui.BeginGroup()
	imgui.PushItemWidth(imgui.FontSize() * 14)
	imgui.ColorPicker3V("##color_picker", &pick.current, imgui.ColorPickerFlagsPickerHueBar|imgui.ColorPickerFlagsNoAlpha)
	imgui.PopItemWidth()
	imgui.TextDisabled(pick.varName + " = " + pick.value())
	if imgui.Button("OK") {
		value := pick.value()
		v.config().RecentColors = colorvar.Remember(v.config().RecentColors, value)
		v.setCurrentVariable(pick.varName, value)
		v.colorPick = colorPick{}
		imgui.CloseCurrentPopup()
	}
	imgui.SameLine()
	if imgui.Button("Cancel") {
		v.colorPick = colorPick{}
		imgui.CloseCurrentPopup()
	}
	imgui.EndGroup()
}

func (v *VarEditor) colorSwatchButton(id string, c colorvar.Color, size float32) {
	f := c.Floats()
	if imgui.ColorButton(id, imgui.Vec4{X: f[0], Y: f[1], Z: f[2], W: 1}, imgui.ColorEditFlagsNone, imgui.Vec2{X: size, Y: size}) {
		v.colorPick.current = f
	}
}

// APHELION EDIT ADDITION END
