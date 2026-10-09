package theme

import (
	"sync"
	"sync/atomic"

	"github.com/SpaiR/imgui-go"
)

var selected atomic.Value // string

var (
	listenersMu sync.Mutex
	listeners   []func(meridian bool)
)

// OnChange registers fn to run with the new theme on every Select, so
// packages with fixed colour tables (map overlays) can follow the theme.
func OnChange(fn func(meridian bool)) {
	listenersMu.Lock()
	listeners = append(listeners, fn)
	listenersMu.Unlock()
	fn(IsMeridian())
}

// Select sets the theme by name; unknown or empty names select Meridian.
func Select(name string) {
	if name != NameClassic {
		name = NameMeridian
	}
	selected.Store(name)
	listenersMu.Lock()
	fns := append([]func(bool){}, listeners...)
	listenersMu.Unlock()
	for _, fn := range fns {
		fn(name == NameMeridian)
	}
}

// Selected is the active theme name.
func Selected() string {
	if name, ok := selected.Load().(string); ok {
		return name
	}
	return NameMeridian
}

// IsMeridian reports whether the Meridian theme is active.
func IsMeridian() bool { return Selected() == NameMeridian }

// Current is the palette panels should use for semantic colours.
func Current() Palette { return Meridian }

// ApplyColors sets every imgui colour role from the Meridian palette.
// Selection, hover and focus are accent tints over the surface, as in
// professional editors; full accent is kept for check marks, sliders and the
// docking preview.
func ApplyColors(s imgui.Style) {
	p := Meridian
	tint := func(base imgui.Vec4, t float32) imgui.Vec4 { return Mix(base, p.Accent, t) }
	set := s.SetColor

	set(imgui.StyleColorText, p.Text)
	set(imgui.StyleColorTextDisabled, p.TextFaint)
	set(imgui.StyleColorWindowBg, p.Bg0)
	set(imgui.StyleColorChildBg, imgui.Vec4{})
	set(imgui.StyleColorPopupBg, Alpha(p.Bg1, 0.98))
	set(imgui.StyleColorBorder, p.Line)
	set(imgui.StyleColorBorderShadow, imgui.Vec4{})

	set(imgui.StyleColorFrameBg, p.Bg2)
	set(imgui.StyleColorFrameBgHovered, p.Bg3)
	set(imgui.StyleColorFrameBgActive, tint(p.Bg3, 0.14))

	set(imgui.StyleColorTitleBg, p.Void)
	set(imgui.StyleColorTitleBgActive, p.Bg1)
	set(imgui.StyleColorTitleBgCollapsed, Alpha(p.Void, 0.75))
	set(imgui.StyleColorMenuBarBg, p.Void)

	set(imgui.StyleColorScrollbarBg, imgui.Vec4{})
	set(imgui.StyleColorScrollbarGrab, p.LineStrong)
	set(imgui.StyleColorScrollbarGrabHovered, p.TextGhost)
	set(imgui.StyleColorScrollbarGrabActive, p.TextFaint)

	set(imgui.StyleColorCheckMark, p.Accent)
	set(imgui.StyleColorSliderGrab, Mix(p.Accent, p.Bg3, 0.25))
	set(imgui.StyleColorSliderGrabActive, p.Accent)

	set(imgui.StyleColorButton, p.Bg3)
	set(imgui.StyleColorButtonHovered, p.LineStrong)
	set(imgui.StyleColorButtonActive, tint(p.Bg3, 0.28))

	set(imgui.StyleColorHeader, tint(p.Bg2, 0.16))
	set(imgui.StyleColorHeaderHovered, tint(p.Bg2, 0.24))
	set(imgui.StyleColorHeaderActive, tint(p.Bg2, 0.32))

	set(imgui.StyleColorSeparator, p.Line)
	set(imgui.StyleColorSeparatorHovered, p.LineStrong)
	set(imgui.StyleColorSeparatorActive, p.Accent)

	set(imgui.StyleColorResizeGrip, Alpha(p.LineStrong, 0.5))
	set(imgui.StyleColorResizeGripHovered, p.LineStrong)
	set(imgui.StyleColorResizeGripActive, p.Accent)

	// Inactive tabs sit on the tab bar; the selected tab is raised, and in the
	// focused panel it carries a trace of accent.
	set(imgui.StyleColorTab, p.Void)
	set(imgui.StyleColorTabHovered, p.LineStrong)
	set(imgui.StyleColorTabActive, tint(p.Bg3, 0.14))
	set(imgui.StyleColorTabUnfocused, p.Void)
	set(imgui.StyleColorTabUnfocusedActive, p.Bg3)

	set(imgui.StyleColorDockingPreview, Alpha(p.Accent, 0.45))
	set(imgui.StyleColorDockingEmptyBg, p.Void)

	set(imgui.StyleColorPlotLines, p.Accent)
	set(imgui.StyleColorPlotLinesHovered, p.Orange)
	set(imgui.StyleColorPlotHistogram, p.Yellow)
	set(imgui.StyleColorPlotHistogramHovered, p.Orange)

	set(imgui.StyleColorTableHeaderBg, p.Bg2)
	set(imgui.StyleColorTableBorderStrong, p.LineStrong)
	set(imgui.StyleColorTableBorderLight, p.LineSoft)
	set(imgui.StyleColorTableRowBg, imgui.Vec4{})
	set(imgui.StyleColorTableRowBgAlt, Alpha(p.Text, 0.03))

	set(imgui.StyleColorTextSelectedBg, Alpha(p.Accent, 0.35))
	set(imgui.StyleColorDragDropTarget, p.Yellow)
	set(imgui.StyleColorNavHighlight, p.Accent)
	set(imgui.StyleColorNavWindowingHighlight, Alpha(p.Text, 0.7))
	set(imgui.StyleColorNavWindowingDimBg, Alpha(p.Void, 0.5))
	set(imgui.StyleColorModalWindowDimBg, Alpha(p.Void, 0.7))
}

type vec2Metric struct {
	id imgui.StyleVarID
	v  imgui.Vec2
}

type floatMetric struct {
	id imgui.StyleVarID
	v  float32
}

// Spacing follows the site's 4 px grid (--sp-1 .. --sp-3); radii follow
// --radius-1 (2 px, controls) and --radius-2 (3 px, panels); borders are
// hairlines.
var (
	vec2Metrics = []vec2Metric{
		{imgui.StyleVarWindowPadding, imgui.Vec2{X: 8, Y: 8}},
		{imgui.StyleVarFramePadding, imgui.Vec2{X: 6, Y: 4}},
		{imgui.StyleVarItemSpacing, imgui.Vec2{X: 8, Y: 5}},
		{imgui.StyleVarItemInnerSpacing, imgui.Vec2{X: 6, Y: 4}},
		{imgui.StyleVarCellPadding, imgui.Vec2{X: 6, Y: 3}},
	}
	floatMetrics = []floatMetric{
		{imgui.StyleVarIndentSpacing, 16},
		{imgui.StyleVarScrollbarSize, 11},
		{imgui.StyleVarGrabMinSize, 10},
		{imgui.StyleVarWindowRounding, 3},
		{imgui.StyleVarChildRounding, 3},
		{imgui.StyleVarPopupRounding, 3},
		{imgui.StyleVarFrameRounding, 2},
		{imgui.StyleVarGrabRounding, 2},
		{imgui.StyleVarScrollbarRounding, 2},
		{imgui.StyleVarTabRounding, 2},
		{imgui.StyleVarWindowBorderSize, 1},
		{imgui.StyleVarPopupBorderSize, 1},
		{imgui.StyleVarFrameBorderSize, 0},
	}
)

// compactMetrics tighten spacing for small screens (JetBrains-style compact
// mode); radii and borders stay.
var compactMetrics = map[imgui.StyleVarID]imgui.Vec2{
	imgui.StyleVarWindowPadding:    {X: 6, Y: 6},
	imgui.StyleVarFramePadding:     {X: 4, Y: 2},
	imgui.StyleVarItemSpacing:      {X: 6, Y: 3},
	imgui.StyleVarItemInnerSpacing: {X: 4, Y: 3},
	imgui.StyleVarCellPadding:      {X: 4, Y: 1},
}

var compact atomic.Bool

// SetCompact selects the compact density.
func SetCompact(on bool) { compact.Store(on) }

// Compact reports whether the compact density is selected.
func Compact() bool { return compact.Load() }

// borderless metrics are hairlines; they do not grow with the interface scale.
var unscaled = map[imgui.StyleVarID]bool{
	imgui.StyleVarWindowBorderSize: true,
	imgui.StyleVarPopupBorderSize:  true,
	imgui.StyleVarFrameBorderSize:  true,
}

// PushMetrics pushes the Meridian spacing, radii and borders scaled by the
// interface scale and returns how many style vars to pop. Classic pushes
// nothing. Call it at the start of the frame's UI and pop at the end.
func PushMetrics(scale float32) int {
	if !IsMeridian() {
		return 0
	}
	if scale <= 0 {
		scale = 1
	}
	dense := Compact()
	for _, m := range vec2Metrics {
		v := m.v
		if c, ok := compactMetrics[m.id]; ok && dense {
			v = c
		}
		imgui.PushStyleVarVec2(m.id, imgui.Vec2{X: v.X * scale, Y: v.Y * scale})
	}
	for _, m := range floatMetrics {
		v := m.v
		if m.id == imgui.StyleVarIndentSpacing && dense {
			v = 12
		}
		if !unscaled[m.id] {
			v *= scale
		}
		imgui.PushStyleVarFloat(m.id, v)
	}
	return len(vec2Metrics) + len(floatMetrics)
}
