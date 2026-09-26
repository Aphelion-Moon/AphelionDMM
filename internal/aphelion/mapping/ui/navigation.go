package mappingui

import (
	"fmt"
	"path/filepath"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/render"
)

type contextFrame struct {
	path, root string
	transform  mapping.Transform
	lifetime   string
	camera     render.Camera
}

func (p *Panel) enterPlacement(placement mapping.Placement, remember bool) {
	p.enterContext(contextFrame{path: placement.Source.Path, root: placement.Root.ID, transform: placement.Transform}, remember)
}

func (p *Panel) enterContainingSource(root mapping.Root) {
	if p.stale || p.pending != nil || p.results != nil || p.operation.err != nil || !p.choiceFulfilled(root.ID) {
		p.sourceStatus = "Wait for a current preview before navigating its occurrences."
		return
	}
	if root.Parent == "" {
		p.returnToParent()
		return
	}
	if parent, ok := p.placement(root.Parent); ok {
		p.enterPlacement(parent, true)
	} else {
		p.sourceStatus = "Containing occurrence is unavailable."
	}
}

func (p *Panel) enterContext(frame contextFrame, remember bool) {
	p.navigation++
	id, published, environment, generation := p.navigation, p.current, p.environment, p.generation
	p.openingSource, p.sourceStatus = frame.path, "Opening source: "+filepath.Base(frame.path)
	valid := func() bool {
		return p.navigation == id && p.generation == generation && p.current == published && !p.stale && p.environment == environment && p.app.LoadedEnvironment() == environment && p.open
	}
	done := func(err error) {
		if p.navigation != id {
			return
		}
		p.openingSource = ""
		if !valid() {
			p.sourceStatus = "Source navigation superseded."
			return
		}
		if err != nil {
			p.sourceStatus = "Source open failed: " + err.Error()
			return
		}
		if remember && p.contextPath != "" {
			if placement, ok := p.placement(p.contextRoot); ok {
				previous := contextFrame{path: p.contextPath, root: p.contextRoot, transform: placement.Transform}
				if host, ok := p.app.(interface {
					MappingNavigationFrame(string) (string, render.Camera)
				}); ok {
					previous.lifetime, previous.camera = host.MappingNavigationFrame(previous.path)
				}
				p.history = append(p.history, previous)
			}
		}
		p.referencePath, p.contextPath, p.contextRoot = frame.path, frame.path, frame.root
		p.sourceStatus = ""
		if frame.lifetime != "" {
			if host, ok := p.app.(interface {
				RestoreMappingFrame(string, string, render.Camera)
			}); ok {
				host.RestoreMappingFrame(frame.path, frame.lifetime, frame.camera)
			}
		}
		p.queue(nil)
		p.operation.target = "source context"
	}
	if host, ok := p.app.(interface {
		OpenMappingSource(string, string, mapping.Transform, func() bool, func(error))
	}); ok {
		host.OpenMappingSource(p.parentPath, frame.path, frame.transform, valid, done)
	} else {
		// Legacy hosts can open a normal source but cannot acknowledge contextual
		// activation. Never invent a successful editing context for them.
		p.app.DoLoadResource(frame.path)
		p.openingSource = ""
		p.sourceStatus = "Source requested; contextual activation is unavailable."
	}
}

func (p *Panel) backContext() {
	for len(p.history) > 0 {
		frame := p.history[len(p.history)-1]
		p.history = p.history[:len(p.history)-1]
		if placement, ok := p.placement(frame.root); ok && viewKey(placement.Source.Path) == viewKey(frame.path) {
			if host, ok := p.app.(interface {
				MappingNavigationFrame(string) (string, render.Camera)
			}); ok {
				lifetime, _ := host.MappingNavigationFrame(frame.path)
				if lifetime == "" || lifetime != frame.lifetime {
					continue
				}
			}
			frame.transform = placement.Transform
			p.enterContext(frame, false)
			return
		}
	}
	p.returnToParent()
}

// ContextHeader stays on the source canvas even when the Composition dock or
// composed host overlay is hidden. The actual source workspace supplies dirty state.
func (p *Panel) ContextHeader(path string, dirty bool) {
	if p.contextPath == "" || viewKey(path) != viewKey(p.contextPath) {
		return
	}
	if imgui.SmallButton("Back") {
		p.backContext()
	}
	imgui.SameLine()
	if imgui.SmallButton("Back to map") {
		p.history = nil
		p.returnToParent()
	}
	crumb := filepath.Base(p.parentPath)
	for _, frame := range p.history {
		crumb += " > " + filepath.Base(frame.path)
	}
	imgui.TextWrapped(crumb + " > " + filepath.Base(path))
	label := "Editing: " + filepath.Base(path)
	if dirty {
		label += " · Unsaved changes"
	}
	imgui.TextWrapped(label)
	styles := []string{"Normal", "Dim", "Hidden"}
	imgui.SetNextItemWidth(120)
	if imgui.BeginCombo("Context", styles[p.contextStyle]) {
		for i, style := range styles {
			if imgui.Selectable(style) {
				p.contextStyle = i
			}
		}
		imgui.EndCombo()
	}
	if _, valid := p.placement(p.contextRoot); !valid && p.compose {
		imgui.TextWrapped("Context unavailable - this source is no longer in the displayed scenario.")
	} else if p.operation.err != nil {
		imgui.TextWrapped(fmt.Sprintf("Preview request failed: %v. Source edits remain applied.", p.operation.err))
	} else if p.stale || p.pending != nil || p.results != nil {
		imgui.TextWrapped("Updating composition from accepted source changes; context shows the previous result.")
	} else if p.contextStyle != 2 && p.contextView != nil && !p.contextView.Render().LevelReady(p.contextView.Render().Camera.Level) {
		imgui.TextWrapped("Preparing context geometry for this deck...")
	}
	if p.sourceStatus != "" {
		imgui.TextWrapped(p.sourceStatus)
	}
	imgui.Separator()
}
