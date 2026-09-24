// APHELION EDIT ADDITION START - OWNED MAP OPEN
package cpwsarea

import (
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
)

func (w *WsArea) OpenPreparedMap(prepared *editor.PreparedOpen, ws *workspace.Workspace) bool {
	if existing, ok := w.findMapWorkspace(prepared.Dmm().Path); ok {
		existing.SetTriggerFocus(true)
		return false
	}
	content := wsmap.NewPrepared(w.app, prepared)
	if ws != nil {
		ws.SetContent(content)
	} else {
		ws = workspace.New(content)
		w.addWorkspace(ws)
	}
	ws.SetTriggerFocus(true)
	return true
}

// APHELION EDIT ADDITION END
