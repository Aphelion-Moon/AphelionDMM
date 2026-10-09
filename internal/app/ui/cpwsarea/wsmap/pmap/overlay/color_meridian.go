// APHELION EDIT ADDITION START - MERIDIAN THEME
package overlay

import (
	"sdmm/internal/aphelion/theme"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

// classicOverlay keeps the inherited overlay colours for the Classic theme.
var classicOverlay = struct {
	addAltBorder, fillAltFill, selectBorder, pick, hideType, selectAdd, selectSubtract, selectIntersect, deleteInstance, deleteAltFill, deleteAltBorder, replace, flickInstance util.Color
}{
	ColorToolAddAltTileBorder, ColorToolFillAltTileFill, ColorToolSelectTileBorder, ColorToolPickInstance, ColorToolHideTypeInstance,
	ColorToolSelectAddBorder, ColorToolSelectSubtractBorder, ColorToolSelectIntersectBorder,
	ColorToolDeleteInstance, ColorToolDeleteAltTileFill, ColorToolDeleteAltTileBorder, ColorToolReplaceInstance, ColorFlickInstance,
}

func init() { theme.OnChange(usePalette) }

// usePalette moves the overlays onto the spectrum under Meridian. Overlays
// sit on map art, so each spectrum colour is lightened until it blends into
// no more sprite pixels than the colour it replaces (probe over 310
// Meridian-Rift DMIs, 677k opaque pixels; docs/design/2026-10-09-ui-review.md).
// White fills and borders stay white: nothing reads better on sprites.
func usePalette(meridian bool) {
	if !meridian {
		c := classicOverlay
		ColorToolAddAltTileBorder, ColorToolFillAltTileFill, ColorToolSelectTileBorder, ColorToolPickInstance = c.addAltBorder, c.fillAltFill, c.selectBorder, c.pick
		ColorToolHideTypeInstance, ColorToolSelectAddBorder, ColorToolSelectSubtractBorder, ColorToolSelectIntersectBorder = c.hideType, c.selectAdd, c.selectSubtract, c.selectIntersect
		ColorToolDeleteInstance, ColorToolDeleteAltTileFill, ColorToolDeleteAltTileBorder = c.deleteInstance, c.deleteAltFill, c.deleteAltBorder
		ColorToolReplaceInstance, ColorFlickInstance = c.replace, c.flickInstance
		return
	}
	p := theme.Meridian
	white := imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}
	green := util.MakeColorFromVec4(theme.Mix(p.Green, white, 0.3))
	red := theme.Mix(p.Red, white, 0.25)
	yellow := theme.Mix(p.Yellow, white, 0.2)
	cyan := util.MakeColorFromVec4(p.Cyan)

	ColorToolAddAltTileBorder = util.MakeColorFromVec4(yellow)
	ColorToolFillAltTileFill = util.MakeColorFromVec4(theme.Alpha(yellow, 0.25))
	ColorToolSelectTileBorder = green
	ColorToolPickInstance = green
	ColorToolHideTypeInstance = util.MakeColorFromVec4(yellow)
	ColorToolSelectAddBorder = green
	ColorToolSelectSubtractBorder = util.MakeColorFromVec4(red)
	ColorToolSelectIntersectBorder = cyan
	ColorToolDeleteInstance = util.MakeColorFromVec4(red)
	ColorToolDeleteAltTileFill = util.MakeColorFromVec4(theme.Alpha(red, 0.25))
	ColorToolDeleteAltTileBorder = util.MakeColorFromVec4(yellow)
	ColorToolReplaceInstance = green
	ColorFlickInstance = green
}

// APHELION EDIT ADDITION END
