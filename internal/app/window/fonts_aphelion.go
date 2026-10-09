// APHELION EDIT ADDITION START - MERIDIAN THEME
package window

import (
	"github.com/SpaiR/imgui-go"

	"sdmm/internal/imguiext/icon"
	"sdmm/internal/rsc"
)

const (
	// Same size as the UI font: Plex Mono's lighter letterforms read small at
	// 15 px beside Inter Medium (checked in the theme preview).
	fontSizeMono = 16
	// Section labels (the site's --fs-micro, 11 px).
	fontSizeMicro = 11
)

var (
	// FontMono is IBM Plex Mono, for type paths, variable values and keys.
	FontMono imgui.Font
	// FontMicro is small Inter for uppercase section labels.
	FontMicro imgui.Font
)

// fontsOwner is the context and atlas FontMono and FontMicro were built in.
// A font handle from another (destroyed) context must never be pushed.
var fontsOwner struct {
	context imgui.Context // compared by handle; CurrentContext wraps anew
	atlas   imgui.FontAtlas
}

// atlasMarker tags the atlas that holds FontMono and FontMicro. A destroyed
// context's memory can be reused for a new context and atlas, so handle
// equality alone can pass for a foreign atlas; a fresh atlas starts with
// zero builder flags. The flags are only read by the FreeType builder, which
// this application does not use (fonts build with stb_truetype).
const atlasMarker = 1 << 30

func fontsCurrent() bool {
	context, err := imgui.CurrentContext()
	if err != nil || *context != fontsOwner.context {
		return false
	}
	atlas := imgui.CurrentIO().Fonts()
	return atlas == fontsOwner.atlas && atlas.FontBuilderFlags()&atlasMarker != 0
}

// MonoFont returns FontMono when it belongs to the current UI context.
func MonoFont() (imgui.Font, bool) { return FontMono, FontMono != 0 && fontsCurrent() }

// MicroFont returns FontMicro when it belongs to the current UI context.
func MicroFont() (imgui.Font, bool) { return FontMicro, FontMicro != 0 && fontsCurrent() }

func createAphelionFonts(atlas imgui.FontAtlas) {
	if context, err := imgui.CurrentContext(); err == nil {
		fontsOwner.context, fontsOwner.atlas = *context, atlas
		atlas.SetFontBuilderFlags(atlas.FontBuilderFlags() | atlasMarker)
	}
	mono := imgui.NewFontConfig()
	defer mono.Delete()
	ranges := imgui.GlyphRangesBuilder{}
	ranges.AddExisting(atlas.GlyphRangesCyrillic())
	ranges.Add(0x2010, 0x2027) // dashes, quotes and the truncation ellipsis
	FontMono = atlas.AddFontFromMemoryTTFV(rsc.FontMonoTTF(), fontSizeMono*pointSize, mono, ranges.Build().GlyphRanges)
	// Icons merge in as for the UI font, so buttons inside monospace fields
	// (the variable reset) keep their glyphs.
	icons := imgui.NewFontConfig()
	defer icons.Delete()
	icons.SetMergeMode(true)
	icons.SetPixelSnapH(true)
	icons.SetGlyphOffsetY(2)
	icons.SetGlyphMaxAdvanceX(fontSizeMono * pointSize)
	iconRanges := imgui.GlyphRangesBuilder{}
	iconRanges.Add(icon.RangeMin, icon.RangeMax)
	atlas.AddFontFromMemoryTTFV(rsc.FontIconsTTF(), fontSizeMono*pointSize, icons, iconRanges.Build().GlyphRanges)

	micro := imgui.NewFontConfig()
	defer micro.Delete()
	FontMicro = atlas.AddFontFromMemoryTTFV(rsc.FontTTF(), fontSizeMicro*pointSize, micro, atlas.GlyphRangesDefault())
}

// APHELION EDIT ADDITION END
