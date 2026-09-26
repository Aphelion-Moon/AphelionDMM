package mappingui

import (
	"fmt"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/render"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

type anchorDraft struct {
	root   mapping.Root
	target util.Point
	policy uint64
}

func (p *Panel) armAnchorMove() bool {
	root, ok := p.selectedRoot()
	if !ok || root.Parent != "" || root.StableID == "" || p.stale || p.pending != nil || p.results != nil || p.operation.err != nil {
		return false
	}
	if p.current.projection != nil && (p.current.request.focusRoot != root.ID || p.layerViews[0] == nil || p.layerViews[1] == nil) {
		p.queue(nil)
		p.moveRoot = root.ID
		p.pending.focusRoot = root.ID
		p.status = "Preparing selected placement before anchor movement…"
		return false
	}
	if p.current.projection != nil && (!p.layerViews[0].Render().LevelReady(root.Destination.Z) || !p.layerViews[1].Render().LevelReady(root.Destination.Z)) {
		p.status = "Preparing selected placement geometry before anchor movement…"
		return false
	}
	p.moveArmed = true
	p.status = "Drag the selected root marker; Escape cancels before submission."
	return true
}

func (p *Panel) selectedRoot() (mapping.Root, bool) {
	if p.current != nil {
		for _, root := range p.current.roots {
			if root.ID == p.focusRoot {
				return root, true
			}
		}
	}
	return mapping.Root{}, false
}
func (p *Panel) returnToParent() {
	defer p.clearContext()
	if host, ok := p.app.(interface {
		ReturnMappingContext(string, string, mapping.Transform)
	}); ok && p.current != nil && p.current.projection != nil {
		for _, placement := range p.current.projection.Placements {
			if placement.Root.ID == p.contextRoot {
				host.ReturnMappingContext(p.parentPath, p.contextPath, placement.Transform)
				return
			}
		}
	}
	p.app.DoLoadResource(p.parentPath)
}

func (p *Panel) openSelectedSource() {
	if p.current == nil || p.current.projection == nil || !p.choiceFulfilled(p.focusRoot) || p.stale || p.pending != nil || p.results != nil || p.operation.err != nil {
		p.sourceStatus = "Wait for the requested preview, or dismiss the request to edit the displayed source."
		return
	}
	for _, placement := range p.current.projection.Placements {
		if placement.Root.ID != p.focusRoot {
			continue
		}
		p.enterPlacement(placement, true)
		return
	}
	p.status = "Selected occurrence has no supported placement; inspect its diagnostic."
}

func (p *Panel) handleInline(point util.Point, active, pressed, released, cancel, focused bool, tool string) bool {
	if p.draft != nil {
		if cancel || !focused || p.draft.policy != p.RenderPolicyRevision() {
			p.cancelMove()
			p.status = "Anchor draft cancelled"
			return true
		}
		if active {
			p.draft.target = point
		}
		if released {
			draft := p.draft
			p.cancelMove()
			if host, ok := p.app.(mapHost); ok {
				if err := host.MoveMappingRoot(draft.root, draft.target, false); err != nil {
					p.status = err.Error()
				} else {
					p.status = "Anchor move submitted; waiting for accepted source"
				}
			}
		}
		return true
	}
	if cancel && (p.moveArmed || p.moveRoot != "") {
		p.cancelMove()
		return true
	}
	if !p.open || !p.previewVisible || !p.compose || p.current == nil || !active {
		return false
	}
	if p.views[1] != nil && !p.views[1].Render().LevelReady(point.Z) {
		return true
	}
	if p.stale || p.pending != nil || p.results != nil {
		p.moveArmed = false
	}
	if pressed {
		if !p.moveArmed && tool != "Move" && p.inspectContributors(point) {
			return true
		}
		for _, root := range p.current.roots {
			if root.Destination != point {
				continue
			}
			if tool == "Move" && !p.moveArmed && root.Parent == "" && root.StableID != "" {
				return false
			}
			if p.moveArmed && root.ID == p.focusRoot && root.Parent == "" {
				if host, ok := p.app.(mapHost); ok {
					if err := host.MoveMappingRoot(root, root.Local, true); err != nil {
						p.status = err.Error()
					} else {
						p.draft = &anchorDraft{root: root, target: root.Local, policy: p.RenderPolicyRevision()}
					}
				}
				return true
			}
			if tool != "Move" || p.moveArmed {
				p.selectOccurrence(root.ID)
				return true
			}
		}
	}
	// Conservatively protect contributed cells, including transparent source
	// pixels. A click must never silently hit a different base object underneath.
	if p.derivedCell(point) {
		p.inspected = point
		if pressed {
			p.status = "Derived contribution is locked. Select its root and open source in context."
		}
		return true
	}
	return false
}

func (p *Panel) derivedCell(point util.Point) bool {
	if p.current != nil && p.current.projection != nil {
		return derivedProvenance(p.current.projection.Provenance[point])
	}
	return false
}

func (p *Panel) inspectContributors(point util.Point) bool {
	ids := map[string]bool{}
	for _, root := range p.current.roots {
		if root.Destination == point {
			ids[root.ID] = true
		}
	}
	if p.current.projection != nil {
		if provenance := p.current.projection.Provenance[point]; provenance != nil {
			for _, c := range []*mapping.Contribution{provenance.Turf, provenance.Area} {
				if c != nil && c.Occurrence != "" {
					ids[c.Occurrence] = true
				}
			}
			for _, c := range provenance.Objects {
				if c.Occurrence != "" {
					ids[c.Occurrence] = true
				}
			}
		}
	}
	p.contributors = nil
	for _, root := range p.current.roots {
		if ids[root.ID] {
			p.contributors = append(p.contributors, root.ID)
		}
	}
	if len(p.contributors) == 1 {
		p.selectOccurrence(p.contributors[0])
		p.contributors = nil
		return true
	}
	if len(p.contributors) > 1 {
		p.status = "Choose an occurrence in the Composition inspector."
		return true
	}
	return false
}
func derivedProvenance(r *mapping.CellProvenance) bool {
	if r == nil {
		return false
	}
	if r.Covered || r.ReservedBy != "" || r.Turf != nil && r.Turf.Occurrence != "" || r.Area != nil && r.Area.Occurrence != "" {
		return true
	}
	for _, object := range r.Objects {
		if object.Occurrence != "" {
			return true
		}
	}
	return false
}

func (p *Panel) drawInline(camera render.Camera, size, origin imgui.Vec2) {
	if !p.open || !p.previewVisible || !p.compose || p.current == nil || p.views[1] == nil {
		return
	}
	v := p.views[1]
	if p.draft != nil && p.layerViews[1] != nil && p.current.request.focusRoot == p.draft.root.ID {
		v = p.layerViews[1]
	}
	*v.Render().Camera = camera
	if camera.Level < 1 || camera.Level > p.current.displays[1].MaxZ {
		return
	}
	v.Render().SetActiveLevel(p.current.displays[1], camera.Level)
	if !v.Render().LevelReady(camera.Level) {
		imgui.WindowDrawList().AddText(origin.Plus(imgui.Vec2{X: 12, Y: 12}), 0xffffffff, "Preparing composed view for this deck…")
		return
	}
	v.Process(size)
	draw := imgui.WindowDrawList()
	end := origin.Plus(size)
	draw.PushClipRect(origin, end)
	defer draw.PopClipRect()
	draw.AddImageV(imgui.TextureID(v.Texture()), origin, end, imgui.Vec2{Y: 1}, imgui.Vec2{X: 1}, 0xffffffff)
	p.camera, p.viewSize, p.level = camera, size, int32(camera.Level)
	if p.current.projection != nil {
		for _, placement := range p.current.projection.Placements {
			if placement.Root.ID != p.focusRoot {
				continue
			}
			lo, hi := placement.Transform.Apply(placement.Origin), placement.Transform.Apply(placement.Size)
			if p.draft != nil {
				dx, dy, dz := p.draft.target.X-p.draft.root.Local.X, p.draft.target.Y-p.draft.root.Local.Y, p.draft.target.Z-p.draft.root.Local.Z
				lo.X += dx
				hi.X += dx
				lo.Y += dy
				hi.Y += dy
				lo.Z += dz
				hi.Z += dz
			}
			if camera.Level < lo.Z || camera.Level > hi.Z {
				continue
			}
			bottom := p.screen(lo, origin)
			hi.X++
			hi.Y++
			top := p.screen(hi, origin)
			draw.AddRect(imgui.Vec2{X: bottom.X, Y: top.Y}, imgui.Vec2{X: top.X, Y: bottom.Y}, 0xffffffff)
		}
	}
	for _, root := range p.current.roots {
		if root.Destination.Z != camera.Level {
			continue
		}
		at := p.screen(root.Destination, origin)
		tile := float32(dmmap.WorldIconSize) * camera.Scale
		color := imgui.PackedColor(0xffa0a0a0)
		if root.ID == p.focusRoot {
			color = 0xffffffff
		}
		draw.AddRect(imgui.Vec2{X: at.X, Y: at.Y - tile}, imgui.Vec2{X: at.X + tile, Y: at.Y}, color)
		if root.ID == p.focusRoot {
			label := "Selected: "
			if p.moveArmed {
				label = "Move anchor: "
			}
			draw.AddText(imgui.Vec2{X: at.X, Y: at.Y - tile - imgui.TextLineHeight()}, color, label+root.Key)
		}
	}
	if p.draft != nil {
		at := p.screen(p.draft.target, origin)
		tile := float32(dmmap.WorldIconSize) * camera.Scale
		draw.AddRect(imgui.Vec2{X: at.X, Y: at.Y - tile}, imgui.Vec2{X: at.X + tile, Y: at.Y}, 0xffffffff)
		delta := p.draft.target
		delta.X -= p.draft.root.Local.X
		delta.Y -= p.draft.root.Local.Y
		delta.Z -= p.draft.root.Local.Z
		if preview := p.layerViews[0]; preview != nil && p.current.request.focusRoot == p.draft.root.ID {
			shifted := camera
			shifted.ShiftX += float32(delta.X * dmmap.WorldIconSize)
			shifted.ShiftY += float32(delta.Y * dmmap.WorldIconSize)
			shifted.Level -= delta.Z
			*preview.Render().Camera = shifted
			if shifted.Level >= 1 && shifted.Level <= p.current.layerDisplays[0].MaxZ {
				preview.Render().SetActiveLevel(p.current.layerDisplays[0], shifted.Level)
				preview.Process(size)
				draw.AddImageV(imgui.TextureID(preview.Texture()), origin, end, imgui.Vec2{Y: 1}, imgui.Vec2{X: 1}, 0xffffffff)
			}
		}
		draw.AddText(imgui.Vec2{X: origin.X + 12, Y: end.Y - 70}, 0xffffffff, fmt.Sprintf("Anchor %v → %v; delta %v (provisional)", p.draft.root.Local, p.draft.target, delta))
	}
	if p.stale || p.pending != nil || p.results != nil || p.operation.err != nil {
		draw.AddText(origin.Plus(imgui.Vec2{X: 12, Y: 12}), 0xffffffff, "Previous composition result — read-only context")
	}
}
