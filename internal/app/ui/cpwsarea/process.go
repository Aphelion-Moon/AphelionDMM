package cpwsarea

import (
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	"sdmm/internal/app/render"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/workspace"

	"github.com/SpaiR/imgui-go"
)

var tmpFocusedWs *workspace.Workspace

func (w *WsArea) Process(dockId int32) {
	if len(w.workspaces) == 0 {
		w.switchActiveWorkspace(nil)
		w.showAppLogo(int(dockId))
	}

	w.processWorkspaces(int(dockId))
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	w.processLevelBuilds()
	// APHELION EDIT ADDITION END

	w.switchFocusedWorkspace(tmpFocusedWs)
	tmpFocusedWs = nil
}

// APHELION EDIT ADDITION START - OWNED MAP OPEN
func (w *WsArea) processLevelBuilds() {
	if len(w.workspaces) == 0 {
		return
	}
	b := render.NewLevelBuildBudget()
	cursor := w.levelBuildCursor % len(w.workspaces)
	idle := 0
	for b.Available() && idle < len(w.workspaces) {
		ws := w.workspaces[cursor]
		processor, ok := ws.Content().(interface {
			ProcessLevelBuildBudget(*render.LevelBuildBudget) bool
		})
		if ok && processor.ProcessLevelBuildBudget(b) {
			idle = 0
		} else {
			idle++
		}
		cursor = (cursor + 1) % len(w.workspaces)
	}
	w.levelBuildCursor = cursor
}

// APHELION EDIT ADDITION END

func (w *WsArea) processWorkspaces(dockId int) {
	var workspacesToClose []*workspace.Workspace
	for _, ws := range w.workspaces {
		ws.PreProcess()

		// When the window of the workspace is closed we need to dispose its content as well.
		if !w.showWorkspaceWindow(dockId, ws) {
			workspacesToClose = append(workspacesToClose, ws)
		}

		ws.PostProcess()
	}

	for _, ws := range workspacesToClose {
		w.closeWorkspaceGently(ws)
	}
}

func (w *WsArea) showWorkspaceWindow(dockId int, ws *workspace.Workspace) (open bool) {
	open = true
	id := ws.Name() + "###" + ws.Id()

	dockCondition := imgui.ConditionOnce
	if w.app.IsLayoutReset() {
		dockCondition = imgui.ConditionAlways
	}

	imgui.SetNextWindowDockIDV(dockId, dockCondition)

	flags := imgui.WindowFlagsNoSavedSettings | ws.Content().Ini().WindowFlags

	if ws.Content().Ini().NoPadding {
		imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	}

	visible := imgui.BeginV(id, &open, flags)

	if visible {
		if ws.Content().Ini().NoPadding {
			imgui.PopStyleVar()
		}

		ws.Process()
	} else if ws.Content().Ini().NoPadding {
		imgui.PopStyleVar()
	}

	if ws.Focused() {
		w.switchActiveWorkspace(ws)
		tmpFocusedWs = ws
	} else if ws.TriggerFocus() {
		ws.SetTriggerFocus(false)
		imgui.SetWindowFocus()
	}

	imgui.End()

	return open || ws.Closed()
}
