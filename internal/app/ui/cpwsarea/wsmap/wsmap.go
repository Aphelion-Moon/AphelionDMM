package wsmap

import (
	// APHELION EDIT ADDITION START - ATOMIC_SAVE
	"context"
	"sdmm/internal/aphelion/collab/model"
	// APHELION EDIT ADDITION END
	"fmt"

	"sdmm/internal/app/command"
	"sdmm/internal/app/prefs"
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	"sdmm/internal/app/render"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

type App interface {
	pmap.App

	LoadedEnvironment() *dmenv.Dme
	CommandStorage() *command.Storage
	Prefs() prefs.Prefs
}

type WsMap struct {
	workspace.Content

	app App

	paneMap *pmap.PaneMap
	// APHELION EDIT ADDITION START - ATOMIC_SAVE
	savedMapHash    string
	savedGeneration uint64
	savedRevision   model.Revision
	diskConflict    bool
	saveRequestID   uint64
	saveLifetime    uint64
	activeSave      *saveJob
	pendingSaveAck  *saveAcknowledgement
	disposed        bool
	// APHELION EDIT ADDITION END
}

func New(app App, dmm *dmmap.Dmm) *WsMap {
	// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: return &WsMap{
	ws := &WsMap{
		app:     app,
		paneMap: pmap.New(app, dmm),
	}
	// APHELION EDIT ADDITION START - ATOMIC_SAVE
	if snapshot, err := ws.paneMap.Editor().SaveSnapshot(context.Background()); err == nil {
		ws.savedMapHash, _ = snapshot.Hash()
		ws.savedGeneration, _ = ws.paneMap.Editor().SaveVersion()
		ws.savedRevision = snapshot.Revision
	}
	return ws
	// APHELION EDIT ADDITION END
}

func (ws *WsMap) Map() *pmap.PaneMap {
	return ws.paneMap
}

func (ws *WsMap) CommandStackId() string {
	return ws.paneMap.Dmm().Path.Absolute
}

func (WsMap) Ini() workspace.Ini {
	return workspace.Ini{
		WindowFlags: imgui.WindowFlagsNoScrollbar | imgui.WindowFlagsNoBringToFrontOnFocus,
		NoPadding:   true,
	}
}

func (ws *WsMap) Name() string {
	visibleName := ws.paneMap.Dmm().Name
	// APHELION EDIT CHANGE - ATOMIC_SAVE - ORIGINAL: if ws.app.CommandStorage().IsModified(ws.CommandStackId()) {
	if ws.diskConflict || ws.app.CommandStorage().IsModified(ws.CommandStackId()) || ws.paneMap.Editor().ChangedSinceSave(ws.savedGeneration, ws.savedRevision) {
		visibleName = "* " + visibleName
	}
	// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
	if ws.activeSave != nil {
		visibleName += " (Saving...)"
	}
	// APHELION EDIT ADDITION END
	return fmt.Sprint(visibleName, "###workspace_map_", ws.paneMap.Dmm().Path.Absolute)
}

func (ws *WsMap) Title() string {
	return ws.paneMap.Dmm().Name
}

func (ws *WsMap) NameReadable() string {
	return ws.paneMap.Dmm().Name
}

func (ws *WsMap) PreProcess() {
	/* APHELION EDIT REMOVAL START - DOCUMENT COMMAND OWNERSHIP
	ws.paneMap.SetShortcutsVisible(false)
	APHELION EDIT REMOVAL END */
	ws.processCanvasCameraMirror()
}

func (ws *WsMap) Process() {
	ws.paneMap.Process()
	// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
	ws.tryCompleteSaveAcknowledgement()
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - OWNED MAP OPEN
func (ws *WsMap) ProcessLevelBuildBudget(budget *render.LevelBuildBudget) bool {
	r := ws.paneMap.Canvas().Render()
	r.SetActiveLevel(ws.paneMap.Dmm(), ws.paneMap.ActiveLevel())
	return r.ProcessLevelBuildBudget(budget)
}

// APHELION EDIT ADDITION END

func (ws *WsMap) Dispose() {
	// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
	ws.disposed = true
	ws.saveLifetime++
	if ws.activeSave != nil {
		ws.completeSaveCallbacks(ws.activeSave.callbacks, false)
		ws.activeSave = nil
	}
	if ws.pendingSaveAck != nil {
		ws.completeSaveCallbacks(ws.pendingSaveAck.callbacks, false)
		ws.pendingSaveAck = nil
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - DOCUMENT COMMAND OWNERSHIP
	ws.paneMap.SetShortcutsVisible(false)
	// APHELION EDIT ADDITION END
	ws.paneMap.Dispose()
	log.Print("map workspace disposed:", ws.Name())
}

func (ws *WsMap) Focused() bool {
	return ws.paneMap.Focused()
}

func (ws *WsMap) OnFocusChange(focused bool) {
	if focused {
		ws.paneMap.OnActivate()
	} else {
		ws.paneMap.OnDeactivate()
	}
}

// APHELION EDIT ADDITION START - DOCUMENT COMMAND OWNERSHIP
// OnCommandContextChange tracks the active workspace, which can remain the
// same while keyboard focus moves into one of its palette or editor panels.
func (ws *WsMap) OnCommandContextChange(active bool) {
	ws.paneMap.SetShortcutsVisible(active)
}

// APHELION EDIT ADDITION END

func (ws *WsMap) processCanvasCameraMirror() {
	if !pmap.MirrorCanvasCamera || pmap.ActiveCamera() == nil {
		return
	}

	activeCamera := pmap.ActiveCamera()
	if camera := ws.paneMap.Canvas().Render().Camera; camera != activeCamera {
		camera.ShiftX = activeCamera.ShiftX
		camera.ShiftY = activeCamera.ShiftY
		camera.Level = activeCamera.Level
		camera.Scale = activeCamera.Scale
	}
}
