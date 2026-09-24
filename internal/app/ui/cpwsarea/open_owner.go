// APHELION EDIT ADDITION START - OWNED MAP OPEN
package cpwsarea

import "sdmm/internal/app/ui/cpwsarea/workspace"

// A late open may only replace the exact content that admitted it.
func (w *WsArea) OwnsWorkspaceContent(ws *workspace.Workspace, contentID string) bool {
	return w.findWorkspaceIdx(ws) >= 0 && ws.Content().Id() == contentID
}

// APHELION EDIT ADDITION END
