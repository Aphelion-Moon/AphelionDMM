// APHELION EDIT ADDITION START - SHARED SHAPES
package pmap

import (
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
)

func (p *PaneMap) showShapeControls() {
	if p.app == nil || p.editor == nil {
		return
	}
	settings := p.app.Prefs().Mapper
	if settings == nil {
		return
	}
	if !tools.IsSelected(tools.TNAdd) && !tools.IsSelected(tools.TNDelete) && !tools.IsSelected(tools.TNFill) && !tools.IsSelected(tools.TNGrab) {
		return
	}
	d := &settings.Shape
	for kind, label := range []string{"Rectangle", "Ellipse", "Circle"} {
		if kind > 0 {
			imgui.SameLine()
		}
		if imgui.RadioButton(label+"##brush-shape", d.Kind == editing.ShapeKind(kind)) {
			d.Kind = editing.ShapeKind(kind)
		}
	}
	w, h, thickness := int32(max(1, d.Width)), int32(max(1, d.Height)), int32(max(1, d.Thickness))
	imgui.SetNextItemWidth(90)
	imgui.InputInt("Width / diameter", &w)
	if d.Kind != editing.ShapeCircle {
		imgui.SetNextItemWidth(90)
		imgui.InputInt("Height", &h)
	} else {
		h = w
	}
	imgui.Checkbox("Outline", &d.Outline)
	if d.Outline {
		imgui.SetNextItemWidth(90)
		imgui.InputInt("Thickness", &thickness)
	}
	d.Width, d.Height, d.Thickness = int(max(1, min(4096, w))), int(max(1, min(4096, h))), int(max(1, min(4096, thickness)))
	imgui.Text("Anchor: bottom-left tile. Fill and Grab use dragged dimensions.")
}

// APHELION EDIT ADDITION END
