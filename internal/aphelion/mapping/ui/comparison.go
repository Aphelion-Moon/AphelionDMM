package mappingui

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"path/filepath"
	"sdmm/internal/aphelion/mapview"
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

// Comparison is only a view. Save, undo and edits remain owned by the source
// workspace; closing this view cannot dispose that source's composition session.
type Comparison struct {
	workspace.Content
	Panel     *Panel
	shortcuts shortcut.Shortcuts
}

func (p *Panel) SourcePath() string { return p.parentPath }
func (p *Panel) Frame(point util.Point) {
	p.level = int32(point.Z)
	p.inspected = point
	mapview.Center(&p.camera, p.viewSize, point)
}
func (c *Comparison) Initialize() {
	for _, b := range []struct {
		name   string
		key    glfw.Key
		action func()
	}{
		{"doMoveCameraUp", glfw.KeyUp, func() { mapview.Pan(&c.Panel.camera, imgui.Vec2{Y: float32(dmmap.WorldIconSize)}) }},
		{"doMoveCameraDown", glfw.KeyDown, func() { mapview.Pan(&c.Panel.camera, imgui.Vec2{Y: -float32(dmmap.WorldIconSize)}) }},
		{"doMoveCameraLeft", glfw.KeyLeft, func() { mapview.Pan(&c.Panel.camera, imgui.Vec2{X: float32(dmmap.WorldIconSize)}) }},
		{"doMoveCameraRight", glfw.KeyRight, func() { mapview.Pan(&c.Panel.camera, imgui.Vec2{X: -float32(dmmap.WorldIconSize)}) }},
		{"doZoomIn", glfw.KeyEqual, func() {
			mapview.Zoom(&c.Panel.camera, c.Panel.viewSize, imgui.Vec2{X: c.Panel.viewSize.X / 2, Y: c.Panel.viewSize.Y / 2}, true)
		}},
		{"doZoomOut", glfw.KeyMinus, func() {
			mapview.Zoom(&c.Panel.camera, c.Panel.viewSize, imgui.Vec2{X: c.Panel.viewSize.X / 2, Y: c.Panel.viewSize.Y / 2}, false)
		}},
	} {
		c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#" + b.name, FirstKey: b.key, Action: b.action})
	}
	zoomIn := func() {
		mapview.Zoom(&c.Panel.camera, c.Panel.viewSize, imgui.Vec2{X: c.Panel.viewSize.X / 2, Y: c.Panel.viewSize.Y / 2}, true)
	}
	c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#doZoomIn", FirstKey: glfw.KeyKPAdd, Action: zoomIn})
	c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#doZoomIn", FirstKey: glfw.KeyLeftShift, FirstKeyAlt: glfw.KeyRightShift, SecondKey: glfw.KeyEqual, Action: zoomIn})
	c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#doZoomOut", FirstKey: glfw.KeyKPSubtract, Action: func() {
		mapview.Zoom(&c.Panel.camera, c.Panel.viewSize, imgui.Vec2{X: c.Panel.viewSize.X / 2, Y: c.Panel.viewSize.Y / 2}, false)
	}})
	c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#doNextLevel", FirstKey: platform.KeyModLeft(), FirstKeyAlt: platform.KeyModRight(), SecondKey: glfw.KeyUp, Action: func() { c.Panel.level++ }})
	c.shortcuts.Add(shortcut.Shortcut{Name: "pmap#doPreviousLevel", FirstKey: platform.KeyModLeft(), FirstKeyAlt: platform.KeyModRight(), SecondKey: glfw.KeyDown, Action: func() { c.Panel.level = max(1, c.Panel.level-1) }})
}
func (c *Comparison) OnCommandContextChange(active bool) { c.shortcuts.SetVisible(active) }
func (c *Comparison) Dispose()                           { c.shortcuts.Dispose() }
func (c *Comparison) Name() string                       { return "Compare " + filepath.Base(c.Panel.parentPath) }
func (c *Comparison) Title() string                      { return c.Name() }
func (c *Comparison) Process() {
	if !c.Panel.open {
		imgui.TextWrapped("Source composition is closed. Reopen it from the source map.")
		return
	}
	c.Panel.comparisonControls()
	c.Panel.compareViews()
}
func (p *Panel) comparisonControls() {
	modes := []string{"Synchronized split", "Overlay", "Wipe", "Blink", "Structural differences", "Source bounds", "Reservations / suppression", "Single composed view"}
	imgui.SetNextItemWidth(max(80, min(240, imgui.ContentRegionAvail().X-90)))
	if imgui.BeginCombo("##comparison-mode", modes[p.mode]) {
		for i, mode := range modes {
			if imgui.Selectable(mode) {
				p.mode = int32(i)
			}
		}
		imgui.EndCombo()
	}
	imgui.SameLine()
	if imgui.Button("View…") {
		imgui.OpenPopup("comparison-options")
	}
	if imgui.BeginPopup("comparison-options") {
		if imgui.Button("Fit source") && p.current != nil && p.current.sources[0] != nil {
			s := p.current.sources[0]
			mapview.Fit(&p.camera, p.viewSize, util.Point{X: 1, Y: 1, Z: int(p.level)}, s.Size)
		}
		if imgui.Button("Fit selected placement") {
			p.fitPlacement()
		}
		if p.mode == 1 {
			imgui.SetNextItemWidth(120)
			imgui.SliderFloat("Blend", &p.alpha, 0, 1)
		}
		if p.mode == 2 {
			imgui.SetNextItemWidth(120)
			imgui.SliderFloat("Wipe", &p.wipe, 0, 1)
		}
		imgui.SetNextItemWidth(100)
		imgui.InputInt("Z", &p.level)
		imgui.EndPopup()
	}
	p.level = max(1, p.level)
	if p.current != nil && p.current.sources[0] != nil {
		p.level = min(p.level, int32(p.current.sources[0].Size.Z))
	}
	imgui.TextWrapped("Locked comparison — source: " + filepath.Base(p.parentPath))
}

func (p *Panel) fitPlacement() {
	if p.current == nil || p.current.projection == nil {
		p.status = "Select a placed root and wait for its preview."
		return
	}
	for _, placement := range p.current.projection.Placements {
		if placement.Root.ID == p.focusRoot {
			lo, hi := placement.Transform.Apply(placement.Origin), placement.Transform.Apply(placement.Size)
			mapview.Fit(&p.camera, p.viewSize, lo, hi)
			p.level = int32(lo.Z)
			return
		}
	}
	p.status = "Selected root has no supported placement; inspect its diagnostic."
}
