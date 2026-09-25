// APHELION EDIT ADDITION START - COMPOSITION WORKSPACE
package cpwsarea

import (
	mappingui "sdmm/internal/aphelion/mapping/ui"
	"sdmm/internal/app/ui/cpwsarea/workspace"
)

func (w *WsArea) OpenComposition(p *mappingui.Panel) {
	for _, ws := range w.workspaces {
		if c, ok := ws.Content().(*mappingui.Comparison); ok && c.Panel == p {
			ws.SetTriggerFocus(true)
			return
		}
	}
	w.addWorkspace(workspace.New(&mappingui.Comparison{Panel: p}))
}

// APHELION EDIT ADDITION END
