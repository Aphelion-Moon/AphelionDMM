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
)

var collaborationPresenceColors = [...]imgui.PackedColor{
	imgui.Packed(color.RGBA{R: 191, G: 211, B: 230, A: 255}),
	imgui.Packed(color.RGBA{R: 209, G: 196, B: 233, A: 255}),
	imgui.Packed(color.RGBA{R: 178, G: 223, B: 219, A: 255}),
	imgui.Packed(color.RGBA{R: 255, G: 224, B: 178, A: 255}),
	imgui.Packed(color.RGBA{R: 207, G: 216, B: 220, A: 255}),
	imgui.Packed(color.RGBA{R: 197, G: 225, B: 165, A: 255}),
	imgui.Packed(color.RGBA{R: 255, G: 204, B: 188, A: 255}),
	imgui.Packed(color.RGBA{R: 225, G: 190, B: 231, A: 255}),
}

var collaborationPresenceTextShadow = imgui.Packed(color.RGBA{A: 255})

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
		styleColor := collaborationPresenceColors[int(overlay.StyleSlot)%len(collaborationPresenceColors)]
		drawList.AddRectV(min, max, styleColor, 0, imgui.DrawFlagsNone, collaborationPresenceWidth)
		if overlay.Selection != nil {
			selectionMin, selectionMax := presenceSelectionScreenBounds(*overlay.Selection, camera.Scale, camera.ShiftX, camera.ShiftY, p.canvasControl.PosMin(), p.canvasControl.PosMax())
			drawList.AddRectV(selectionMin, selectionMax, styleColor, 0, imgui.DrawFlagsNone, collaborationPresenceWidth)
		}
		labelPosition := min.Plus(imgui.Vec2{X: 3, Y: 2})
		drawList.AddText(labelPosition.Plus(imgui.Vec2{X: 1, Y: 1}), collaborationPresenceTextShadow, overlay.Label)
		drawList.AddText(labelPosition, styleColor, overlay.Label)
	}
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
