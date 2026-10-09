// APHELION EDIT ADDITION START - TAB CONTEXT MENU
package cpwsarea

import (
	"path/filepath"

	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/platform"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
	"github.com/skratchdot/open-golang/open"
)

// showWorkspaceTabMenu opens on a right-click of the workspace's tab. It must
// run directly after the workspace window's Begin, while the tab is the last
// item. Chosen actions are queued and run after the workspace loop, because
// closing tabs changes the list being iterated.
func (w *WsArea) showWorkspaceTabMenu(ws *workspace.Workspace) {
	if !imgui.BeginPopupContextItemV("workspace-tab-menu", imgui.PopupFlagsMouseButtonRight) {
		return
	}
	defer imgui.EndPopup()
	idx := w.findWorkspaceIdx(ws)
	if imgui.MenuItem("Close") {
		w.queueTabAction(func() { w.closeWorkspaceGently(ws) })
	}
	if imgui.MenuItemV("Close Others", "", false, len(w.workspaces) > 1) {
		w.queueTabAction(func() { w.closeWorkspacesGently(w.workspacesExcept(ws)) })
	}
	if imgui.MenuItemV("Close to the Right", "", false, idx >= 0 && idx < len(w.workspaces)-1) {
		w.queueTabAction(func() {
			if i := w.findWorkspaceIdx(ws); i >= 0 {
				w.closeWorkspacesGently(append([]*workspace.Workspace(nil), w.workspaces[i+1:]...))
			}
		})
	}
	if imgui.MenuItem("Close All") {
		w.queueTabAction(w.CloseAll)
	}
	content, isMap := ws.Content().(*wsmap.WsMap)
	if !isMap {
		return
	}
	imgui.Separator()
	if imgui.MenuItem("Save") {
		w.queueTabAction(func() { content.Save() })
	}
	if imgui.MenuItem("Save As...") {
		w.queueTabAction(content.SaveAs)
	}
	path := content.Map().Dmm().Path.Absolute
	onDisk := !content.Untitled() && path != ""
	if imgui.MenuItemV("Copy Path", "", false, onDisk) {
		platform.SetClipboard(path)
	}
	if imgui.MenuItemV("Open Containing Folder", "", false, onDisk) {
		if err := open.Run(filepath.Dir(path)); err != nil {
			log.Print("unable to open map folder:", err)
		}
	}
}

func (w *WsArea) queueTabAction(action func()) {
	w.tabActions = append(w.tabActions, action)
}

func (w *WsArea) runTabActions() {
	actions := w.tabActions
	w.tabActions = nil
	for _, action := range actions {
		action()
	}
}

func (w *WsArea) workspacesExcept(keep *workspace.Workspace) []*workspace.Workspace {
	out := make([]*workspace.Workspace, 0, len(w.workspaces))
	for _, ws := range w.workspaces {
		if ws != keep {
			out = append(out, ws)
		}
	}
	return out
}

// APHELION EDIT ADDITION END
