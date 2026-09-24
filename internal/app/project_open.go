// APHELION EDIT ADDITION START - OWNED MAP OPEN
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapopen"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"
)

type mapOpenRequest struct {
	path, backupDir, environmentName, contentID string
	environment                                 *dmenv.Dme
	workspace                                   *workspace.Workspace
	results                                     chan mapOpenResult
	dialog                                      dialog.Type
	cancelled                                   bool // UI-owned; workers only own their result channel and captured strings.
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	builder                                     *mapopen.Builder
	started                                     time.Time
}
type mapOpenResult struct {
	data        *dmmdata.DmmData
	backup      string
	err         error
	prepared    *editor.PreparedOpen
	unknown     map[string]*dmmprefab.Prefab
	reservation *resources.Reservation
}

func (a *app) enqueueMapOpen(path string, ws *workspace.Workspace) {
	if a.mapOpenActive != nil && !a.mapOpenActive.cancelled && a.mapOpenActive.path == path {
		return
	}
	for _, request := range a.mapOpenQueue {
		if request.path == path && request.environment == a.loadedEnvironment {
			return
		}
	}
	request := &mapOpenRequest{path: path, backupDir: a.backupDir, environmentName: a.environmentName(), environment: a.loadedEnvironment, workspace: ws, results: make(chan mapOpenResult, 1)}
	request.ctx, request.cancel = context.WithCancel(context.Background())
	if ws != nil {
		request.contentID = ws.Content().Id()
	}
	a.mapOpenQueue = append(a.mapOpenQueue, request)
	a.startNextMapOpen()
}

func (a *app) startNextMapOpen() {
	if a.mapOpenActive != nil || len(a.mapOpenQueue) == 0 || a.closed {
		return
	}
	request := a.mapOpenQueue[0]
	a.mapOpenQueue[0] = nil
	a.mapOpenQueue = a.mapOpenQueue[1:]
	a.mapOpenActive = request
	request.started = time.Now()
	path, backupDir, environmentName := request.path, request.backupDir, request.environmentName
	results := request.results
	go func() { results <- prepareMapOpen(path, backupDir, environmentName) }()
}

// Loading one map does not place a modal over unrelated open documents.
func (a *app) showMapOpenStatus() {
	request := a.mapOpenActive
	if request == nil || request.cancelled || time.Since(request.started) < 200*time.Millisecond {
		return
	}
	imgui.SetNextWindowPosV(imgui.Vec2{X: 20, Y: 50}, imgui.ConditionAppearing, imgui.Vec2{})
	if imgui.BeginV("Opening map##map_open", nil, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoSavedSettings|imgui.WindowFlagsNoFocusOnAppearing) {
		imgui.Text(filepath.Base(request.path))
		if imgui.Button("Cancel") {
			request.cancelled = true
			request.cancel()
		}
	}
	imgui.End()
}

func prepareMapOpen(path, backupDir, environmentName string) (result mapOpenResult) {
	dir := filepath.Join(backupDir, environmentName, filepath.Base(path))
	if err := os.MkdirAll(dir, 0700); err != nil {
		result.err = fmt.Errorf("create map backup directory: %w", err)
		return
	}
	backup, err := os.CreateTemp(dir, time.Now().Format(util.TimeFormat)+"-*.dmm")
	if err != nil {
		result.err = fmt.Errorf("create map backup: %w", err)
		return
	}
	result.backup = backup.Name()
	defer func() {
		closeErr := backup.Close()
		if result.err == nil && closeErr != nil {
			result.err = fmt.Errorf("close map backup: %w", closeErr)
		}
		if result.err != nil {
			_ = os.Remove(result.backup)
			result.backup = ""
		}
	}()
	result.data, result.err = dmmdata.NewWithSourceCopy(path, backup)
	if result.err != nil {
		result.err = fmt.Errorf("parse map and capture backup: %w", result.err)
		return
	}
	if err := backup.Sync(); err != nil {
		result.err = fmt.Errorf("flush map backup: %w", err)
	}
	return
}

func (a *app) processMapOpen() {
	request := a.mapOpenActive
	if request == nil {
		return
	}
	if request.builder != nil {
		if !a.mapOpenCurrent(request) {
			a.finishMapOpen(request)
			return
		}
		if !request.builder.InternStep(256, time.Now().Add(2*time.Millisecond)) {
			return
		}
		builder, ctx, environment, results := request.builder, request.ctx, request.environment, request.results
		request.builder = nil
		go func() {
			result := mapOpenResult{}
			result.reservation, result.err = resources.DefaultBudget().Reserve(builder.EstimateBytes())
			if result.err == nil {
				dmm, unknown, err := builder.Build(ctx)
				result.unknown, result.err = unknown, err
				if err == nil {
					result.prepared, result.err = editor.PrepareOpen(ctx, environment, dmm)
				}
			}
			if ctx.Err() != nil {
				result.reservation.Release()
				result.reservation = nil
				result.prepared = nil
				result.unknown = nil
				result.err = ctx.Err()
			}
			results <- result
		}()
		return
	}
	select {
	case result := <-request.results:
		defer result.reservation.Release()
		if !a.mapOpenCurrent(request) {
			a.finishMapOpen(request)
			return
		}
		if result.err == nil && result.prepared == nil {
			request.builder = mapopen.NewBuilder(request.environment, result.data, result.backup)
			return
		}
		a.finishMapOpen(request)
		if result.err != nil {
			err := result.err
			dialog.Open(dialog.TypeCustom{Title: "Unable to open map", Layout: w.Layout{
				w.Text("The map was not installed. " + err.Error()),
				w.Button("Retry", func() { imgui.CloseCurrentPopup(); a.enqueueMapOpen(request.path, request.workspace) }), w.SameLine(), w.Button("Cancel", imgui.CloseCurrentPopup),
			}})
			return
		}
		a.installOpenMap(request.path, request.workspace, result.prepared.Dmm(), result.unknown, result.prepared)
	default:
	}
}

func (a *app) mapOpenCurrent(request *mapOpenRequest) bool {
	return !request.cancelled && !a.closed && a.loadedEnvironment == request.environment &&
		(request.workspace == nil || a.layout != nil && a.layout.WsArea.OwnsWorkspaceContent(request.workspace, request.contentID))
}
func (a *app) finishMapOpen(request *mapOpenRequest) {
	if request.cancel != nil {
		request.cancel()
	}
	a.mapOpenActive = nil
	dialog.Close(request.dialog)
	a.startNextMapOpen()
}

func (a *app) cancelMapOpens() {
	if a.mapOpenActive != nil {
		a.mapOpenActive.cancelled = true
		if a.mapOpenActive.cancel != nil {
			a.mapOpenActive.cancel()
		}
	}
	for _, request := range a.mapOpenQueue {
		if request.cancel != nil {
			request.cancel()
		}
	}
	clear(a.mapOpenQueue)
	a.mapOpenQueue = nil
}

// APHELION EDIT ADDITION END
