package pmap

import (
	"math"
	// APHELION EDIT ADDITION START - SHARED MAP VIEW
	"sdmm/internal/aphelion/mapview"
	// APHELION EDIT ADDITION END
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/imguiext"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
)

const scaleFactor float32 = math.Sqrt2

func (p *PaneMap) processCanvasCamera() {
	p.processCameraMove()
	p.processCameraZoom()
}

func (p *PaneMap) processCameraMove() {
	if p.canvasControl.Moving() {
		if delta := imgui.CurrentIO().MouseDelta(); delta.X != 0 || delta.Y != 0 {
			p.translateCanvas(delta.X, delta.Y)
		}
	}
}

func (p *PaneMap) processCameraZoom() {
	if !p.canvasControl.Zoomed() || !p.canvasControl.Active() {
		return
	}

	camera := p.canvas.Render().Camera
	_, mouseWheel := imgui.CurrentIO().MouseWheel()

	// Support for alternative scroll behaviour.
	// Pan with a scroll, zoom if a space key pressed.
	if p.app.Prefs().Controls.AltScrollBehaviour && !imgui.IsKeyDown(int(glfw.KeySpace)) {
		shift := p.calcManualCanvasTranslateShiftV(mouseWheel)
		if imguiext.IsCtrlDown() {
			p.translateCanvas(shift, 0)
		} else {
			p.translateCanvas(0, shift)
		}
		return
	}

	mousePos := imgui.MousePos()
	localPos := mousePos.Minus(p.canvasControl.PosMin())
	/* APHELION EDIT REMOVAL START - SHARED MAP VIEW
	zoomIn := mouseWheel > 0
	scale := camera.Scale
	if zoomIn { scale *= -scaleFactor }
	offsetX := localPos.X / scale / 2
	offsetY := (p.size.Y - localPos.Y) / scale / 2
	camera.Translate(offsetX, offsetY)
	camera.Zoom(zoomIn, scaleFactor)
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SHARED MAP VIEW
	mapview.Zoom(camera, p.size, localPos, mouseWheel > 0)
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) calcManualCanvasTranslateShift() float32 {
	// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: return p.calcManualCanvasTranslateShiftV(1)
	return float32(dmmap.WorldIconSize)
}

func (p *PaneMap) calcManualCanvasTranslateShiftV(mod float32) float32 {
	value := mod * float32(dmmap.WorldIconSize)
	if imguiext.IsShiftDown() {
		return value * 5
	}
	return value
}

func (p *PaneMap) translateCanvas(shiftX, shiftY float32) {
	camera := p.canvas.Render().Camera
	// APHELION EDIT CHANGE - SHARED MAP VIEW - ORIGINAL: camera.Translate(shiftX/camera.Scale, -shiftY/camera.Scale)
	mapview.Pan(camera, imgui.Vec2{X: shiftX, Y: shiftY})
}
