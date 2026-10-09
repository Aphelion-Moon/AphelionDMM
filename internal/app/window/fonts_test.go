package window

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
)

// Each header font must keep its own glyph metrics. A shared FontConfig used
// to carry the previous (smaller) font's GlyphMaxAdvanceX into FontH1, which
// squeezed changelog headers together.
func TestHeaderFontsKeepTheirOwnAdvance(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 700})
	configureFonts()
	io.Fonts().TextureDataRGBA32()
	imgui.NewFrame()
	defer imgui.EndFrame()

	width := func(font imgui.Font, text string) float32 {
		imgui.PushFont(font)
		defer imgui.PopFont()
		return imgui.CalcTextSize(text, false, -1).X
	}
	const sample = "WWWWWWWW"
	h1, def := width(FontH1, sample), width(FontDefault, sample)
	if want := def * fontSizeH1 / fontSizeH4 * 0.9; h1 < want {
		t.Fatalf("H1 is condensed: %q is %.1fpx, want at least %.1fpx (default %.1fpx)", sample, h1, want, def)
	}
	// The em dash in release headers must be a real glyph, not the fallback.
	dash, fallback := width(FontH1, "—"), width(FontH1, "￿")
	if dash == fallback {
		t.Fatalf("em dash renders as the fallback glyph (%.1fpx)", dash)
	}
}
