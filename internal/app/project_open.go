// APHELION EDIT ADDITION START - OWNED MAP OPEN
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
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
}
type mapOpenResult struct {
	data   *dmmdata.DmmData
	backup string
	err    error
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
	request.dialog = dialog.TypeCustom{Title: "Opening map", Layout: w.Layout{
		w.Text(filepath.Base(request.path)),
		w.Text("Preparing map and recovery backup…"),
		w.Button("Cancel", func() { request.cancelled = true; imgui.CloseCurrentPopup() }),
	}}
	dialog.Open(request.dialog)
	path, backupDir, environmentName := request.path, request.backupDir, request.environmentName
	results := request.results
	go func() { results <- prepareMapOpen(path, backupDir, environmentName) }()
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
	select {
	case result := <-request.results:
		a.mapOpenActive = nil
		dialog.Close(request.dialog)
		defer a.startNextMapOpen()
		if request.cancelled || a.closed || a.loadedEnvironment != request.environment {
			return
		}
		if request.workspace != nil && (a.layout == nil || !a.layout.WsArea.OwnsWorkspaceContent(request.workspace, request.contentID)) {
			return
		}
		if result.err != nil {
			err := result.err
			dialog.Open(dialog.TypeCustom{Title: "Unable to open map", Layout: w.Layout{
				w.Text("The map was not installed. " + err.Error()),
				w.Button("Retry", func() { imgui.CloseCurrentPopup(); a.enqueueMapOpen(request.path, request.workspace) }), w.SameLine(), w.Button("Cancel", imgui.CloseCurrentPopup),
			}})
			return
		}
		a.installParsedMap(request.path, request.workspace, result.data, result.backup)
	default:
	}
}

func (a *app) cancelMapOpens() {
	if a.mapOpenActive != nil {
		a.mapOpenActive.cancelled = true
	}
	clear(a.mapOpenQueue)
	a.mapOpenQueue = nil
}

// APHELION EDIT ADDITION END
