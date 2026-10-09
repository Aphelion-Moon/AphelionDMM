// APHELION EDIT ADDITION START - COLLABORATION
package pmap

import (
	"image/color"
	"time"

	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/dmapi/dmmap"

	"github.com/SpaiR/imgui-go"
)

const (
	collaborationPresenceCap     = 64
	collaborationPresenceTimeout = collabui.PresenceTimeout
	collaborationPresenceWidth   = 2

	presenceBadgeMaxLabelRunes = 24
	presenceBadgeGap           = 2
	presenceTileFillAlpha      = 36
)

// The palette lives in collab/ui so the panel picker and this overlay agree.
// Markers are outlined in black so every entry stays visible on light and dark tiles.
var collaborationPresenceOutline = imgui.Packed(color.RGBA{A: 255})

func (p *PaneMap) showCollaborationPresence() {
	overlays := collabui.BuildPresenceOverlays(
		p.app.CollaborationPresence(),
		p.activeLevel,
		dmmap.WorldIconSize,
		collaborationPresenceCap,
		collaborationPresenceTimeout,
		time.Now(),
	)
	if len(overlays) == 0 {
		return
	}

	drawList := imgui.WindowDrawList()
	drawList.PushClipRect(p.canvasControl.PosMin(), p.canvasControl.PosMax())
	defer drawList.PopClipRect()
	camera := p.canvas.Render().Camera
	for _, overlay := range overlays {
		min, max := presenceScreenBounds(overlay, dmmap.WorldIconSize, camera.Scale, camera.ShiftX, camera.ShiftY, p.canvasControl.PosMin(), p.canvasControl.PosMax())
		fill := collabui.PresenceSlotColor(overlay.StyleSlot)
		styleColor := imgui.Packed(fill)
		// Keep the faint tile highlight so the exact tile is still readable.
		tint := fill
		tint.A = presenceTileFillAlpha
		drawList.AddRectFilled(min, max, imgui.Packed(tint))
		drawList.AddRectV(min, max, styleColor, 0, imgui.DrawFlagsNone, collaborationPresenceWidth)
		if overlay.Selection != nil {
			selectionMin, selectionMax := presenceSelectionScreenBounds(*overlay.Selection, camera.Scale, camera.ShiftX, camera.ShiftY, p.canvasControl.PosMin(), p.canvasControl.PosMax())
			drawList.AddRectV(selectionMin, selectionMax, styleColor, 0, imgui.DrawFlagsNone, collaborationPresenceWidth)
		}
		// Presence is tile-granular: the highlighted tile is the cursor, so no
		// pointer is drawn that would suggest a sub-tile position.
		drawPresenceBadge(drawList, presenceBadgeAnchor(max), overlay, fill)
	}
}

// presenceBadgeAnchor places the badge just off the tile's lower-right corner.
func presenceBadgeAnchor(tileMax imgui.Vec2) imgui.Vec2 {
	return tileMax.Plus(imgui.Vec2{X: presenceBadgeGap, Y: presenceBadgeGap})
}

// presenceBadgeText is the badge caption: initials, then the bounded name.
func presenceBadgeText(overlay collabui.PresenceOverlay) string {
	label := []rune(overlay.Label)
	if len(label) > presenceBadgeMaxLabelRunes {
		label = append(label[:presenceBadgeMaxLabelRunes-3], []rune("...")...)
	}
	return overlay.Initials + "  " + string(label)
}

func drawPresenceBadge(drawList imgui.DrawList, anchor imgui.Vec2, overlay collabui.PresenceOverlay, fill color.RGBA) {
	text := presenceBadgeText(overlay)
	size := imgui.CalcTextSize(text, false, 0)
	padding := imgui.Vec2{X: 5, Y: 2}
	min := anchor
	max := min.Plus(size).Plus(padding).Plus(padding)
	drawList.AddRectFilledV(min, max, imgui.Packed(fill), 4, imgui.DrawFlagsRoundCornersAll)
	drawList.AddRectV(min, max, collaborationPresenceOutline, 4, imgui.DrawFlagsRoundCornersAll, 1)
	drawList.AddText(min.Plus(padding), imgui.Packed(collabui.PresenceTextColor(fill)), text)
}

func presenceSelectionScreenBounds(selection collabui.PresenceSelectionOverlay, scale, shiftX, shiftY float32, canvasMin, canvasMax imgui.Vec2) (imgui.Vec2, imgui.Vec2) {
	return imgui.Vec2{
			X: canvasMin.X + (float32(selection.PixelX1)+shiftX)*scale,
			Y: canvasMax.Y - (float32(selection.PixelY2)+shiftY)*scale,
		}, imgui.Vec2{
			X: canvasMin.X + (float32(selection.PixelX2)+shiftX)*scale,
			Y: canvasMax.Y - (float32(selection.PixelY1)+shiftY)*scale,
		}
}

func presenceScreenBounds(overlay collabui.PresenceOverlay, iconSize int, scale, shiftX, shiftY float32, canvasMin, canvasMax imgui.Vec2) (imgui.Vec2, imgui.Vec2) {
	worldX := float32(overlay.PixelX)
	worldY := float32(overlay.PixelY)
	scaledIconSize := float32(iconSize) * scale
	min := imgui.Vec2{
		X: canvasMin.X + (worldX+shiftX)*scale,
		Y: canvasMax.Y - (worldY+shiftY)*scale - scaledIconSize,
	}
	return min, min.Plus(imgui.Vec2{X: scaledIconSize, Y: scaledIconSize})
}

// APHELION EDIT ADDITION END
