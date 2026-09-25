// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
package pmap

import (
	"github.com/SpaiR/imgui-go"
	"math"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"time"
)

// ResolveCanvasInput is shared by the pane and native input fixtures. Control
// geometry is current before tools run; camera, picking and overlays precede GL.
func (p *PaneMap) ResolveCanvasInput() {
	p.processCanvasCamera()
	if !p.canvas.Render().LevelReady(p.activeLevel) {
		p.pointerSamples = nil
		p.lastSampleValid = false
		p.canvasState.SetMousePosition(-1, -1, -1)
		p.canvasState.SetHoveredInstance(nil)
		p.processCanvasOverlayFlick()
		p.processCanvasOverlayAreasZones()
		return
	}
	owner := activePane == p || activePane == nil && lastActivePane == p
	if !owner {
		p.pointerSamples = nil
		p.processCanvasOverlayFlick()
		p.processCanvasOverlayAreasZones()
		return
	}
	processTempToolsMode()
	if tools.OwnsGesture(p.editor) {
		point := imgui.MousePos()
		if len(p.pointerSamples) == 0 || p.pointerSamples[len(p.pointerSamples)-1] != point {
			p.pointerSamples = append(p.pointerSamples, point)
		}
	} else {
		p.pointerSamples = nil
		p.lastSampleValid = false
	}
	r := p.canvas.Render()
	r.BeginUpdateBatch()
	started := time.Now()
	consumed := 0
	steps := 0
	for consumed < len(p.pointerSamples) && steps < 256 {
		point := p.pointerSamples[consumed]
		p.updateCanvasMousePosition(int(point.X), int(point.Y))
		x, y := p.canvasState.RelMouseX(), p.canvasState.RelMouseY()
		reached := true
		brush := tools.Selected().Name() == tools.TNDelete || tools.Selected().Name() == tools.TNAdd || tools.Selected().Name() == tools.TNReplace
		if p.lastSampleValid && brush {
			dx, dy := float64(x)-float64(p.lastSample.X), float64(y)-float64(p.lastSample.Y)
			distance := math.Max(math.Abs(dx), math.Abs(dy))
			if distance > float64(dmmap.WorldIconSize) {
				factor := float64(dmmap.WorldIconSize) / distance
				x = int(p.lastSample.X) + int(math.Round(dx*factor))
				y = int(p.lastSample.Y) + int(math.Round(dy*factor))
				p.canvasState.SetMousePosition(x, y, p.activeLevel)
				reached = false
			}
		}
		p.lastSample = imgui.Vec2{X: float32(x), Y: float32(y)}
		p.lastSampleValid = true
		p.resolvePointerPick()
		tools.OnMouseMove()
		if reached {
			consumed++
		}
		steps++
		if time.Since(started) >= 2*time.Millisecond {
			break
		}
	}
	if consumed > 0 {
		copy(p.pointerSamples, p.pointerSamples[consumed:])
		p.pointerSamples = p.pointerSamples[:len(p.pointerSamples)-consumed]
	}
	if len(p.pointerSamples) == 0 {
		point := imgui.MousePos()
		p.updateCanvasMousePosition(int(point.X), int(point.Y))
		p.resolvePointerPick()
		// Deliver the final position before releasing the stroke.
		tools.OnMouseMove()
	}
	tools.ProcessForEditor(p.editor, len(p.pointerSamples) != 0)
	if tools.OwnsGesture(p.editor) && !p.lastSampleValid {
		p.lastSample = imgui.Vec2{X: float32(p.canvasState.RelMouseX()), Y: float32(p.canvasState.RelMouseY())}
		p.lastSampleValid = true
	}
	r.EndUpdateBatch(p.dmm)
	p.resolvePointerPick()
	if p.pendingTileMenu {
		p.pendingTileMenu = false
		p.openTileMenu()
	}
	p.processCanvasOverlay()
	p.publishPointerPresence()
}

func (p *PaneMap) resolvePointerPick() {
	var hovered *dmminstance.Instance
	if p.canvas.Render().LevelReady(p.activeLevel) && (p.canvasControl.Active() || tools.OwnsGesture(p.editor)) {
		hovered = p.canvas.Render().PickAt(p.canvasState.RelMouseX(), p.canvasState.RelMouseY(), p.activeLevel, func(i *dmminstance.Instance) bool {
			return i != nil && i.Prefab() != nil && p.app.PathsFilter().IsVisiblePath(i.Prefab().Path())
		})
	}
	p.canvasState.SetHoveredInstance(hovered)
}

func (p *PaneMap) publishPointerPresence() {
	if p.canvasState.HoverOutOfBounds() {
		return
	}
	hovered := p.canvasState.HoveredTile()
	var selection *protocol.PresenceSelection
	if selected, ok := tools.Selected().(*tools.ToolGrab); ok && selected.HasSelectedArea() {
		bounds := selected.Bounds()
		selection = &protocol.PresenceSelection{Min: model.Coord{X: int(bounds.X1), Y: int(bounds.Y1), Z: p.activeLevel}, Max: model.Coord{X: int(bounds.X2), Y: int(bounds.Y2), Z: p.activeLevel}}
	}
	p.app.PublishCollaborationPresence(model.Coord{X: hovered.X, Y: hovered.Y, Z: hovered.Z}, selection)
}

// APHELION EDIT ADDITION END
