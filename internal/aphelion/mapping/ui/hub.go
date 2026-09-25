package mappingui

import (
	"path/filepath"
	"strings"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/util"
)

type mapHost interface {
	MappingDocuments() []string
	FrameMappingSource(string, util.Point)
	MoveMappingRoot(mapping.Root, util.Point, bool) error
	OpenMappingContext(string, string, mapping.Transform)
	OpenMappingComparison(*Panel)
}

// Hub routes views to document-owned sessions. Sidebar visibility never owns
// requests, accepted source snapshots, choices or canvas resources.
type Hub struct {
	component.Component
	app            App
	sessions       map[string]*Panel
	contextCameras map[string]render.Camera
	cursor         int
}

func NewHub(app App) *Hub { return &Hub{app: app, sessions: map[string]*Panel{}} }
func (h *Hub) SetContextCamera(parent, path string, camera render.Camera) {
	if h.contextCameras == nil {
		h.contextCameras = map[string]render.Camera{}
	}
	h.contextCameras[viewKey(path)] = camera
	// Only one chosen occurrence supplies context for a reused source tab.
	for _, p := range h.sessions {
		if viewKey(p.parentPath) != viewKey(parent) && viewKey(p.contextPath) == viewKey(path) {
			p.clearContext()
		}
	}
}
func (h *Hub) TakeContextCamera(path string) (render.Camera, bool) {
	camera, ok := h.contextCameras[viewKey(path)]
	delete(h.contextCameras, viewKey(path))
	return camera, ok
}
func viewKey(path string) string { return strings.ToLower(filepath.Clean(path)) }
func (h *Hub) session(path string) *Panel {
	key := viewKey(path)
	if p := h.sessions[key]; p != nil {
		return p
	}
	p := New(h.app)
	p.parentPath, p.environment = path, h.app.LoadedEnvironment()
	p.managed = true
	h.sessions[key] = p
	return p
}
func (h *Hub) Open() {
	if path := h.app.ActiveMappingPath(); path != "" {
		p := h.session(path)
		p.Open()
		p.compose = true
		if p.current == nil {
			p.queue(nil)
		}
	}
}
func (h *Hub) CloseSource(path string) {
	delete(h.contextCameras, viewKey(path))
	if p := h.sessions[viewKey(path)]; p != nil {
		p.open = false
		p.Invalidate()
		delete(h.sessions, viewKey(path))
	}
	for _, p := range h.sessions {
		if viewKey(p.contextPath) == viewKey(path) {
			p.clearContext()
		}
	}
}
func (h *Hub) Invalidate() {
	clear(h.contextCameras)
	for key, p := range h.sessions {
		p.open = false
		p.Invalidate()
		delete(h.sessions, key)
	}
}
func (h *Hub) Advance() {
	if host, ok := h.app.(mapHost); ok {
		open := map[string]bool{}
		for _, path := range host.MappingDocuments() {
			open[viewKey(path)] = true
		}
		for key, p := range h.sessions {
			if !open[key] {
				h.CloseSource(p.parentPath)
			}
		}
	}
	for _, p := range h.sessions {
		p.advance()
	}
}
func (h *Hub) Process(int32) {
	path := h.app.ActiveMappingPath()
	if path == "" {
		imgui.TextWrapped("Open a map to inspect its composition.")
		return
	}
	p := h.forView(path)
	if p == nil {
		p = h.session(path)
	}
	p.sidebar()
}
func (h *Hub) forView(path string) *Panel {
	// Explicit source-in-context selection takes precedence over a template's
	// own standalone composition, without creating a second editable document.
	for _, p := range h.sessions {
		if p.open && p.contextPath != "" && viewKey(p.contextPath) == viewKey(path) {
			return p
		}
	}
	return h.sessions[viewKey(path)]
}
func (h *Hub) Backdrop(path string, camera render.Camera, size imgui.Vec2) (uint32, bool) {
	if p := h.forView(path); p != nil {
		return p.Backdrop(path, camera, size)
	}
	return 0, false
}
func (h *Hub) Draw(path string, camera render.Camera, size, origin imgui.Vec2) {
	if p := h.forView(path); p != nil && viewKey(p.parentPath) == viewKey(path) {
		p.drawInline(camera, size, origin)
	}
}
func (h *Hub) VisibleInMap(path string) bool {
	p := h.forView(path)
	return p != nil && viewKey(p.parentPath) == viewKey(path) && p.open && p.compose && p.current != nil
}
func (h *Hub) Handle(path string, point util.Point, active, pressed, released, cancel, focused bool, tool string) bool {
	if p := h.forView(path); p != nil && viewKey(p.parentPath) == viewKey(path) {
		return p.handleInline(point, active, pressed, released, cancel, focused, tool)
	}
	return false
}
func (h *Hub) SelectedRoot(path string) (mapping.Root, bool) {
	if p := h.sessions[viewKey(path)]; p != nil {
		return p.selectedRoot()
	}
	return mapping.Root{}, false
}
func (h *Hub) ArmAnchorMove(path string) bool {
	if p := h.sessions[viewKey(path)]; p != nil {
		return p.armAnchorMove()
	}
	return false
}
func (h *Hub) CancelDraft(path string) {
	if p := h.sessions[viewKey(path)]; p != nil && p.draft != nil {
		p.draft = nil
		p.moveArmed = false
		p.status = "Anchor draft cancelled"
	}
}
func (h *Hub) TileLocked(path string, point util.Point) bool {
	p := h.forView(path)
	return p != nil && viewKey(p.parentPath) == viewKey(path) && p.open && p.compose && p.derivedCell(point)
}

// EditFence captures immutable provenance for existing asynchronous edit workers.
func (h *Hub) EditFence(path string) func(util.Point) bool {
	p := h.forView(path)
	if p == nil || viewKey(p.parentPath) != viewKey(path) || !p.open || !p.compose || p.current == nil || p.current.projection == nil {
		return nil
	}
	provenance := p.current.projection.Provenance
	return func(point util.Point) bool { return derivedProvenance(provenance[point]) }
}
func (h *Hub) ProcessLevelBuildBudget(b *render.LevelBuildBudget) bool {
	// Rotate across sources, including sessions whose sidebar tab is hidden.
	host, ok := h.app.(mapHost)
	if !ok {
		return false
	}
	paths := host.MappingDocuments()
	for range len(paths) {
		index := h.cursor % len(paths)
		h.cursor++
		if p := h.sessions[viewKey(paths[index])]; p != nil && p.ProcessLevelBuildBudget(b) {
			return true
		}
	}
	return false
}
