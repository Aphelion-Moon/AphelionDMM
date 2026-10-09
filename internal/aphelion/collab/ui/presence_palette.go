package ui

import (
	"image/color"
	"math"
)

// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR

// PresencePaletteEntry is one selectable cursor colour. The palette is a
// programmatic set (no image or art assets); entries are drawn with a dark
// outline so they remain visible on both light and dark map tiles. The first
// seven entries follow the Okabe-Ito colour-blind-safe set.
type PresencePaletteEntry struct {
	Name  string
	Color color.RGBA
}

// PresencePalette is indexed by the protocol cursor_color value. Its length must
// equal protocol.CursorColorPaletteSize; a test enforces this.
var PresencePalette = [...]PresencePaletteEntry{
	{"Orange", color.RGBA{R: 230, G: 159, B: 0, A: 255}},
	{"Sky blue", color.RGBA{R: 86, G: 180, B: 233, A: 255}},
	{"Green", color.RGBA{R: 0, G: 158, B: 115, A: 255}},
	{"Yellow", color.RGBA{R: 240, G: 228, B: 66, A: 255}},
	{"Blue", color.RGBA{R: 0, G: 114, B: 178, A: 255}},
	{"Vermilion", color.RGBA{R: 213, G: 94, B: 0, A: 255}},
	{"Pink", color.RGBA{R: 204, G: 121, B: 167, A: 255}},
	{"White", color.RGBA{R: 240, G: 240, B: 240, A: 255}},
	{"Violet", color.RGBA{R: 160, G: 110, B: 230, A: 255}},
	{"Lime", color.RGBA{R: 150, G: 225, B: 60, A: 255}},
	{"Cyan", color.RGBA{R: 40, G: 225, B: 225, A: 255}},
	{"Red", color.RGBA{R: 225, G: 50, B: 50, A: 255}},
}

// PresenceSlotColor returns the palette colour for an overlay StyleSlot.
func PresenceSlotColor(slot uint32) color.RGBA {
	return PresencePalette[int(slot)%len(PresencePalette)].Color
}

// PresenceTextColor returns black or white, whichever reads better on fill.
func PresenceTextColor(fill color.RGBA) color.RGBA {
	if luminance(fill) > 0.179 {
		return color.RGBA{A: 255}
	}
	return color.RGBA{R: 255, G: 255, B: 255, A: 255}
}

func luminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// APHELION EDIT ADDITION END
