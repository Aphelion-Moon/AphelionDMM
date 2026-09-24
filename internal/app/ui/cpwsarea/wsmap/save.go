package wsmap

import (
	// APHELION EDIT ADDITION START - ATOMIC_SAVE
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/dmapi/dmmap"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/prefs"
	// APHELION EDIT ADDITION START - DISK_VERSION
	"sdmm/internal/app/ui/dialog"
	// APHELION EDIT ADDITION END
	"sdmm/internal/dmapi/dmmsave"
	// APHELION EDIT ADDITION START - DISK_VERSION
	w "sdmm/internal/imguiext/widget"
	// APHELION EDIT ADDITION END
	"sdmm/internal/util"

	// APHELION EDIT ADDITION START - DISK_VERSION
	"github.com/SpaiR/imgui-go"
	nativeDialog "github.com/sqweek/dialog"
	// APHELION EDIT ADDITION END
	"github.com/rs/zerolog/log"
)

func (ws *WsMap) Save() bool {
	return ws.saveUsingDiskState(ws.paneMap.Dmm().DiskState)
}

// APHELION EDIT ADDITION START - DISK_VERSION
func (ws *WsMap) saveUsingDiskState(expected diskversion.State) bool {
	return ws.saveAtPath(ws.CommandStackId(), expected, false)
}

func (ws *WsMap) saveAtPath(path string, expected diskversion.State, rebind bool) bool {
	log.Print("saving map workspace:", path)
	oldStackID := ws.CommandStackId()
	if rebind && !ws.app.CommandStorage().CanRebindStack(oldStackID, path) {
		return ws.saveFailed(fmt.Errorf("cannot move map history to Save As destination %q", path))
	}

	editorPrefs := ws.app.Prefs().Editor

	var saveFormat dmmsave.Format
	switch editorPrefs.SaveFormat {
	case prefs.SaveFormatInitial:
		saveFormat = dmmsave.FormatInitial
	case prefs.SaveFormatTGM:
		saveFormat = dmmsave.FormatTGM
	case prefs.SaveFormatDMM:
		saveFormat = dmmsave.FormatDM
	}

	// APHELION EDIT ADDITION START - ATOMIC_SAVE
	snapshot, err := ws.paneMap.Editor().SaveSnapshot(context.Background())
	if err != nil {
		return ws.saveFailed(err)
	}
	source := ws.paneMap.Dmm()
	acknowledged := &dmmap.Dmm{Name: source.Name, Path: source.Path, Backup: source.Backup}
	if rebind {
		readable, relErr := filepath.Rel(ws.app.LoadedEnvironment().RootDir, path)
		if relErr != nil {
			readable = path
		}
		acknowledged.Name = filepath.Base(path)
		acknowledged.Path = dmmap.DmmPath{Readable: readable, Absolute: path}
	}
	if err := mapadapter.ApplyWithEnvironment(acknowledged, snapshot, ws.app.LoadedEnvironment()); err != nil {
		return ws.saveFailed(err)
	}
	savedDiskState, err := dmmsave.SaveVWithDiskState(ws.app.LoadedEnvironment(), acknowledged, path, dmmsave.Config{
		Format:            saveFormat,
		SanitizeVariables: editorPrefs.SanitizeVariables,
	}, expected)
	if err != nil {
		if !rebind && errors.Is(err, diskversion.ErrConflict) {
			return ws.showDiskConflict()
		}
		if rebind && errors.Is(err, diskversion.ErrConflict) {
			return ws.saveFailed(fmt.Errorf("Save As destination changed while saving; choose a new file name: %w", err))
		}
		return ws.saveFailed(err)
	}

	if rebind {
		// The UI thread owns command storage. Preflight above ensures this move
		// cannot replace another map's history or interrupt an async operation.
		if !ws.app.CommandStorage().RebindStack(oldStackID, path) {
			return ws.saveFailed(fmt.Errorf("map history changed while saving to %q", path))
		}
		source.Name = acknowledged.Name
		source.Path = acknowledged.Path
	}
	source.DiskState = savedDiskState
	ws.savedMapHash, _ = snapshot.Hash() // SaveSnapshot and ApplyWithEnvironment validated this state.
	ws.savedGeneration, ws.savedRevision = ws.paneMap.Editor().SaveVersion()
	ws.diskConflict = false
	ws.app.CommandStorage().ForceBalance(oldStackID)
	if currentStackID := ws.CommandStackId(); currentStackID != oldStackID {
		ws.app.CommandStorage().ForceBalance(currentStackID)
	}
	// APHELION EDIT ADDITION END
	return true
}

func (ws *WsMap) showDiskConflict() bool {
	ws.diskConflict = true
	state, err := diskversion.Capture(ws.CommandStackId())
	if err != nil {
		return ws.saveFailed(err)
	}
	path := ws.CommandStackId()
	question := "The map file changed after it was loaded. Overwrite it with the current editor state?"
	overwriteLabel := "Overwrite current file"
	if !state.Exists() {
		question = "The map file was deleted after it was loaded. Recreate it with the current editor state?"
		overwriteLabel = "Recreate deleted file"
	}
	ws.app.RunLater(func() {
		dialog.Open(dialog.TypeCustom{
			Title: "Map changed on disk##" + path,
			Layout: w.Layout{
				w.Text(question),
				w.Text(path),
				w.Button(overwriteLabel, func() {
					imgui.CloseCurrentPopup()
					ws.app.RunLater(func() { ws.overwriteAfterConflict(state) })
				}),
				w.SameLine(),
				w.Button("Save As...", func() {
					imgui.CloseCurrentPopup()
					ws.app.RunLater(ws.chooseSaveAs)
				}),
				w.SameLine(),
				w.Button("Cancel", imgui.CloseCurrentPopup),
			},
			CloseButton: false,
		})
	})
	return false
}

func (ws *WsMap) overwriteAfterConflict(state diskversion.State) bool {
	return ws.saveUsingDiskState(state)
}

func (ws *WsMap) chooseSaveAs() {
	path, err := nativeDialog.File().Title("Save Map As").Filter(".dmm").SetStartDir(filepath.Dir(ws.CommandStackId())).Save()
	if err != nil {
		log.Print("Save As canceled or unavailable:", err)
		return
	}
	if filepath.Ext(path) == "" {
		path += ".dmm"
	}
	ws.saveAsTo(path)
}

func (ws *WsMap) saveAsTo(path string) bool {
	if filepath.Ext(path) == "" {
		path += ".dmm"
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return ws.saveFailed(fmt.Errorf("resolve Save As destination: %w", err))
	}
	if _, err := os.Lstat(absolutePath); err == nil {
		return ws.saveFailed(fmt.Errorf("Save As destination already exists; choose a new file name: %q", absolutePath))
	} else if !errors.Is(err, os.ErrNotExist) {
		return ws.saveFailed(fmt.Errorf("inspect Save As destination %q: %w", absolutePath, err))
	}
	return ws.saveAtPath(absolutePath, diskversion.Absent(), true)
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - ATOMIC_SAVE
func (ws *WsMap) saveFailed(err error) bool {
	log.Error().Err(err).Msg("unable to save map workspace")
	ws.app.RunLater(func() { util.ShowErrorDialog("Unable to save the map: " + err.Error()) })
	return false
}

// HasUnsavedChanges checks authority when a close decision is made. The tab
// label uses a cheap version check instead of hashing the map every frame.
func (ws *WsMap) HasUnsavedChanges() bool {
	if ws.diskConflict || ws.app.CommandStorage().IsModified(ws.CommandStackId()) {
		return true
	}
	snapshot, err := ws.paneMap.Editor().SaveSnapshot(context.Background())
	if err != nil {
		return true
	}
	hash, err := snapshot.Hash()
	return err != nil || hash != ws.savedMapHash
}

// APHELION EDIT ADDITION END
