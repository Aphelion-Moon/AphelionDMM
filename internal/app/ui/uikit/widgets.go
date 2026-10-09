package uikit

import (
	"strings"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/theme"
	"sdmm/internal/app/window"
	"sdmm/internal/imguiext/style"
)

// Mono draws body in the monospace face (IBM Plex Mono) under the Meridian
// theme; Classic keeps the inherited single font.
func Mono(body func()) {
	font, ok := window.MonoFont()
	if !theme.IsMeridian() || !ok {
		body()
		return
	}
	imgui.PushFont(font)
	body()
	imgui.PopFont()
}

// MonoText draws one line of monospace secondary text.
func MonoText(text string) { Mono(func() { imgui.TextDisabled(text) }) }

// tracking is the site's --tracking-wider (0.14em) for micro labels.
const tracking = 0.14

// SectionLabel draws a section heading the way the site does: small,
// uppercase, widely tracked and faint. Classic keeps a plain gold label.
func SectionLabel(text string) {
	micro, ok := window.MicroFont()
	if !theme.IsMeridian() || !ok {
		imgui.TextColored(style.ColorGold, text) // the inherited section colour
		return
	}
	imgui.PushFont(micro)
	defer imgui.PopFont()
	upper := strings.ToUpper(text)
	spacing := imgui.FontSize() * tracking
	pos := imgui.CursorScreenPos()
	color := imgui.PackedColorFromVec4(imgui.CurrentStyle().Color(imgui.StyleColorTextDisabled))
	list := imgui.WindowDrawList()
	x := pos.X
	for _, r := range upper {
		glyph := string(r)
		list.AddText(imgui.Vec2{X: x, Y: pos.Y}, color, glyph)
		x += imgui.CalcTextSize(glyph, false, 0).X + spacing
	}
	// Reserve the drawn area, with a little air above the section.
	imgui.Dummy(imgui.Vec2{X: x - pos.X, Y: imgui.FontSize() + 2})
}

// EmptyState is the one pattern for an empty panel: secondary text that
// names the next action, wrapped to the panel.
func EmptyState(text string) {
	imgui.Spacing()
	imgui.PushTextWrapPos()
	imgui.TextDisabled(text)
	imgui.PopTextWrapPos()
}

// TruncatedMono draws a type path in monospace, middle-truncated to the
// available width, with the full path as a tooltip when shortened.
func TruncatedMono(path string, width float32) {
	Mono(func() {
		shown := MiddleTruncate(path, width, func(s string) float32 { return imgui.CalcTextSize(s, false, 0).X })
		imgui.TextDisabled(shown)
		if shown != path && imgui.IsItemHovered() {
			imgui.SetTooltip(path)
		}
	})
}
