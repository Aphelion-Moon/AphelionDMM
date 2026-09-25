// APHELION EDIT ADDITION START - COMPOSITION INSPECTOR
package app

import (
	"context"
	"fmt"
	"github.com/SpaiR/imgui-go"
	"path/filepath"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
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
			parts = append(parts, fmt.Sprintf("%s:%d:%d", ws.Map().Dmm().Path.Absolute, generation, revision))
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

func (a *app) DoOpenCompositionInspector() { a.layout.Composition.Open() }
func (a *app) ActiveMappingPath() string {
	if ws, ok := a.activeWsMap(); ok {
		return ws.Map().Dmm().Path.Absolute
	}
	return ""
}

// APHELION EDIT ADDITION END
