package mappingui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/aphelion/mapview"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/imguiext"
	"sdmm/internal/util"
)

type App interface {
	LoadedEnvironment() *dmenv.Dme
	ActiveMappingPath() string
	DoLoadResource(string)
}
type acceptedProvider interface {
	MappingRevisionKey([]string) string
	CaptureMappingSources() map[string]mapping.AcceptedSource
}
type request struct {
	contextRoot            string
	focusRoot              string
	thumbnail, scanProject bool
	guidance               bool
	accepted               map[string]mapping.AcceptedSource
	generation             uint64
	environment            *dmenv.Dme
	parent, reference      string
	anchor                 *util.Point
	compose                bool
	scenario               mapping.Scenario
	mapConfig              string
	fixedChoices           map[string]int
}
type result struct {
	contextSource       *mapping.Source
	contextDisplay      *dmmap.Dmm
	configurations      []string
	layers              [2]*mapping.Source
	layerDisplays       [2]*dmmap.Dmm
	thumbnailSource     *mapping.Source
	thumbnailDisplay    *dmmap.Dmm
	thumbnailConnectors int
	discovered          []string
	advisory            *mapping.Advisory
	dependencies        []string
	request             request
	catalog             *mapping.Catalog
	sources             [2]*mapping.Source
	displays            [2]*dmmap.Dmm
	roots               []mapping.Root
	diagnostics         []mapping.Diagnostic
	err                 error
	transform           *mapping.Transform
	projection          *mapping.Projection
	fixed               []mapping.FixedBinding
	connectors          [2]int
}

func (r *result) close() {
	r.contextSource.Close()
	for _, s := range r.layers {
		s.Close()
	}
	r.advisory.Close()
	r.projection.Close()
	r.catalog.Close()
}

// Panel is a modeless source inspector. It owns no editable document, tools,
// clipboard or history; opening a source for editing is an explicit separate action.
type Panel struct {
	contextRoot                            string
	contextView                            *canvas.Canvas
	managed, moveArmed, revealRoot         bool
	rootFilter                             string
	draft                                  *anchorDraft
	split                                  float32
	targetDeck                             int32
	layerViews                             [2]*canvas.Canvas
	thumbnail                              *canvas.Canvas
	thumbnailRequested, scanNext           bool
	discovered                             []string
	browserFilter                          string
	guidance                               bool
	spawnSelection, spawnPage, spawnTarget int
	author                                 authoringUI
	app                                    App
	open                                   bool
	environment                            *dmenv.Dme
	generation                             uint64
	parentPath, referencePath, status      string
	pending                                *request
	results                                chan result
	cancel                                 context.CancelFunc
	current                                *result
	reuse                                  *mapping.ReuseCache
	views                                  [2]*canvas.Canvas
	visualCursor                           int
	camera                                 render.Camera
	level                                  int32
	offset                                 [3]int32
	anchor                                 *util.Point
	transformParent, transformReference    string
	mode                                   int32
	alpha                                  float32
	wipe                                   float32
	viewSize                               imgui.Vec2
	compose                                bool
	choices                                map[string]mapping.Choice
	mapConfig                              string
	fixedChoices                           map[string]int
	scenario                               mapping.Scenario
	showHelpers                            bool
	policyRevision                         uint64
	inspected                              util.Point
	observedRevisions                      string
	nextRevisionCheck                      time.Time
	retryAccepted                          bool
	contextPath, focusRoot                 string
	backdrop                               *canvas.Canvas
}

func New(app App) *Panel {
	return &Panel{app: app, choices: map[string]mapping.Choice{}, camera: render.Camera{Scale: .25, Level: 1}, level: 1, alpha: .5, wipe: .5, showHelpers: true}
}
func (p *Panel) Open() {
	p.open = true
	if p.parentPath == "" {
		p.parentPath = p.app.ActiveMappingPath()
	}
}

// OpenSources is the same explicit, read-only navigation used by the inspector's
// path controls. It never opens an editable workspace implicitly.
func (p *Panel) OpenSources(parent, reference string, anchor *util.Point) {
	if p.environment != p.app.LoadedEnvironment() {
		p.Invalidate()
		p.environment = p.app.LoadedEnvironment()
	}
	p.parentPath, p.referencePath = parent, reference
	p.open = true
	p.queue(anchor)
}

func (p *Panel) OpenReferenceForEditing() {
	if p.environment == nil || p.referencePath == "" {
		return
	}
	path := p.referencePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.environment.RootDir, path)
	}
	p.app.DoLoadResource(path)
	p.contextPath = path
}
func (p *Panel) clearContext() {
	p.contextPath = ""
	p.contextRoot = ""
	if p.contextView != nil {
		p.contextView.Dispose()
		p.contextView = nil
	}
	if p.backdrop != nil {
		p.backdrop.Dispose()
		p.backdrop = nil
	}
}
func (p *Panel) release() {
	if p.contextView != nil {
		p.contextView.Dispose()
		p.contextView = nil
	}
	for i, v := range p.layerViews {
		if v != nil {
			v.Dispose()
			p.layerViews[i] = nil
		}
	}
	if p.thumbnail != nil {
		p.thumbnail.Dispose()
		p.thumbnail = nil
	}
	if p.backdrop != nil {
		p.backdrop.Dispose()
		p.backdrop = nil
	}
	for i, v := range p.views {
		if v != nil {
			v.Dispose()
			p.views[i] = nil
		}
	}
	if p.current != nil {
		p.current.close()
		p.current = nil
	}
}
func (p *Panel) Invalidate() {
	p.draft = nil
	p.author.invalidate()
	p.generation++
	p.pending = nil
	if p.cancel != nil {
		p.cancel()
	}
	if results := p.results; results != nil {
		p.results = nil
		p.cancel = nil
		go func() { r := <-results; r.close() }()
	}
	p.release()
	if p.reuse != nil {
		p.reuse.Close()
		p.reuse = nil
	}
	p.choices = nil
	p.fixedChoices = nil
	p.scenario = mapping.Scenario{}
	p.contextPath = ""
	p.contextRoot = ""
	p.discovered = nil
	p.thumbnailRequested = false
	p.anchor = nil
	p.offset = [3]int32{}
	p.transformParent, p.transformReference = "", ""
}
func (p *Panel) queue(anchor *util.Point) {
	// Selection, binding, or accepted-source refresh invalidates a captured draft.
	p.draft = nil
	if p.transformParent != p.parentPath || p.transformReference != p.referencePath {
		p.anchor = nil
		p.offset = [3]int32{}
		p.transformParent, p.transformReference = p.parentPath, p.referencePath
	}
	if anchor != nil {
		copy := *anchor
		p.anchor = &copy
		p.offset = [3]int32{}
	}
	p.generation++
	p.pending = &request{generation: p.generation, environment: p.environment, parent: p.parentPath, reference: p.referencePath, anchor: p.anchor}
	p.pending.compose = p.compose
	p.pending.contextRoot = p.contextRoot
	p.pending.focusRoot = p.focusRoot
	p.pending.guidance = p.guidance
	p.pending.thumbnail = p.thumbnailRequested
	p.pending.scanProject = p.scanNext
	p.scanNext = false
	p.pending.scenario = p.scenario
	p.pending.scenario.Choices = maps.Clone(p.choices)
	p.pending.scenario.Excluded = maps.Clone(p.scenario.Excluded)
	p.pending.mapConfig = p.mapConfig
	p.pending.fixedChoices = maps.Clone(p.fixedChoices)
	if provider, ok := p.app.(acceptedProvider); ok {
		p.pending.accepted = provider.CaptureMappingSources()
		paths := []string{p.parentPath, p.referencePath}
		if p.current != nil {
			paths = p.current.dependencies
		}
		p.observedRevisions = provider.MappingRevisionKey(paths)
		// An accepted base revision is an intentional editor change. Occurrence
		// identities still invalidate any pins whose source or binding changed.
		p.pending.scenario.BaseHash = ""
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.status = "Loading source snapshots…"
}
func (p *Panel) advance() {
	p.author.advance()
	if p.environment != p.app.LoadedEnvironment() {
		p.Invalidate()
		p.environment = p.app.LoadedEnvironment()
		p.status = "Project changed; reopen references for this environment."
	}
	if p.open && (p.current != nil || p.retryAccepted) {
		paths := []string{p.parentPath, p.referencePath}
		if p.current != nil {
			paths = p.current.dependencies
		}
		if provider, ok := p.app.(acceptedProvider); ok && ((p.retryAccepted && time.Now().After(p.nextRevisionCheck)) || provider.MappingRevisionKey(paths) != p.observedRevisions) {
			p.nextRevisionCheck = time.Now().Add(100 * time.Millisecond)
			p.queue(nil)
		}
	}
	if p.results != nil {
		select {
		case r := <-p.results:
			p.results = nil
			p.cancel = nil
			if r.request.generation != p.generation || r.request.environment != p.environment || !p.open {
				r.close()
			} else if r.err != nil {
				r.close()
				p.status = r.err.Error()
				p.retryAccepted = errors.Is(r.err, mapping.ErrAcceptedDeferred)
			} else if !r.catalog.AcceptedCurrent() {
				r.close()
				p.queue(nil)
			} else {
				p.release()
				p.current = &r
				if r.contextDisplay != nil {
					v := canvas.New()
					v.Render().SetUnitProcessor(p)
					v.Render().BeginLevelBuild(r.contextDisplay, 1)
					p.contextView = v
				}
				if p.mapConfig == "" && r.request.mapConfig != "" {
					p.mapConfig = r.request.mapConfig
				}
				for i, d := range r.layerDisplays {
					if d != nil {
						v := canvas.New()
						v.SetTransparent(i == 0)
						v.Render().SetUnitProcessor(p)
						v.Render().BeginLevelBuild(d, 1)
						p.layerViews[i] = v
					}
				}
				if r.request.scanProject {
					p.discovered = r.discovered
				}
				if r.thumbnailDisplay != nil {
					p.thumbnail = canvas.New()
					p.thumbnail.Render().SetUnitProcessor(p)
					p.thumbnail.Render().BeginLevelBuild(r.thumbnailDisplay, 1)
				}
				p.retryAccepted = false
				if provider, ok := p.app.(acceptedProvider); ok {
					p.observedRevisions = provider.MappingRevisionKey(r.dependencies)
				}
				if r.request.compose {
					p.offset = [3]int32{}
					p.scenario = r.projection.Scenario
					p.choices = maps.Clone(p.scenario.Choices)
				}
				if r.transform != nil {
					p.offset = [3]int32{int32(r.transform.Offset.X), int32(r.transform.Offset.Y), int32(r.transform.Offset.Z)}
					p.level = int32(r.request.anchor.Z)
					p.camera.ShiftX = -float32(r.request.anchor.X*dmmap.WorldIconSize) + 500
					p.camera.ShiftY = -float32(r.request.anchor.Y*dmmap.WorldIconSize) + 500
				} else if r.request.anchor != nil {
					p.offset = [3]int32{}
				}
				for i, d := range r.displays {
					if d != nil {
						p.views[i] = canvas.New()
						p.views[i].Render().SetUnitProcessor(p)
						p.views[i].Render().BeginLevelBuild(d, 1)
					}
				}
				p.status = "Locked reference context; open source documents use accepted revisions. Runtime initialization and asynchronous order are not simulated."
			}
		default:
		}
	}
	if p.results == nil && p.pending != nil && p.environment != nil && p.open {
		r := *p.pending
		p.pending = nil
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		results := make(chan result, 1)
		p.results = results
		if p.reuse == nil {
			p.reuse = mapping.NewReuseCache()
		}
		reuse := p.reuse
		go func() {
			out := result{request: r, catalog: mapping.NewCatalogWithReuseCache(r.environment, reuse)}
			out.catalog.SetAcceptedSources(r.accepted)
			out.request.accepted = nil
			if r.scanProject {
				var err error
				out.discovered, err = mapping.ScanProject(ctx, r.environment.RootDir)
				if err != nil {
					out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "warning", Code: "project-scan", Message: err.Error()})
				}
			}
			for i, path := range []string{r.parent, r.reference} {
				if i == 1 && r.compose {
					continue
				}
				if path == "" {
					continue
				}
				if !filepath.IsAbs(path) {
					path = filepath.Join(r.environment.RootDir, path)
				}
				out.sources[i], out.err = out.catalog.Load(ctx, path)
				if out.err != nil {
					break
				}
				out.displays[i], out.err = out.sources[i].DisplayMap(ctx)
				if out.err != nil {
					break
				}
			}
			if out.err == nil && out.sources[0] != nil {
				var rootDiagnostics []mapping.Diagnostic
				out.roots, rootDiagnostics = out.catalog.Roots(ctx, out.sources[0], mapping.Transform{}, "")
				if !r.compose {
					out.diagnostics = append(out.diagnostics, rootDiagnostics...)
				}
				if r.compose {
					var fixed []mapping.FixedPlacement
					var fixedDiagnostics []mapping.Diagnostic
					if r.mapConfig == "" {
						out.configurations, _ = mapping.DiscoverMapConfigurations(ctx, r.environment.RootDir, out.sources[0].Identity.Path)
						if len(out.configurations) == 1 {
							r.mapConfig = out.configurations[0]
							out.request.mapConfig = r.mapConfig
						}
					}
					if r.mapConfig != "" {
						fixed, out.fixed, fixedDiagnostics = out.catalog.Fixed(ctx, out.sources[0], r.mapConfig, r.fixedChoices)
					}
					out.projection, out.err = out.catalog.Compose(ctx, out.sources[0], r.scenario, fixed)
					if out.err == nil {
						out.sources[1] = out.projection.Source
						out.displays[1], out.err = out.sources[1].DisplayMap(ctx)
						out.roots = out.projection.Roots
						out.diagnostics = append(out.diagnostics, out.projection.Diagnostics...)
						out.diagnostics = append(out.diagnostics, fixedDiagnostics...)
						if r.mapConfig == "" {
							out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "warning", Code: "partial", Message: "Modular-only scenario: select a map configuration to resolve fixed templates and reservations."})
						}
					}
				}
			}
			if out.err == nil && !r.compose && r.anchor != nil && out.sources[1] != nil {
				transform, err := mapping.Anchor(out.sources[1], *r.anchor)
				if err != nil {
					out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "error", Code: "anchor", Message: err.Error()})
				} else {
					out.transform = &transform
				}
			}
			if out.err == nil && r.thumbnail && r.reference != "" {
				path := r.reference
				if !filepath.IsAbs(path) {
					path = filepath.Join(r.environment.RootDir, path)
				}
				var err error
				out.thumbnailSource, err = out.catalog.Load(ctx, path)
				if err == nil {
					out.thumbnailConnectors = len(out.thumbnailSource.Connectors())
					size := out.thumbnailSource.Size
					if size.X*size.Y > 65536 {
						err = fmt.Errorf("thumbnail exceeds 65536-cell limit; use the full reference view")
					} else {
						out.thumbnailDisplay, err = out.thumbnailSource.DisplayMap(ctx)
					}
				}
				if err != nil {
					out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "warning", Code: "thumbnail", Message: err.Error(), Source: path})
				}
			}
			if ctx.Err() != nil {
				out.err = ctx.Err()
			}
			if out.err == nil && r.guidance && out.sources[0] != nil {
				source := out.sources[0]
				if out.projection != nil {
					source = out.projection.Source
					seams, err := out.projection.AnalyzeSeams(ctx)
					if err != nil {
						out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "warning", Code: "seam-unavailable", Message: err.Error()})
					} else {
						out.diagnostics = append(out.diagnostics, seams...)
					}
				}
				var err error
				out.advisory, err = source.AnalyzeSpawns(ctx, out.catalog.MapName())
				if err != nil {
					out.diagnostics = append(out.diagnostics, mapping.Diagnostic{Severity: "warning", Code: "spawn-unavailable", Message: err.Error()})
				} else {
					out.diagnostics = append(out.diagnostics, out.advisory.Diagnostics...)
				}
			}
			if err := out.catalog.DeferredError(); err != nil {
				out.err = err
			}
			if out.err == nil {
				for i, source := range out.sources {
					if source != nil {
						out.connectors[i] = len(source.Connectors())
					}
				}
			}
			out.dependencies = out.catalog.SourcePaths()
			if out.err == nil && out.projection != nil && r.contextRoot != "" && r.contextRoot != r.focusRoot {
				out.contextSource, out.err = out.projection.OccurrenceLayer(ctx, r.contextRoot, false)
				if out.err == nil {
					out.contextDisplay, out.err = out.contextSource.DisplayMap(ctx)
				}
			}
			if out.err == nil && out.projection != nil && r.focusRoot != "" {
				for i := range 2 {
					out.layers[i], out.err = out.projection.OccurrenceLayer(ctx, r.focusRoot, i == 0)
					if out.err != nil {
						break
					}
					out.layerDisplays[i], out.err = out.layers[i].DisplayMap(ctx)
					if out.err != nil {
						break
					}
				}
			}
			results <- out
		}()
	}
}

func (p *Panel) Process() {
	p.advance()
	if !p.open {
		return
	}
	visible := imgui.BeginV("Composition", &p.open, imgui.WindowFlagsNone)
	if visible {
		p.sidebar()
	}
	imgui.End()
	if !p.open {
		p.Invalidate()
	}
}
func (p *Panel) controls() {
	if host, ok := p.app.(interface{ RestoreCompositionDock() }); ok && imgui.Button("Restore Composition beside Environment") {
		host.RestoreCompositionDock()
	}
	if p.environment == nil {
		imgui.TextWrapped("Open a project to resolve source appearances and metadata.")
		return
	}
	if !p.managed {
		imgui.InputText("Parent source", &p.parentPath)
	}
	imgui.InputText("Explicit reference source", &p.referencePath)
	imgui.InputText("Map configuration", &p.mapConfig)
	if imgui.Checkbox("Compose configured modules", &p.compose) {
		p.queue(nil)
	}
	if imgui.Button("Refresh source snapshots") {
		p.queue(nil)
	}
	if imgui.Checkbox("Show source helpers", &p.showHelpers) {
		p.policyRevision++
	}
	if p.current != nil && len(p.current.fixed) > 0 && imgui.TreeNode("Fixed templates / trait-relative placements") {
		for _, binding := range p.current.fixed {
			imgui.PushID(binding.ID)
			imgui.TextWrapped(fmt.Sprintf("%s: %s[%d] → local %v — %s", binding.ID, binding.TraitName, binding.TraitIndex, binding.Destination, binding.Error))
			slot := int32(binding.Slot + 1)
			if imgui.InputInt("Candidate slot", &slot) {
				if p.fixedChoices == nil {
					p.fixedChoices = map[string]int{}
				}
				p.fixedChoices[binding.ID] = int(slot) - 1
				p.queue(nil)
			}
			imgui.PopID()
		}
		imgui.TextWrapped("Changing a runtime origin uses the guarded configuration authoring controls below.")
		imgui.TreePop()
	}
	if !p.compose {
		imgui.TextWrapped("Reference offsets are editor-only alignment; they do not change runtime placement.")
		if imgui.InputInt("Reference offset X", &p.offset[0]) {
			p.anchor = nil
		}
		if imgui.InputInt("Reference offset Y", &p.offset[1]) {
			p.anchor = nil
		}
		if imgui.InputInt("Reference offset Z", &p.offset[2]) {
			p.anchor = nil
		}
		if imgui.Button("Open reference for editing") {
			p.OpenReferenceForEditing()
		}
	}
	p.browserControls()
	if p.current != nil {
		p.provenanceControls()
	}
	if imgui.Checkbox("Analyze authored seams and spawn candidates", &p.guidance) {
		p.queue(nil)
	}
	if p.current != nil {
		p.advisoryControls()
	}
	p.authoringControls()
}

func (p *Panel) ProcessLevelBuildBudget(b *render.LevelBuildBudget) bool {
	for range 7 {
		idx := p.visualCursor % 7
		p.visualCursor++
		v := p.backdrop
		if idx == 3 {
			v = p.thumbnail
		}
		if idx < 2 {
			v = p.views[idx]
		}
		if idx >= 4 && idx < 6 {
			v = p.layerViews[idx-4]
		}
		if idx == 6 {
			v = p.contextView
		}
		if v != nil && v.Render().ProcessLevelBuildBudget(b) {
			return true
		}
	}
	return false
}

// Backdrop supplies pixels only. The editable pane's tools, picking, bounds and
// command executor remain attached exclusively to its own source document.
func (p *Panel) Backdrop(path string, camera render.Camera, size imgui.Vec2) (uint32, bool) {
	if !p.open || p.current == nil || p.current.displays[0] == nil || p.contextPath == "" || !strings.EqualFold(filepath.Clean(path), filepath.Clean(p.contextPath)) {
		return 0, false
	}
	offset := util.Point{X: int(p.offset[0]), Y: int(p.offset[1]), Z: int(p.offset[2])}
	if p.current.projection != nil {
		found := false
		for _, placement := range p.current.projection.Placements {
			if placement.Root.ID == p.contextRoot && strings.EqualFold(filepath.Clean(placement.Source.Path), filepath.Clean(path)) {
				offset = placement.Transform.Offset
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	contextView, contextDisplay := p.contextView, p.current.contextDisplay
	if p.contextRoot == p.current.request.focusRoot {
		contextView, contextDisplay = p.layerViews[1], p.current.layerDisplays[1]
	}
	if p.current.projection != nil && contextView != nil && p.current.request.contextRoot == p.contextRoot {
		camera.ShiftX -= float32(offset.X * dmmap.WorldIconSize)
		camera.ShiftY -= float32(offset.Y * dmmap.WorldIconSize)
		camera.Level += offset.Z
		v := contextView
		*v.Render().Camera = camera
		if camera.Level < 1 || camera.Level > contextDisplay.MaxZ {
			return 0, false
		}
		v.Render().SetActiveLevel(contextDisplay, camera.Level)
		v.Process(size)
		return v.Texture(), true
	}
	if p.current.projection != nil {
		return 0, false
	}
	if p.backdrop == nil {
		p.backdrop = canvas.New()
		p.backdrop.Render().SetUnitProcessor(p)
	}
	camera.ShiftX -= float32(offset.X * dmmap.WorldIconSize)
	camera.ShiftY -= float32(offset.Y * dmmap.WorldIconSize)
	camera.Level += offset.Z
	*p.backdrop.Render().Camera = camera
	if camera.Level < 1 || camera.Level > p.current.displays[0].MaxZ {
		return 0, false
	}
	p.backdrop.Render().SetActiveLevel(p.current.displays[0], camera.Level)
	p.backdrop.Process(size)
	return p.backdrop.Texture(), true
}

func (p *Panel) compareViews() {
	if p.current == nil || p.views[0] == nil {
		return
	}
	size := imgui.ContentRegionAvail()
	if size.X < 1 || size.Y < 1 {
		return
	}
	start := imgui.CursorScreenPos()
	mode := p.mode
	minimum := max(float32(160), 16*imgui.FontSize())
	if mode == 0 && size.X < minimum*2+8 {
		mode = 7
	}
	if p.split == 0 {
		p.split = .5
	}
	widths := [2]float32{size.X, size.X}
	if mode == 0 {
		widths[0] = max(minimum, min(size.X-minimum-8, (size.X-8)*p.split))
		widths[1] = size.X - widths[0] - 8
		imgui.SetCursorScreenPos(imgui.Vec2{X: start.X + widths[0], Y: start.Y})
		imgui.InvisibleButton("comparison-splitter", imgui.Vec2{X: 8, Y: size.Y})
		if imgui.IsItemActive() && imgui.IsMouseDragging(0, 0) {
			p.split = max(.1, min(.9, p.split+imgui.CurrentIO().MouseDelta().X/(size.X-8)))
		}
	}
	imgui.SetCursorScreenPos(start)
	imgui.InvisibleButton("comparison-canvas", size)
	if imgui.IsItemHovered() {
		origin := start
		paneSize := size
		if mode == 0 {
			paneSize.X = widths[0]
			if imgui.MousePos().X > start.X+widths[0]+8 {
				origin.X += widths[0] + 8
				paneSize.X = widths[1]
			}
		}
		p.camera.Level = int(p.level)
		if imgui.IsMouseClicked(0) {
			p.inspected = mapview.Tile(p.camera, paneSize, origin, imgui.MousePos())
			for _, root := range p.current.roots {
				if root.Destination == p.inspected {
					p.focusRoot = root.ID
					p.revealRoot = true
					p.moveArmed = false
					p.queue(nil)
					break
				}
			}
		}
		_, wheel := imgui.CurrentIO().MouseWheel()
		altScroll := false
		if host, ok := p.app.(interface{ Prefs() *prefs.Prefs }); ok {
			altScroll = host.Prefs().Controls.AltScrollBehaviour
		}
		space := imgui.IsKeyDown(int(glfw.KeySpace))
		if wheel != 0 {
			if altScroll && !space {
				delta := wheel * float32(dmmap.WorldIconSize)
				if imguiext.IsShiftDown() {
					delta *= 5
				}
				move := imgui.Vec2{Y: delta}
				if imguiext.IsCtrlDown() {
					move = imgui.Vec2{X: delta}
				}
				mapview.Pan(&p.camera, move)
			} else {
				mapview.Zoom(&p.camera, paneSize, imgui.MousePos().Minus(origin), wheel > 0)
			}
		}
		if imgui.IsMouseDown(2) || space {
			mapview.Pan(&p.camera, imgui.CurrentIO().MouseDelta())
		}
	}
	p.viewSize = imgui.Vec2{X: widths[0], Y: size.Y}
	for i, v := range p.views {
		if v == nil {
			continue
		}
		camera := p.camera
		camera.Level = int(p.level)
		if i == 1 {
			camera.ShiftX += float32(p.offset[0] * int32(dmmap.WorldIconSize))
			camera.ShiftY += float32(p.offset[1] * int32(dmmap.WorldIconSize))
			camera.Level -= int(p.offset[2])
		}
		*v.Render().Camera = camera
		if camera.Level > 0 && camera.Level <= p.current.displays[i].MaxZ {
			v.Render().SetActiveLevel(p.current.displays[i], camera.Level)
		}
		v.Process(imgui.Vec2{X: widths[i], Y: size.Y})
	}
	draw := imgui.WindowDrawList()
	end := start.Plus(p.viewSize)
	draw.PushClipRect(start, start.Plus(size))
	defer draw.PopClipRect()
	show := func(i int, from, to imgui.Vec2, alpha float32) {
		if p.views[i] != nil {
			draw.AddImageV(imgui.TextureID(p.views[i].Texture()), from, to, imgui.Vec2{Y: 1}, imgui.Vec2{X: 1}, imgui.PackedColorFromVec4(imgui.Vec4{X: 1, Y: 1, Z: 1, W: alpha}))
		}
	}
	if mode == 7 {
		show(1, start, start.Plus(size), 1)
		return
	}
	show(0, start, end, 1)
	switch mode {
	case 0:
		from := imgui.Vec2{X: end.X + 8, Y: start.Y}
		show(1, from, imgui.Vec2{X: from.X + widths[1], Y: end.Y}, 1)
	case 1:
		show(1, start, end, p.alpha)
	case 2:
		draw.PushClipRect(imgui.Vec2{X: start.X + size.X*p.wipe, Y: start.Y}, end)
		show(1, start, end, 1)
		draw.PopClipRect()
	case 3:
		if time.Now().UnixMilli()/700%2 == 1 {
			show(1, start, end, 1)
		}
	case 4:
		p.drawDifferences(start, end)
	case 5:
		p.drawBounds(start)
	case 6:
		p.drawReservations(start, end)
	}
	if p.views[0].Render().LevelLoading() {
		draw.AddText(start, 0xffffffff, "Preparing source geometry...")
	}
}

func (p *Panel) navigate(point util.Point) {
	if p.managed {
		if host, ok := p.app.(mapHost); ok {
			host.FrameMappingSource(p.parentPath, point)
			return
		}
	}
	p.level = int32(point.Z)
	p.inspected = point
	p.camera.ShiftX = -float32((point.X-1)*dmmap.WorldIconSize) + 100/p.camera.Scale
	p.camera.ShiftY = -float32((point.Y-1)*dmmap.WorldIconSize) + 100/p.camera.Scale
}

func (p *Panel) ProcessUnit(u unit.Unit) bool {
	if provider, ok := p.app.(interface{ MappingPathVisible(string) bool }); ok && u.Instance() != nil && !provider.MappingPathVisible(u.Instance().Prefab().Path()) {
		return false
	}
	if p.showHelpers || u.Instance() == nil {
		return true
	}
	path := u.Instance().Prefab().Path()
	for object, depth := p.environment.Objects[path], 0; object != nil && depth < 512; object, depth = object.Parent(), depth+1 {
		if object.Path == "/obj/modular_map_root" || object.Path == "/obj/modular_map_connector" {
			return false
		}
	}
	return path != "/obj/modular_map_root" && path != "/obj/modular_map_connector"
}
func (p *Panel) RenderPolicyRevision() uint64 {
	if provider, ok := p.app.(interface{ MappingFilterRevision() uint64 }); ok {
		return p.policyRevision + provider.MappingFilterRevision()
	}
	return p.policyRevision
}

func (p *Panel) screen(point util.Point, start imgui.Vec2) imgui.Vec2 {
	return mapview.Screen(p.camera, p.viewSize, start, point)
}
