package window

import (
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/rsc"

	"github.com/SpaiR/imgui-go"
)

const (
	fontSizeH1 = 32
	fontSizeH2 = 24
	fontSizeH3 = 19
	fontSizeH4 = 16
)

var (
	FontDefault imgui.Font

	FontH1 imgui.Font
	FontH2 imgui.Font
	FontH3 imgui.Font
)

func configureFonts() {
	/* APHELION EDIT REMOVAL START - FONT METRICS
	fontConfig := imgui.NewFontConfig()
	defer fontConfig.Delete()
	APHELION EDIT REMOVAL END */

	fontAtlas := imgui.CurrentIO().Fonts()
	fontAtlas.Clear()

	// APHELION EDIT ADDITION START - FONT METRICS
	// A shared config leaked the icon merge settings (GlyphMaxAdvanceX of the
	// previous, smaller size) into the next base font, condensing FontH1.
	createFont := func(size float32, atlas imgui.FontAtlas) imgui.Font {
		config := imgui.NewFontConfig()
		defer config.Delete()
		return createFont(size, atlas, config)
	}
	// APHELION EDIT ADDITION END

	// APHELION EDIT CHANGE - FONT METRICS - ORIGINAL: FontDefault = createFont(fontSizeH4, fontAtlas, fontConfig)
	FontDefault = createFont(fontSizeH4, fontAtlas)

	// APHELION EDIT CHANGE - FONT METRICS - ORIGINAL: FontH1 = createFont(fontSizeH1, fontAtlas, fontConfig)
	FontH1 = createFont(fontSizeH1, fontAtlas)
	// APHELION EDIT CHANGE - FONT METRICS - ORIGINAL: FontH2 = createFont(fontSizeH2, fontAtlas, fontConfig)
	FontH2 = createFont(fontSizeH2, fontAtlas)
	// APHELION EDIT CHANGE - FONT METRICS - ORIGINAL: FontH3 = createFont(fontSizeH3, fontAtlas, fontConfig)
	FontH3 = createFont(fontSizeH3, fontAtlas)

	imgui.CurrentIO().SetFontDefault(FontDefault)
}

func createFont(size float32, atlas imgui.FontAtlas, config imgui.FontConfig) (font imgui.Font) {
	fontSize := size * pointSize

	// APHELION EDIT ADDITION START - FONT METRICS
	// General punctuation (em dash, curly quotes, ellipsis) and arrows appear in
	// release notes and UI text.
	textGlyphs := imgui.GlyphRangesBuilder{}
	textGlyphs.AddExisting(atlas.GlyphRangesCyrillic())
	textGlyphs.Add(0x2010, 0x2027)
	textGlyphs.Add(0x2190, 0x2193)
	textRanges := textGlyphs.Build()
	// APHELION EDIT ADDITION END

	font = atlas.AddFontFromMemoryTTFV(
		rsc.FontTTF(),
		fontSize,
		config,
		// APHELION EDIT CHANGE - FONT METRICS - ORIGINAL: atlas.GlyphRangesCyrillic(),
		textRanges.GlyphRanges,
	)

	config.SetMergeMode(true)
	config.SetPixelSnapH(true)
	config.SetGlyphOffsetY(2)
	config.SetGlyphMaxAdvanceX(fontSize)

	glyphsBuilder := imgui.GlyphRangesBuilder{}
	glyphsBuilder.Add(icon.RangeMin, icon.RangeMax)

	atlas.AddFontFromMemoryTTFV(
		rsc.FontIconsTTF(),
		fontSize,
		config,
		glyphsBuilder.Build().GlyphRanges,
	)

	config.SetMergeMode(false)

	return font
}
