// Package colorvar reads and writes DM colour variables for the variable
// editor's colour picker. It only handles plain hex strings; colour matrices
// (lists) and expressions are left to the text input.
package colorvar

import (
	"fmt"
	"strconv"
	"strings"
)

// Color is one hex colour as a DM string holds it.
type Color struct {
	R, G, B, A uint8
	// HasAlpha keeps the alpha digits of an 8-digit value; Upper keeps the
	// value's letter case so a pick does not restyle the map.
	HasAlpha bool
	Upper    bool
}

// IsColorVar reports whether a variable holds a colour by name: color,
// light_color, bulb_colour, pipe_color, base_lighting_color and the like.
func IsColorVar(name string) bool {
	name = strings.ToLower(name)
	return name == "color" || name == "colour" || strings.HasSuffix(name, "_color") || strings.HasSuffix(name, "_colour")
}

// Parse reads a quoted (or bare) "#rgb", "#rrggbb" or "#rrggbbaa" value.
func Parse(raw string) (Color, bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if !strings.HasPrefix(s, "#") {
		return Color{}, false
	}
	h := s[1:]
	c := Color{A: 255, Upper: strings.ToUpper(h) == h && strings.ToLower(h) != h}
	switch len(h) {
	case 3:
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 6:
	case 8:
		c.HasAlpha = true
	default:
		return Color{}, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Color{}, false
	}
	if c.HasAlpha {
		c.R, c.G, c.B, c.A = uint8(n>>24), uint8(n>>16), uint8(n>>8), uint8(n)
	} else {
		c.R, c.G, c.B = uint8(n>>16), uint8(n>>8), uint8(n)
	}
	return c, true
}

// Format writes c as a quoted DM hex string.
func Format(c Color) string {
	s := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	if c.HasAlpha {
		s += fmt.Sprintf("%02x", c.A)
	}
	if c.Upper {
		s = strings.ToUpper(s)
	}
	return `"` + s + `"`
}

// FromFloats builds a colour from 0..1 channels, keeping style's alpha and case.
func FromFloats(rgb [3]float32, style Color) Color {
	conv := func(v float32) uint8 {
		v = min(1, max(0, v))
		return uint8(v*255 + 0.5)
	}
	style.R, style.G, style.B = conv(rgb[0]), conv(rgb[1]), conv(rgb[2])
	return style
}

// Floats returns the 0..1 channels of c.
func (c Color) Floats() [3]float32 {
	return [3]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}
}

// Same reports whether two colours have equal channels.
func (c Color) Same(o Color) bool {
	return c.R == o.R && c.G == o.G && c.B == o.B && (!c.HasAlpha && !o.HasAlpha || c.A == o.A)
}

// BasicColors is the 48-swatch "Basic colors" grid of the Windows colour
// dialog (Paint's Edit Colors), row by row, eight per row.
var BasicColors = [48]Color{
	rgb(0xFF8080), rgb(0xFFFF80), rgb(0x80FF80), rgb(0x00FF80), rgb(0x80FFFF), rgb(0x0080FF), rgb(0xFF80C0), rgb(0xFF80FF),
	rgb(0xFF0000), rgb(0xFFFF00), rgb(0x80FF00), rgb(0x00FF40), rgb(0x00FFFF), rgb(0x0080C0), rgb(0x8080C0), rgb(0xFF00FF),
	rgb(0x804040), rgb(0xFF8040), rgb(0x00FF00), rgb(0x008080), rgb(0x004080), rgb(0x8080FF), rgb(0x800040), rgb(0xFF0080),
	rgb(0x800000), rgb(0xFF8000), rgb(0x008000), rgb(0x008040), rgb(0x0000FF), rgb(0x0000A0), rgb(0x800080), rgb(0x8000FF),
	rgb(0x400000), rgb(0x804000), rgb(0x004000), rgb(0x004040), rgb(0x000080), rgb(0x000040), rgb(0x400040), rgb(0x400080),
	rgb(0x000000), rgb(0x808000), rgb(0x808040), rgb(0x808080), rgb(0x408080), rgb(0xC0C0C0), rgb(0x400040), rgb(0xFFFFFF),
}

func rgb(n uint32) Color { return Color{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 255} }

// MaxRecent is the number of "Custom colors" slots, as in the Windows dialog.
const MaxRecent = 16

// Remember puts value first in the recent list, without duplicates, keeping
// at most MaxRecent entries. Values are quoted DM strings.
func Remember(recent []string, value string) []string {
	picked, ok := Parse(value)
	if !ok {
		return recent
	}
	out := []string{value}
	for _, old := range recent {
		if c, ok := Parse(old); ok && !c.Same(picked) && len(out) < MaxRecent {
			out = append(out, old)
		}
	}
	return out
}
