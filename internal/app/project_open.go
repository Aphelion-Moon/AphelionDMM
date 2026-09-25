// APHELION EDIT ADDITION START - OWNED MAP OPEN
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/diagnostics/uistage"
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
	cancelled                                   bool // UI-owned; workers only own their result channel and captured strings.
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	builder                                     *mapopen.Builder
	started                                     time.Time
	traceTask                                   *trace.Task
	internItems, internSlices                   int
	internTime, longestInternItem               time.Duration
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
	request.ctx, request.traceTask = trace.NewTask(context.Background(), "aphelion.map.open")
	request.ctx, request.cancel = context.WithCancel(request.ctx)
	trace.Log(request.ctx, "state", "queued")
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
	trace.Log(request.ctx, "state", "source-start")
	path, backupDir, environmentName := request.path, request.backupDir, request.environmentName
	results := request.results
	go func() {
		region := trace.StartRegion(request.ctx, string(uistage.MapSource))
		result := prepareMapOpen(path, backupDir, environmentName)
		region.End()
		results <- result
	}()
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
		flush := uistage.Begin(uistage.MapFlush)
		defer flush.End()
		if result.err == nil {
			if err := backup.Sync(); err != nil {
				result.err = fmt.Errorf("flush map backup: %w", err)
			}
		}
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
		allowance := resources.FrameWorkRemaining(2 * time.Millisecond)
		if allowance <= 0 {
			return
		}
		started := time.Now()
		intern := trace.StartRegion(request.ctx, string(uistage.MapIntern))
		progress := request.builder.InternUntil(request.ctx, started.Add(allowance))
		intern.End()
		request.internSlices++
		request.internItems += progress.Items
		request.internTime += time.Since(started)
		request.longestInternItem = max(request.longestInternItem, progress.LongestItem)
		if trace.IsEnabled() {
			trace.Logf(request.ctx, "intern", "items=%d reason=%s longest=%s", progress.Items, progress.Reason, progress.LongestItem)
		}
		resources.ChargeFrameWork(started)
		if !progress.Done {
			return
		}
		builder, ctx, environment, results := request.builder, request.ctx, request.environment, request.results
		request.builder = nil
		go func() {
			result := mapOpenResult{}
			result.reservation, result.err = resources.DefaultBudget().Reserve(builder.EstimateBytes())
			if result.err == nil {
				tiles := trace.StartRegion(ctx, string(uistage.MapTiles))
				dmm, unknown, err := builder.Build(ctx)
				tiles.End()
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
		if result.err != nil {
			a.finishMapOpen(request)
			err := result.err
			dialog.Open(dialog.TypeCustom{Title: "Unable to open map", Layout: w.Layout{
				w.Text("The map was not installed. " + err.Error()),
				w.Button("Retry", func() { imgui.CloseCurrentPopup(); a.enqueueMapOpen(request.path, request.workspace) }), w.SameLine(), w.Button("Cancel", imgui.CloseCurrentPopup),
			}})
			return
		}
		install := trace.StartRegion(request.ctx, string(uistage.MapInstall))
		a.installOpenMap(request.path, request.workspace, result.prepared.Dmm(), result.unknown, result.prepared)
		install.End()
		a.finishMapOpen(request)
	default:
	}
}

func (a *app) mapOpenCurrent(request *mapOpenRequest) bool {
	return !request.cancelled && !a.closed && a.loadedEnvironment == request.environment &&
		(request.workspace == nil || a.layout != nil && a.layout.WsArea.OwnsWorkspaceContent(request.workspace, request.contentID))
}
func (a *app) finishMapOpen(request *mapOpenRequest) {
	if request.traceTask != nil {
		trace.Logf(request.ctx, "intern-total", "items=%d slices=%d execution=%s longest=%s", request.internItems, request.internSlices, request.internTime, request.longestInternItem)
		request.traceTask.End()
	}
	if request.cancel != nil {
		request.cancel()
	}
	a.mapOpenActive = nil
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
		if request.traceTask != nil {
			request.traceTask.End()
		}
		if request.cancel != nil {
			request.cancel()
		}
	}
	clear(a.mapOpenQueue)
	a.mapOpenQueue = nil
}

// APHELION EDIT ADDITION END
