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
	presencePointerFillAlpha   = 235
	presenceTileFillAlpha      = 36
)

// The palette lives in collab/ui so the panel picker and this overlay agree.
// Markers are outlined in black so every entry stays visible on light and dark tiles.
var collaborationPresenceOutline = imgui.Packed(color.RGBA{A: 255})

// presencePointerTriangles triangulates presencePointerPolygon for filling.
var presencePointerTriangles = [5][3]int{{0, 1, 2}, {0, 2, 5}, {0, 5, 6}, {2, 3, 4}, {2, 4, 5}}

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
		tip := min.Plus(max).Times(0.5)
		drawPresencePointer(drawList, tip, fill)
		drawPresenceBadge(drawList, tip.Plus(imgui.Vec2{X: 14, Y: 16}), overlay, fill)
	}
}

// presencePointerPolygon returns the arrow outline with vertex 0 at tip.
// Offsets are in screen pixels so the marker stays legible at any map zoom.
func presencePointerPolygon(tip imgui.Vec2, scale float32) [7]imgui.Vec2 {
	offsets := [7]imgui.Vec2{{X: 0, Y: 0}, {X: 0, Y: 16}, {X: 4, Y: 12.4}, {X: 7, Y: 18.5}, {X: 9.6, Y: 17.3}, {X: 6.6, Y: 11.3}, {X: 11.5, Y: 11.3}}
	var vertices [7]imgui.Vec2
	for index, offset := range offsets {
		vertices[index] = tip.Plus(imgui.Vec2{X: offset.X * scale, Y: offset.Y * scale})
	}
	return vertices
}

// presenceBadgeText is the badge caption: initials, then the bounded name.
func presenceBadgeText(overlay collabui.PresenceOverlay) string {
	label := []rune(overlay.Label)
	if len(label) > presenceBadgeMaxLabelRunes {
		label = append(label[:presenceBadgeMaxLabelRunes-3], []rune("...")...)
	}
	return overlay.Initials + "  " + string(label)
}

func drawPresencePointer(drawList imgui.DrawList, tip imgui.Vec2, fill color.RGBA) {
	vertices := presencePointerPolygon(tip, 1)
	fill.A = presencePointerFillAlpha
	fillColor := imgui.Packed(fill)
	for _, triangle := range presencePointerTriangles {
		drawList.AddTriangleFilled(vertices[triangle[0]], vertices[triangle[1]], vertices[triangle[2]], fillColor)
	}
	for index := range vertices {
		drawList.AddLineV(vertices[index], vertices[(index+1)%len(vertices)], collaborationPresenceOutline, 1.5)
	}
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
