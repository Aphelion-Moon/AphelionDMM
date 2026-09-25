// APHELION EDIT ADDITION START - OWNED MAP OPEN
package wsmap

import (
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
)

func NewPrepared(app App, prepared *editor.PreparedOpen) *WsMap {
	hash, revision := prepared.Hash(), prepared.Revision()
	ws := &WsMap{app: app, paneMap: pmap.NewPrepared(app, prepared), savedMapHash: hash, savedRevision: revision}
	ws.savedGeneration, _ = ws.paneMap.Editor().SaveVersion()
	return ws
}

// APHELION EDIT ADDITION END
