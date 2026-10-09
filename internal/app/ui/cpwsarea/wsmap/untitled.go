// APHELION EDIT ADDITION START - JOIN INTO NEW DOCUMENT
package wsmap

import (
	"path/filepath"

	"github.com/rs/zerolog/log"
	nativeDialog "github.com/sqweek/dialog"
)

// MarkUntitled makes the workspace a session-owned document with no backing
// file. Its Dmm path is only an identity for undo history and tab identity; it
// is never read or written. Save routes to a user-chosen Save As destination.
func (ws *WsMap) MarkUntitled() { ws.untitled = true }

// Untitled reports whether the document has no backing file yet.
func (ws *WsMap) Untitled() bool { return ws.untitled }

// chooseSaveAsThen asks the user for a destination and reports the outcome.
// The destination is always a local user choice, never data from a session.
func (ws *WsMap) chooseSaveAsThen(callback func(bool)) bool {
	pick := ws.pickSavePath
	if pick == nil {
		pick = func(startDir string) (string, error) {
			return nativeDialog.File().Title("Save Map As").Filter(".dmm").SetStartDir(startDir).Save()
		}
	}
	startDir := filepath.Dir(ws.CommandStackId())
	if environment := ws.app.LoadedEnvironment(); ws.untitled && environment != nil {
		startDir = environment.RootDir
	}
	path, err := pick(startDir)
	if err != nil || path == "" {
		log.Print("Save As canceled or unavailable:", err)
		if callback != nil {
			callback(false)
		}
		return false
	}
	return ws.saveAsToAsync(path, callback)
}

// APHELION EDIT ADDITION END
