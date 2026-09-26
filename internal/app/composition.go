// APHELION EDIT ADDITION START - COMPOSITION INSPECTOR
package app

import (
	"context"
	"fmt"
	"github.com/SpaiR/imgui-go"
	"path/filepath"
	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/aphelion/mapping"
	mappingui "sdmm/internal/aphelion/mapping/ui"
	"sdmm/internal/aphelion/mapview"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/layout/lnode"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"sort"
	"strings"
)

func (a *app) MappingSourceNoop(path, channel string) error {
	ws, ok := a.activeWsMap()
	if !ok || !strings.EqualFold(filepath.Clean(path), filepath.Clean(ws.Map().Dmm().Path.Absolute)) {
		return fmt.Errorf("activate the intended source document first")
	}
	if channel != "turf" && channel != "area" {
		return fmt.Errorf("unknown noop channel")
	}
	typePath := "/" + channel + "/template_noop"
	if a.loadedEnvironment.Objects[typePath] == nil {
		return fmt.Errorf("this project does not define %s", typePath)
	}
	e := ws.Map().Editor()
	return e.FillSelection(e.WorkingSelection().Get(ws.Map().ActiveLevel()), dmmap.PrefabStorage.Initial(typePath), true)
}

func (a *app) PrepareMappingExport(target string, connector *util.Point) (func(context.Context) (*mapping.AuthoringProposal, error), func() bool, error) {
	ws, ok := a.activeWsMap()
	if !ok {
		return nil, nil, fmt.Errorf("activate a source map and select its cells")
	}
	e := ws.Map().Editor()
	selection := e.WorkingSelection().Get(ws.Map().ActiveLevel())
	if selection.Len() == 0 {
		return nil, nil, fmt.Errorf("select cells on the active source map")
	}
	if sourceKey := filepath.Clean(ws.Map().Dmm().Path.Absolute); strings.EqualFold(sourceKey, filepath.Clean(target)) {
		return nil, nil, fmt.Errorf("export to a separate source file")
	}
	handle, version, err := e.CaptureSaveSnapshot(context.Background())
	if err != nil {
		return nil, nil, err
	}
	return func(ctx context.Context) (*mapping.AuthoringProposal, error) {
		lease, err := resources.DefaultBudget().Reserve(handle.EstimatedBytes())
		if err != nil {
			return nil, err
		}
		defer lease.Release()
		return mapping.PrepareTemplateExport(ctx, target, handle.Snapshot(), selection, connector)
	}, func() bool { return e.SaveCaptureReady(version) }, nil
}

func (a *app) CompositionBackdrop(path string, camera render.Camera, size imgui.Vec2) (uint32, bool) {
	if a.layout == nil || a.layout.Composition == nil {
		return 0, false
	}
	return a.layout.Composition.Backdrop(path, camera, size)
}

func (a *app) MappingRevisionKey(paths []string) string {
	var parts []string
	for _, workspace := range a.layout.WsArea.MapWorkspaces() {
		if ws, ok := workspace.Content().(*wsmap.WsMap); ok {
			wanted := false
			for _, path := range paths {
				if !filepath.IsAbs(path) && a.LoadedEnvironment() != nil {
					path = filepath.Join(a.LoadedEnvironment().RootDir, path)
				}
				if strings.EqualFold(filepath.Clean(path), filepath.Clean(ws.Map().Dmm().Path.Absolute)) {
					wanted = true
					break
				}
			}
			if !wanted {
				continue
			}
			generation, revision := ws.Map().Editor().SaveVersion()
			parts = append(parts, fmt.Sprintf("%s:%s:%d:%d", ws.Map().Dmm().Path.Absolute, ws.Id(), generation, revision))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func (a *app) CaptureMappingSources() map[string]mapping.AcceptedSource {
	sources := make(map[string]mapping.AcceptedSource)
	for _, workspace := range a.layout.WsArea.MapWorkspaces() {
		if ws, ok := workspace.Content().(*wsmap.WsMap); ok {
			editor := ws.Map().Editor()
			snapshot, version, err := editor.CaptureSaveSnapshot(context.Background())
			sources[ws.Map().Dmm().Path.Absolute] = mapping.AcceptedSource{Snapshot: snapshot, Generation: version.Generation, Err: err, Current: func() bool { return editor.SaveCaptureReady(version) }}
		}
	}
	return sources
}

func (a *app) DoOpenCompositionInspector() {
	a.layout.Composition.Open()
	a.ShowLayout(lnode.NameComposition, true)
}
func (a *app) ActiveMappingPath() string {
	if a.layout != nil && a.layout.WsArea.ActiveWorkspace() != nil {
		if c, ok := a.layout.WsArea.ActiveWorkspace().Content().(*mappingui.Comparison); ok {
			return c.Panel.SourcePath()
		}
	}
	if ws, ok := a.activeWsMap(); ok {
		return ws.Map().Dmm().Path.Absolute
	}
	return ""
}

func (a *app) MappingDocuments() []string {
	var paths []string
	for _, workspace := range a.layout.WsArea.MapWorkspaces() {
		if ws, ok := workspace.Content().(*wsmap.WsMap); ok {
			paths = append(paths, ws.Map().Dmm().Path.Absolute)
		}
	}
	return paths
}
func (a *app) mappingWorkspace(path string) *wsmap.WsMap {
	for _, workspace := range a.layout.WsArea.MapWorkspaces() {
		if ws, ok := workspace.Content().(*wsmap.WsMap); ok && strings.EqualFold(filepath.Clean(path), filepath.Clean(ws.Map().Dmm().Path.Absolute)) {
			return ws
		}
	}
	return nil
}
func (a *app) MappingPathVisible(path string) bool { return a.PathsFilter().IsVisiblePath(path) }
func (a *app) MappingFilterRevision() uint64       { return a.PathsFilter().PolicyRevision() }
func (a *app) FrameMappingSource(path string, point util.Point) {
	if active := a.layout.WsArea.ActiveWorkspace(); active != nil {
		if c, ok := active.Content().(*mappingui.Comparison); ok && c.Panel.SourcePath() == path {
			c.Panel.Frame(point)
			return
		}
	}
	if ws := a.mappingWorkspace(path); ws != nil {
		ws.Map().SetActiveLevel(point.Z)
		camera := ws.Map().Canvas().Render().Camera
		size := ws.Map().Size()
		mapview.Center(camera, size, point)
	}
}

func (a *app) MappingNavigationFrame(path string) (string, render.Camera) {
	if ws := a.mappingWorkspace(path); ws != nil {
		camera := *ws.Map().Canvas().Render().Camera
		camera.Level = ws.Map().ActiveLevel()
		return ws.Id(), camera
	}
	return "", render.Camera{}
}
func (a *app) RestoreMappingFrame(path, lifetime string, camera render.Camera) {
	if ws := a.mappingWorkspace(path); ws != nil && ws.Id() == lifetime {
		a.layout.Composition.RestoreContextCamera(path, camera)
	}
}
func (a *app) MoveMappingRoot(root mapping.Root, to util.Point, check bool) error {
	ws := a.mappingWorkspace(root.Source.Path)
	if ws == nil {
		return fmt.Errorf("open the containing source map first")
	}
	if check {
		_, err := ws.Map().Editor().CompositionRoot(root, to)
		return err
	}
	return ws.Map().Editor().MoveCompositionRoot(root, to)
}
func (a *app) OpenMappingComparison(p *mappingui.Panel) { a.layout.WsArea.OpenComposition(p) }
func (a *app) OpenMappingContext(parent, path string, transform mapping.Transform) {
	if ws := a.mappingWorkspace(parent); ws != nil {
		camera := *ws.Map().Canvas().Render().Camera
		camera.Level = ws.Map().ActiveLevel()
		camera.ShiftX += float32(transform.Offset.X * dmmap.WorldIconSize)
		camera.ShiftY += float32(transform.Offset.Y * dmmap.WorldIconSize)
		camera.Level = max(1, camera.Level-transform.Offset.Z)
		a.layout.Composition.SetContextCamera(parent, path, camera)
	}
	a.DoLoadResource(path)
}

// OpenMappingSource binds a navigation intent to the original host workspace,
// environment and foreground workspace. A late load cannot steal focus.
func (a *app) OpenMappingSource(parent, path string, transform mapping.Transform, valid func() bool, done func(error)) {
	host, environment := a.mappingWorkspace(parent), a.loadedEnvironment
	foreground := a.layout.WsArea.ActiveWorkspace()
	current := func() bool {
		return valid() && host != nil && a.mappingWorkspace(parent) == host && a.loadedEnvironment == environment && a.layout.WsArea.ActiveWorkspace() == foreground
	}
	activate := func() {
		camera := *host.Map().Canvas().Render().Camera
		camera.Level = host.Map().ActiveLevel()
		camera.ShiftX += float32(transform.Offset.X * dmmap.WorldIconSize)
		camera.ShiftY += float32(transform.Offset.Y * dmmap.WorldIconSize)
		camera.Level = max(1, camera.Level-transform.Offset.Z)
		a.layout.Composition.SetContextCamera(parent, path, camera)
		done(nil)
	}
	if !current() {
		done(fmt.Errorf("navigation no longer belongs to the active host"))
		return
	}
	if ws := a.mappingWorkspace(path); ws != nil {
		ws.Root().SetTriggerFocus(true)
		activate()
		return
	}
	if a.mapOpenActive != nil && strings.EqualFold(a.mapOpenActive.path, path) {
		done(fmt.Errorf("source is already opening; retry when it is ready"))
		return
	}
	for _, request := range a.mapOpenQueue {
		if strings.EqualFold(request.path, path) {
			done(fmt.Errorf("source is already queued for opening"))
			return
		}
	}
	a.enqueueMapOpen(path, nil)
	request := a.mapOpenActive
	for _, queued := range a.mapOpenQueue {
		if queued.path == path {
			request = queued
			break
		}
	}
	if request == nil || request.path != path {
		done(fmt.Errorf("source could not be queued"))
		return
	}
	request.navigationCurrent = current
	request.navigationDone = func(err error) {
		if err != nil {
			done(err)
		} else {
			activate()
		}
	}
}

func (a *app) MappingRefreshReady(paths []string) bool {
	for _, path := range paths {
		if ws := a.mappingWorkspace(path); ws != nil && tools.OwnsGesture(ws.Map().Editor()) {
			return false
		}
	}
	return true
}

func (a *app) CompositionHeader(path string, dirty bool) bool {
	return a.layout.Composition.ContextHeader(path, dirty)
}
func (a *app) CompositionContextAlpha(path string) float32 {
	return a.layout.Composition.ContextAlpha(path)
}
func (a *app) ShowMappingEnvironment() {
	a.layout.ShowNode(lnode.NameEnvironment)
	a.layout.FocusNode(lnode.NameEnvironment)
}
func (a *app) ReturnMappingContext(parent, path string, transform mapping.Transform) {
	if source, target := a.mappingWorkspace(path), a.mappingWorkspace(parent); source != nil && target != nil {
		camera := *source.Map().Canvas().Render().Camera
		camera.ShiftX -= float32(transform.Offset.X * dmmap.WorldIconSize)
		camera.ShiftY -= float32(transform.Offset.Y * dmmap.WorldIconSize)
		camera.Level = max(1, min(target.Map().Dmm().MaxZ, source.Map().ActiveLevel()+transform.Offset.Z))
		*target.Map().Canvas().Render().Camera = camera
		target.Map().SetActiveLevel(camera.Level)
	}
	a.DoLoadResource(parent)
}
func (a *app) CompositionCamera(path string) (render.Camera, bool) {
	return a.layout.Composition.TakeContextCamera(path)
}
func (a *app) CompositionDraw(path string, camera render.Camera, size, origin imgui.Vec2) {
	a.layout.Composition.Draw(path, camera, size, origin)
}
func (a *app) CompositionVisible(path string) bool { return a.layout.Composition.VisibleInMap(path) }
func (a *app) CompositionInput(path string, point util.Point, active, pressed, released, cancel, focused bool, tool string) bool {
	return a.layout.Composition.Handle(path, point, active, pressed, released, cancel, focused, tool)
}
func (a *app) CloseCompositionSource(path string) { a.layout.Composition.CloseSource(path) }
func (a *app) RestoreCompositionDock()            { a.layout.RestoreCompositionDock() }
func (a *app) CancelCompositionDraft(path string) { a.layout.Composition.CancelDraft(path) }
func (a *app) CompositionTileLocked(path string, point util.Point) bool {
	return a.layout != nil && a.layout.Composition.TileLocked(path, point)
}
func (a *app) CompositionEditFence(path string) func(util.Point) bool {
	return a.layout.Composition.EditFence(path)
}
func (a *app) UnhideMappingRoot(root mapping.Root) error {
	ws := a.mappingWorkspace(root.Source.Path)
	if ws == nil || !ws.Map().Dmm().HasTile(root.Local) {
		return fmt.Errorf("open the containing source first")
	}
	for _, i := range ws.Map().Dmm().GetTile(root.Local).Instances() {
		if root.StableID != "" && i.StableID() == root.StableID {
			return a.SetFilterVisibility(i.Prefab().Path(), filterprofiles.ScopeExact, true)
		}
	}
	return fmt.Errorf("source root changed; refresh its binding")
}

// APHELION EDIT ADDITION END
