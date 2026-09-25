package mappingui

import (
	"fmt"
	"path/filepath"
	"strings"

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
	if !ok || root.Parent != "" || root.StableID == "" {
		return false
	}
	if p.current.projection != nil && (p.current.request.focusRoot != root.ID || p.layerViews[0] == nil || p.layerViews[1] == nil) {
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
func (p *Panel) sidebar() {
	imgui.Text(filepath.Base(p.parentPath))
	if imgui.IsItemHovered() {
		imgui.SetTooltip(p.parentPath)
	}
	if imgui.Button("Show composition in this map") {
		p.open = true
		p.compose = true
		p.queue(nil)
	}
	if p.open && imgui.SmallButton("Hide composition") {
		p.open = false
		p.draft = nil
		p.moveArmed = false
	}
	if host, ok := p.app.(mapHost); ok && imgui.Button("Open comparison tab") {
		p.open = true
		host.OpenMappingComparison(p)
		if p.current == nil {
			p.compose = true
			p.queue(nil)
		}
	}
	if p.contextPath != "" {
		imgui.TextWrapped("Editing: " + filepath.Base(p.contextPath) + " (other contributions locked)")
		if imgui.Button("Return to parent") {
			p.returnToParent()
		}
	}
	imgui.TextWrapped(p.status)
	if p.mapConfig != "" {
		imgui.TextWrapped("Configuration: " + p.mapConfig)
	} else if p.current != nil && len(p.current.configurations) > 1 {
		imgui.TextWrapped("Multiple configurations match this map. Choose one:")
		for _, config := range p.current.configurations {
			if imgui.Selectable(config) {
				p.mapConfig = config
				p.queue(nil)
			}
		}
	}
	imgui.InputText("Find root", &p.rootFilter)
	if p.current != nil {
		imgui.BeginChildV("roots", imgui.Vec2{Y: 180}, true, imgui.WindowFlagsNone)
		for _, root := range p.current.roots {
			label := fmt.Sprintf("%s [%d,%d,%d]", root.Key, root.Destination.X, root.Destination.Y, root.Destination.Z)
			if !strings.Contains(strings.ToLower(label), strings.ToLower(p.rootFilter)) {
				continue
			}
			imgui.PushID(root.ID)
			if imgui.SelectableV(label, root.ID == p.focusRoot, imgui.SelectableFlagsNone, imgui.Vec2{}) {
				p.focusRoot = root.ID
				p.moveArmed = false
				p.queue(nil)
			}
			if p.revealRoot && root.ID == p.focusRoot {
				imgui.SetScrollHereY(.5)
				p.revealRoot = false
			}
			imgui.PopID()
		}
		imgui.EndChild()
	}
	if root, ok := p.selectedRoot(); ok {
		imgui.TextWrapped(fmt.Sprintf("%s — %s\n%d alternatives; source %d,%d,%d", root.Key, filepath.Base(root.Source.Path), len(root.Candidates), root.Local.X, root.Local.Y, root.Local.Z))
		if imgui.Button("Frame selected / Go to root") {
			p.navigate(root.Destination)
		}
		p.alternativeControls(root)
		excluded := p.scenario.Excluded[root.ID]
		if imgui.Checkbox("Exclude (editor what-if)", &excluded) {
			if p.scenario.Excluded == nil {
				p.scenario.Excluded = map[string]bool{}
			}
			p.scenario.Excluded[root.ID] = excluded
			p.queue(nil)
		}
		for _, candidate := range root.Candidates {
			if candidate.Error != "" {
				imgui.TextWrapped(filepath.Base(candidate.Path) + ": " + candidate.Error)
				continue
			}
			if imgui.SelectableV(fmt.Sprintf("%d: %s##candidate", candidate.Slot+1, filepath.Base(candidate.Path)), p.choices[root.ID].Slot == candidate.Slot, imgui.SelectableFlagsNone, imgui.Vec2{}) && candidate.Error == "" {
				if p.choices == nil {
					p.choices = map[string]mapping.Choice{}
				}
				p.choices[root.ID] = mapping.Choice{Slot: candidate.Slot}
				p.referencePath = candidate.Path
				p.queue(nil)
			}
		}
		if p.current.projection != nil {
			for _, placement := range p.current.projection.Placements {
				if placement.Root.ID == root.ID {
					uses := 0
					for _, other := range p.current.projection.Placements {
						if strings.EqualFold(other.Source.Path, placement.Source.Path) {
							uses++
						}
					}
					imgui.TextWrapped(fmt.Sprintf("Editing %s affects %d occurrence(s) in this composition.", filepath.Base(placement.Source.Path), uses))
					break
				}
			}
		}
		if imgui.Button("Open source in context") {
			p.openSelectedSource()
		}
		if host, ok := p.app.(interface{ UnhideMappingRoot(mapping.Root) error }); ok && imgui.SmallButton("Unhide this root's exact type") {
			if err := host.UnhideMappingRoot(root); err != nil {
				p.status = err.Error()
			}
		}
		if root.Parent == "" && root.StableID != "" && imgui.Button("Move anchor") {
			p.armAnchorMove()
		}
		if root.Parent == "" && root.StableID != "" && imgui.TreeNode("Move anchor to another deck") {
			if p.targetDeck < 1 {
				p.targetDeck = int32(root.Local.Z)
			}
			imgui.InputInt("Destination deck", &p.targetDeck)
			if imgui.Button("Commit deck move") {
				if host, ok := p.app.(mapHost); ok {
					to := root.Local
					to.Z = int(p.targetDeck)
					if err := host.MoveMappingRoot(root, to, false); err != nil {
						p.status = err.Error()
					} else {
						p.status = "Deck move submitted"
					}
				}
			}
			imgui.TreePop()
		}
		if root.Parent != "" {
			imgui.TextWrapped("Nested root: enter its containing template before moving it. Editing that source affects every use.")
			if imgui.Button("Edit containing source") {
				p.focusRoot = root.Parent
				p.openSelectedSource()
			}
		}
		if imgui.TreeNode("Binding properties") {
			imgui.TextWrapped(root.Source.Path + "\n" + root.Config + "\n" + root.ID)
			imgui.TreePop()
		}
	}
	if p.current != nil && imgui.TreeNode(fmt.Sprintf("Diagnostics (%d)", len(p.current.diagnostics))) {
		for _, d := range p.current.diagnostics {
			imgui.TextWrapped(d.Severity + ": " + d.Message)
		}
		imgui.TreePop()
	}
	if imgui.TreeNode("Advanced references and authoring") {
		p.controls()
		imgui.TreePop()
	}
}

func (p *Panel) returnToParent() {
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
	if p.current == nil || p.current.projection == nil {
		return
	}
	for _, placement := range p.current.projection.Placements {
		if placement.Root.ID != p.focusRoot {
			continue
		}
		p.referencePath = placement.Source.Path
		p.contextPath = placement.Source.Path
		p.contextRoot = placement.Root.ID
		if host, ok := p.app.(mapHost); ok {
			host.OpenMappingContext(p.parentPath, placement.Source.Path, placement.Transform)
		} else {
			p.OpenReferenceForEditing()
		}
		p.queue(nil)
		return
	}
	p.status = "Selected occurrence has no supported placement; inspect its diagnostic."
}

func (p *Panel) handleInline(point util.Point, active, pressed, released, cancel, focused bool, tool string) bool {
	if p.draft != nil {
		if cancel || !focused || p.draft.policy != p.RenderPolicyRevision() {
			p.draft = nil
			p.status = "Anchor draft cancelled"
			return true
		}
		if active {
			p.draft.target = point
		}
		if released {
			draft := p.draft
			p.draft = nil
			p.moveArmed = false
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
	if !p.open || !p.compose || p.current == nil || !active {
		return false
	}
	if pressed {
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
				p.focusRoot = root.ID
				p.revealRoot = true
				p.queue(nil)
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
	if !p.open || !p.compose || p.current == nil || p.views[1] == nil {
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
			draw.AddText(imgui.Vec2{X: at.X, Y: at.Y - tile - imgui.TextLineHeight()}, color, "Move anchor: "+root.Key)
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
}
