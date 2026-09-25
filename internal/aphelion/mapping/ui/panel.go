package mappingui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
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

func (r *result) close() { r.advisory.Close(); r.projection.Close(); r.catalog.Close() }

// Panel is a modeless source inspector. It owns no editable document, tools,
// clipboard or history; opening a source for editing is an explicit separate action.
type Panel struct {
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
	return &Panel{app: app, camera: render.Camera{Scale: .25, Level: 1}, level: 1, alpha: .5, wipe: .5, showHelpers: true}
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
func (p *Panel) release() {
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
	p.author.invalidate()
	p.generation++
	p.pending = nil
	if p.cancel != nil {
		p.cancel()
	}
	p.release()
	p.choices = nil
	p.fixedChoices = nil
	p.scenario = mapping.Scenario{}
	p.contextPath = ""
	p.discovered = nil
	p.thumbnailRequested = false
	p.anchor = nil
	p.offset = [3]int32{}
	p.transformParent, p.transformReference = "", ""
}
func (p *Panel) queue(anchor *util.Point) {
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
	if p.open && (p.current != nil || p.retryAccepted) && time.Now().After(p.nextRevisionCheck) {
		p.nextRevisionCheck = time.Now().Add(500 * time.Millisecond)
		paths := []string{p.parentPath, p.referencePath}
		if p.current != nil {
			paths = p.current.dependencies
		}
		if provider, ok := p.app.(acceptedProvider); ok && (p.retryAccepted || provider.MappingRevisionKey(paths) != p.observedRevisions) {
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
		go func() {
			out := result{request: r, catalog: mapping.NewCatalog(r.environment)}
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
			results <- out
		}()
	}
}

func (p *Panel) Process() {
	p.advance()
	if !p.open {
		return
	}
	imgui.SetNextWindowSizeV(imgui.Vec2{X: 1000, Y: 680}, imgui.ConditionFirstUseEver)
	visible := imgui.BeginV("Composition Inspector", &p.open, imgui.WindowFlagsNone)
	if visible {
		p.controls()
		p.compareViews()
	}
	imgui.End()
	if !p.open {
		p.Invalidate()
	}
}
func (p *Panel) controls() {
	imgui.TextWrapped(p.status)
	if p.environment == nil {
		imgui.Text("Open a project to resolve source appearances and metadata.")
		return
	}
	imgui.InputText("Parent source", &p.parentPath)
	imgui.SameLine()
	if imgui.Button("Use active map") {
		p.parentPath = p.app.ActiveMappingPath()
		p.scenario = mapping.Scenario{}
		p.choices = nil
	}
	imgui.InputText("Reference source", &p.referencePath)
	imgui.InputText("Map configuration (project-relative JSON)", &p.mapConfig)
	if imgui.Checkbox("Assemble selected scenario", &p.compose) {
		p.queue(nil)
	}
	if imgui.Checkbox("Analyze authored seams and spawn candidates", &p.guidance) {
		p.queue(nil)
	}
	if imgui.Button("Open / refresh references") {
		p.OpenSources(p.parentPath, p.referencePath, nil)
	}
	p.browserControls()
	imgui.SameLine()
	if imgui.Button("Open reference for editing") && p.referencePath != "" {
		p.OpenReferenceForEditing()
	}
	if p.contextPath != "" {
		imgui.SameLine()
		if imgui.Button("Detach locked parent context") {
			p.contextPath = ""
			if p.backdrop != nil {
				p.backdrop.Dispose()
				p.backdrop = nil
			}
		}
	}
	modes := []string{"Synchronized split", "Overlay", "Wipe", "Blink", "Structural differences", "Source bounds", "Reservations / suppression"}
	if imgui.BeginCombo("Comparison", modes[p.mode]) {
		for i, mode := range modes {
			if imgui.Selectable(mode) {
				p.mode = int32(i)
			}
		}
		imgui.EndCombo()
	}
	imgui.SliderFloat("Blend", &p.alpha, 0, 1)
	imgui.SameLine()
	imgui.SliderFloat("Wipe", &p.wipe, 0, 1)
	imgui.InputInt("Destination Z", &p.level)
	p.level = max(1, p.level)
	if imgui.InputInt("Offset X", &p.offset[0]) {
		p.anchor = nil
	}
	if imgui.InputInt("Offset Y", &p.offset[1]) {
		p.anchor = nil
	}
	if imgui.InputInt("Offset Z", &p.offset[2]) {
		p.anchor = nil
	}
	if imgui.Checkbox("Show source helpers", &p.showHelpers) {
		p.policyRevision++
	}
	if p.current != nil {
		if len(p.current.fixed) > 0 && imgui.TreeNode("Fixed templates / trait-relative placements") {
			for _, binding := range p.current.fixed {
				imgui.PushID(binding.ID)
				imgui.TextWrapped(fmt.Sprintf("%s: %s[%d] -> local %d,%d,%d — %s", binding.ID, binding.TraitName, binding.TraitIndex, binding.Destination.X, binding.Destination.Y, binding.Destination.Z, binding.Error))
				slot := int32(binding.Slot + 1)
				if imgui.InputInt("Candidate slot", &slot) {
					if p.fixedChoices == nil {
						p.fixedChoices = make(map[string]int)
					}
					p.fixedChoices[binding.ID] = int(slot) - 1
					p.queue(nil)
				}
				imgui.PopID()
			}
			imgui.TreePop()
		}
		for i, s := range p.current.sources {
			if s == nil {
				continue
			}
			imgui.Text(fmt.Sprintf("%d: %s — %d×%d×%d; %d connectors", i+1, filepath.Base(s.Identity.Path), s.Size.X, s.Size.Y, s.Size.Z, p.current.connectors[i]))
		}
		if imgui.TreeNode("Placed modular roots") {
			for _, root := range p.current.roots {
				imgui.PushID(root.ID)
				if imgui.TreeNode(fmt.Sprintf("%s at %d,%d,%d (%d slots)", root.Key, root.Destination.X, root.Destination.Y, root.Destination.Z, len(root.Candidates))) {
					imgui.TextWrapped(root.Config + " / " + root.Key)
					p.alternativeControls(root)
					if imgui.Button("Go to root") {
						p.navigate(root.Destination)
					}
					if p.compose {
						excluded := p.scenario.Excluded[root.ID]
						if imgui.Checkbox("Exclude (editor what-if)", &excluded) {
							if p.scenario.Excluded == nil {
								p.scenario.Excluded = make(map[string]bool)
							}
							p.scenario.Excluded[root.ID] = excluded
							p.queue(nil)
						}
					}
					for _, candidate := range root.Candidates {
						label := fmt.Sprintf("%d: %s", candidate.Slot+1, filepath.Base(candidate.Path))
						if candidate.Error != "" {
							imgui.TextWrapped(label + ": " + candidate.Error)
							continue
						}
						if imgui.Selectable(label) {
							p.referencePath = candidate.Path
							p.focusRoot = root.ID
							if p.choices == nil {
								p.choices = make(map[string]mapping.Choice)
							}
							p.choices[root.ID] = mapping.Choice{Slot: candidate.Slot}
							anchor := root.Destination
							p.queue(&anchor)
						}
					}
					imgui.TreePop()
				}
				imgui.PopID()
			}
			imgui.TreePop()
		}
		if imgui.TreeNode(fmt.Sprintf("Diagnostics (%d)", len(p.current.diagnostics))) {
			imgui.BeginChildV("diagnostic-list", imgui.Vec2{Y: 150}, true, imgui.WindowFlagsNone)
			for i, d := range p.current.diagnostics {
				imgui.PushIDInt(i)
				if d.Destination.Z > 0 && imgui.SmallButton("Go") {
					p.navigate(d.Destination)
				}
				imgui.SameLine()
				imgui.TextWrapped(d.Severity + ": " + d.Message)
				if imgui.IsItemHovered() {
					imgui.SetTooltip(fmt.Sprintf("%s\nsource %d,%d,%d; destination %d,%d,%d", d.Source, d.Local.X, d.Local.Y, d.Local.Z, d.Destination.X, d.Destination.Y, d.Destination.Z))
				}
				imgui.PopID()
			}
			imgui.EndChild()
			imgui.TreePop()
		}
		p.provenanceControls()
		p.advisoryControls()
	}
	p.authoringControls()
}

func (p *Panel) ProcessLevelBuildBudget(b *render.LevelBuildBudget) bool {
	for range 4 {
		idx := p.visualCursor % 4
		p.visualCursor++
		v := p.backdrop
		if idx == 3 {
			v = p.thumbnail
		}
		if idx < 2 {
			v = p.views[idx]
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
	if !p.open || p.results != nil || p.retryAccepted || p.current == nil || p.current.displays[0] == nil || p.contextPath == "" || !strings.EqualFold(filepath.Clean(path), filepath.Clean(p.contextPath)) {
		return 0, false
	}
	offset := util.Point{X: int(p.offset[0]), Y: int(p.offset[1]), Z: int(p.offset[2])}
	if p.current.projection != nil {
		found := false
		for _, placement := range p.current.projection.Placements {
			if placement.Root.ID == p.focusRoot && strings.EqualFold(filepath.Clean(placement.Source.Path), filepath.Clean(path)) {
				offset = placement.Transform.Offset
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
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
	size.Y = max(100, size.Y)
	size.X = max(100, size.X)
	start := imgui.CursorScreenPos()
	imgui.InvisibleButton("comparison-canvas", size)
	if imgui.IsItemHovered() {
		if imgui.IsMouseClicked(0) {
			mouse := imgui.MousePos()
			localX := mouse.X - start.X
			if p.mode == 0 && localX > (size.X+8)/2 {
				localX -= (size.X + 8) / 2
			}
			p.inspected = util.Point{X: int(math.Floor(float64((localX/p.camera.Scale-p.camera.ShiftX)/float32(dmmap.WorldIconSize)))) + 1, Y: int(math.Floor(float64(((size.Y-mouse.Y+start.Y)/p.camera.Scale-p.camera.ShiftY)/float32(dmmap.WorldIconSize)))) + 1, Z: int(p.level)}
		}
		_, wheel := imgui.CurrentIO().MouseWheel()
		p.camera.Scale = max(.02, min(8, p.camera.Scale*float32(math.Pow(1.15, float64(wheel)))))
		if imgui.IsMouseDragging(2, 0) {
			delta := imgui.CurrentIO().MouseDelta()
			p.camera.Translate(delta.X/p.camera.Scale, -delta.Y/p.camera.Scale)
		}
	}
	viewSize := size
	if p.mode == 0 {
		viewSize.X = (size.X - 8) / 2
	}
	p.viewSize = viewSize
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
		v.Process(viewSize)
	}
	draw := imgui.WindowDrawList()
	end := imgui.Vec2{X: start.X + viewSize.X, Y: start.Y + viewSize.Y}
	show := func(i int, from, to imgui.Vec2, alpha float32) {
		if p.views[i] != nil {
			draw.AddImageV(imgui.TextureID(p.views[i].Texture()), from, to, imgui.Vec2{X: 0, Y: 1}, imgui.Vec2{X: 1, Y: 0}, imgui.PackedColorFromVec4(imgui.Vec4{X: 1, Y: 1, Z: 1, W: alpha}))
		}
	}
	show(0, start, end, 1)
	if p.mode == 0 {
		from := imgui.Vec2{X: end.X + 8, Y: start.Y}
		show(1, from, imgui.Vec2{X: from.X + viewSize.X, Y: end.Y}, 1)
	} else if p.mode == 1 {
		show(1, start, end, p.alpha)
	} else if p.mode == 2 {
		draw.PushClipRect(imgui.Vec2{X: start.X + size.X*p.wipe, Y: start.Y}, end)
		show(1, start, end, 1)
		draw.PopClipRect()
	} else if p.mode == 3 && time.Now().UnixMilli()/700%2 == 1 {
		show(1, start, end, 1)
	} else if p.mode == 4 {
		p.drawDifferences(start, end)
	} else if p.mode == 5 {
		p.drawBounds(start)
	} else if p.mode == 6 {
		p.drawReservations(start, end)
	}
	if p.views[0].Render().LevelLoading() {
		draw.AddText(start, imgui.PackedColorFromVec4(imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}), "Preparing reference geometry…")
	}
}

func (p *Panel) navigate(point util.Point) {
	p.level = int32(point.Z)
	p.inspected = point
	p.camera.ShiftX = -float32((point.X-1)*dmmap.WorldIconSize) + 100/p.camera.Scale
	p.camera.ShiftY = -float32((point.Y-1)*dmmap.WorldIconSize) + 100/p.camera.Scale
}

func (p *Panel) ProcessUnit(u unit.Unit) bool {
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
func (p *Panel) RenderPolicyRevision() uint64 { return p.policyRevision }

func (p *Panel) screen(point util.Point, start imgui.Vec2) imgui.Vec2 {
	return imgui.Vec2{X: start.X + (float32((point.X-1)*dmmap.WorldIconSize)+p.camera.ShiftX)*p.camera.Scale, Y: start.Y + (p.viewSize.Y - (float32((point.Y-1)*dmmap.WorldIconSize)+p.camera.ShiftY)*p.camera.Scale)}
}
