// Package theme is AphelionDMM's interface theme. The Meridian palette is
// taken from the project website's design tokens (meridian.a13.info, :root
// custom properties, read 2026-10-09 at the user's request): warm near-black
// surfaces, parchment text, hairline lines, a cyan accent and the six-colour
// status spectrum. Classic keeps the inherited StrongDMM theme.
package theme

import "github.com/SpaiR/imgui-go"

// Names of the selectable themes, as stored in preferences.
const (
	NameMeridian = "Meridian"
	NameClassic  = "Classic"
)

// Names lists the themes in the order the preference shows them.
var Names = []string{NameMeridian, NameClassic}

// Palette is the set of semantic colours panels may use.
type Palette struct {
	// Surfaces, darkest to lightest (--bg-void, --bg-0 .. --bg-3).
	Void, Bg0, Bg1, Bg2, Bg3 imgui.Vec4
	// Lines (--line-soft, --line, --line-strong).
	LineSoft, Line, LineStrong imgui.Vec4
	// Text, most to least prominent (--text, --text-dim, --text-faint, --text-ghost).
	Text, TextDim, TextFaint, TextGhost imgui.Vec4
	// Accent (--accent = --spec-cyan) and the text drawn on it (--accent-ink).
	Accent, AccentInk imgui.Vec4
	// Spectrum (--spec-*): status and category colours.
	Cyan, Green, Yellow, Orange, Red, Magenta imgui.Vec4
}

// Meridian is the website palette.
var Meridian = Palette{
	Void: hex(0x0e0c0b), Bg0: hex(0x131110), Bg1: hex(0x1a1714), Bg2: hex(0x211d19), Bg3: hex(0x292520),
	LineSoft: hex(0x241f1b), Line: hex(0x2e2a25), LineStrong: hex(0x453f36),
	Text: hex(0xece5d8), TextDim: hex(0xa89f90), TextFaint: hex(0x8f887c), TextGhost: hex(0x5c574e),
	Accent: hex(0x56d4dc), AccentInk: hex(0x0d1516),
	Cyan: hex(0x56d4dc), Green: hex(0x7bc86f), Yellow: hex(0xe5c25b), Orange: hex(0xe0863f), Red: hex(0xd95f4c), Magenta: hex(0xc06bb4),
}

// Spectrum returns the status colours in the site's gradient order.
func (p Palette) Spectrum() []imgui.Vec4 {
	return []imgui.Vec4{p.Cyan, p.Green, p.Yellow, p.Orange, p.Red, p.Magenta}
}

func hex(n uint32) imgui.Vec4 {
	return imgui.Vec4{X: float32(n>>16&0xff) / 255, Y: float32(n>>8&0xff) / 255, Z: float32(n&0xff) / 255, W: 1}
}

// Mix moves a toward b by t (0..1), keeping a's alpha.
func Mix(a, b imgui.Vec4, t float32) imgui.Vec4 {
	return imgui.Vec4{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t, Z: a.Z + (b.Z-a.Z)*t, W: a.W}
}

// Alpha returns c with alpha a.
func Alpha(c imgui.Vec4, a float32) imgui.Vec4 { c.W = a; return c }
