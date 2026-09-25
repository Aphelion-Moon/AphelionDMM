package mappingui

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"math"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func (p *Panel) provenanceControls() {
	if p.inspected.Z == 0 || p.current.projection == nil {
		return
	}
	if !imgui.TreeNode(fmt.Sprintf("Cell provenance %d,%d,%d", p.inspected.X, p.inspected.Y, p.inspected.Z)) {
		return
	}
	defer imgui.TreePop()
	r := p.current.projection.Provenance[p.inspected]
	if r == nil {
		imgui.Text("No authored contribution")
		return
	}
	imgui.BeginChildV("cell-provenance", imgui.Vec2{Y: 120}, true, imgui.WindowFlagsNone)
	defer imgui.EndChild()
	show := func(channel string, c *mapping.Contribution) {
		if c == nil {
			imgui.Text(channel + ": no authored contribution")
			return
		}
		imgui.TextWrapped(fmt.Sprintf("%s: %s %s at %d,%d,%d — %s", channel, c.Phase, c.Source, c.Local.X, c.Local.Y, c.Local.Z, c.Reason))
	}
	show("Turf", r.Turf)
	show("Area", r.Area)
	for _, c := range r.Objects {
		show("Object", &c)
	}
	for _, c := range r.Suppressed {
		show("Suppressed", &c)
	}
	if r.ReservedBy != "" {
		imgui.Text("Reserved by: " + r.ReservedBy)
	}
}

func (p *Panel) drawReservations(start, end imgui.Vec2) {
	if p.current.projection == nil {
		return
	}
	tile := float32(dmmap.WorldIconSize)
	x1 := max(1, int(math.Floor(float64(-p.camera.ShiftX/tile)))+1)
	y1 := max(1, int(math.Floor(float64(-p.camera.ShiftY/tile)))+1)
	x2 := min(p.current.sources[0].Size.X, int((p.viewSize.X/p.camera.Scale-p.camera.ShiftX)/tile)+1)
	y2 := min(p.current.sources[0].Size.Y, int((p.viewSize.Y/p.camera.Scale-p.camera.ShiftY)/tile)+1)
	draw := imgui.WindowDrawList()
	draw.PushClipRect(start, end)
	defer draw.PopClipRect()
	count := 0
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			count++
			if count > 4096 {
				draw.AddText(start, 0xffffffff, "Zoom in to inspect all reservations")
				return
			}
			point := util.Point{X: x, Y: y, Z: int(p.level)}
			r := p.current.projection.Provenance[point]
			if r == nil || (r.ReservedBy == "" && len(r.Suppressed) == 0) {
				continue
			}
			bottom := p.screen(point, start)
			color := imgui.Vec4{X: 1, Y: .6, W: .4}
			if len(r.Suppressed) > 0 {
				color.Z = 1
			}
			draw.AddRectFilled(imgui.Vec2{X: bottom.X, Y: bottom.Y - tile*p.camera.Scale}, imgui.Vec2{X: bottom.X + tile*p.camera.Scale, Y: bottom.Y}, imgui.PackedColorFromVec4(color))
		}
	}
}

func (p *Panel) drawDifferences(start, end imgui.Vec2) {
	if p.current.sources[1] == nil {
		return
	}
	base, reference := p.current.sources[0], p.current.sources[1]
	transform := mapping.Transform{Offset: util.Point{X: int(p.offset[0]), Y: int(p.offset[1]), Z: int(p.offset[2])}}
	tile := float32(dmmap.WorldIconSize)
	x1 := max(1, int(math.Floor(float64(-p.camera.ShiftX/tile)))+1)
	y1 := max(1, int(math.Floor(float64(-p.camera.ShiftY/tile)))+1)
	x2 := min(base.Size.X, int((p.viewSize.X/p.camera.Scale-p.camera.ShiftX)/tile)+1)
	y2 := min(base.Size.Y, int((p.viewSize.Y/p.camera.Scale-p.camera.ShiftY)/tile)+1)
	draw := imgui.WindowDrawList()
	draw.PushClipRect(start, end)
	defer draw.PopClipRect()
	count := 0
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			point := util.Point{X: x, Y: y, Z: int(p.level)}
			local := transform.Inverse(point)
			if local.X < 1 || local.Y < 1 || local.Z < 1 || local.X > reference.Size.X || local.Y > reference.Size.Y || local.Z > reference.Size.Z {
				continue
			}
			count++
			if count > 4096 {
				draw.AddText(start, 0xffffffff, "Zoom in to inspect all cell differences")
				return
			}
			diff := mapping.CompareCell(base.Cell(point), reference.Cell(local))
			if !diff.Turf && !diff.Area && !diff.Objects {
				continue
			}
			color := imgui.Vec4{W: .45}
			if diff.Turf {
				color.X = 1
			}
			if diff.Area {
				color.Y = 1
			}
			if diff.Objects {
				color.Z = 1
			}
			bottom := p.screen(point, start)
			draw.AddRectFilled(imgui.Vec2{X: bottom.X, Y: bottom.Y - tile*p.camera.Scale}, imgui.Vec2{X: bottom.X + tile*p.camera.Scale, Y: bottom.Y}, imgui.PackedColorFromVec4(color))
		}
	}
}

func (p *Panel) drawBounds(start imgui.Vec2) {
	if p.current.sources[1] == nil {
		return
	}
	s := p.current.sources[1]
	origin := util.Point{X: 1 + int(p.offset[0]), Y: 1 + int(p.offset[1]), Z: int(p.level)}
	bottom := p.screen(origin, start)
	top := p.screen(util.Point{X: origin.X + s.Size.X, Y: origin.Y + s.Size.Y, Z: origin.Z}, start)
	imgui.WindowDrawList().AddRect(imgui.Vec2{X: bottom.X, Y: top.Y}, imgui.Vec2{X: top.X, Y: bottom.Y}, 0xffffffff)
}
