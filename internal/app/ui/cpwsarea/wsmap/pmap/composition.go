// APHELION EDIT ADDITION START - MAP COMPOSITION
package pmap

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/mapview"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func (p *PaneMap) compositionInput() bool {
	host, ok := p.app.(interface {
		CompositionInput(string, util.Point, bool, bool, bool, bool, bool, string) bool
	})
	if !ok || tools.OwnsGesture(p.editor) {
		return false
	}
	pos := imgui.MousePos()
	p.updateCanvasMousePosition(int(pos.X), int(pos.Y))
	return host.CompositionInput(p.dmm.Path.Absolute, p.canvasState.HoveredTile(), p.canvasControl.Active(), imgui.IsMouseClicked(0), imgui.IsMouseReleased(0), imgui.IsKeyPressed(int(glfw.KeyEscape)), p.focused, tools.Selected().Name())
}
func (p *PaneMap) compositionCamera() {
	if host, ok := p.app.(interface {
		CompositionCamera(string) (render.Camera, bool)
	}); ok {
		if camera, ready := host.CompositionCamera(p.dmm.Path.Absolute); ready {
			*p.canvas.Render().Camera = camera
			p.SetActiveLevel(min(p.dmm.MaxZ, camera.Level))
		}
	}
}
func (p *PaneMap) compositionDraw() {
	if host, ok := p.app.(interface {
		CompositionDraw(string, render.Camera, imgui.Vec2, imgui.Vec2)
	}); ok {
		camera := *p.canvas.Render().Camera
		camera.Level = p.activeLevel
		host.CompositionDraw(p.dmm.Path.Absolute, camera, p.size, p.canvasControl.PosMin())
		visible, ok := p.app.(interface{ CompositionVisible(string) bool })
		if !ok || !visible.CompositionVisible(p.dmm.Path.Absolute) || !p.canvasControl.Active() {
			return
		}
		context := tools.CurrentActionContext()
		label := context.Badge
		if locked, ok := p.app.(interface{ CompositionTileLocked(string, util.Point) bool }); ok && locked.CompositionTileLocked(p.dmm.Path.Absolute, p.canvasState.HoveredTile()) && !tools.OwnsGesture(p.editor) {
			label = "Locked contribution — open source in context"
			context.HasFootprint = false
		}
		draw := imgui.WindowDrawList()
		origin := p.canvasControl.PosMin()
		end := origin.Plus(p.size)
		draw.PushClipRect(origin, end)
		defer draw.PopClipRect()
		if context.HasFootprint {
			b := context.Footprint
			lo := mapview.Screen(camera, p.size, origin, util.Point{X: int(b.X1), Y: int(b.Y1), Z: p.activeLevel})
			hi := mapview.Screen(camera, p.size, origin, util.Point{X: int(b.X2) + 1, Y: int(b.Y2) + 1, Z: p.activeLevel})
			draw.AddRect(imgui.Vec2{X: lo.X, Y: hi.Y}, imgui.Vec2{X: hi.X, Y: lo.Y}, 0xffffffff)
		}
		draw.AddText(imgui.MousePos().Plus(imgui.Vec2{X: 16, Y: 18}), 0xffffffff, label)
	}
}

// APHELION EDIT ADDITION END
