package wsmap

import (
	// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/mapsave"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmsave"
	"sdmm/internal/util"

	w "sdmm/internal/imguiext/widget"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
	nativeDialog "github.com/sqweek/dialog"
	// APHELION EDIT ADDITION END
)

// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
type saveRequest struct {
	id          uint64
	lifetime    uint64
	stackID     string
	path        string
	expected    diskversion.State
	rebind      bool
	metadata    mapsave.Metadata
	captured    editor.SaveSnapshotCapture
	capture     editor.SaveCapture
	environment *dmenv.Dme
	config      dmmsave.Config
}

type saveJob struct {
	request   saveRequest
	callbacks []func(bool)
	repeat    bool
	// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
	cancel context.CancelFunc // Releases a worker waiting on the key length prompt.
	// acceptedKeyLength is the length the user approved; it is written and read on the UI thread.
	acceptedKeyLength *int
	// APHELION EDIT ADDITION END
}

type saveWorkerResult struct {
	diskState     diskversion.State
	mapHash       string
	conflictState *diskversion.State
	err           error
}

type saveAcknowledgement struct {
	id        uint64
	lifetime  uint64
	capture   editor.SaveCapture
	stackID   string
	callbacks []func(bool)
}

// APHELION EDIT ADDITION END

func (ws *WsMap) Save() bool {
	return ws.saveUsingDiskState(ws.paneMap.Dmm().DiskState)
}

// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
// SaveAsync reports completion on the UI thread. Workspace close uses this
// boundary so the pane and its command history stay alive until the save result
// is known.
func (ws *WsMap) SaveAsync(callback func(bool)) {
	ws.saveAtPath(ws.CommandStackId(), ws.paneMap.Dmm().DiskState, false, callback)
}

func (ws *WsMap) saveUsingDiskState(expected diskversion.State) bool {
	return ws.saveAtPath(ws.CommandStackId(), expected, false, nil)
}

func (ws *WsMap) saveAtPath(path string, expected diskversion.State, rebind bool, callback func(bool)) bool {
	if ws.disposed {
		if callback != nil {
			callback(false)
		}
		return false
	}
	if ws.activeSave != nil {
		if rebind {
			return ws.rejectSave(callback, fmt.Errorf("a map save is already in progress"))
		}
		if callback != nil {
			ws.activeSave.callbacks = append(ws.activeSave.callbacks, callback)
		} else {
			ws.activeSave.repeat = true
		}
		return true
	}
	// APHELION EDIT ADDITION START - JOIN INTO NEW DOCUMENT
	if ws.untitled && !rebind {
		return ws.chooseSaveAsThen(callback)
	}
	// APHELION EDIT ADDITION END
	if ws.pendingSaveAck != nil {
		ws.completeSaveCallbacks(ws.pendingSaveAck.callbacks, false)
		ws.pendingSaveAck = nil
	}

	oldStackID := ws.CommandStackId()
	if rebind && !ws.app.CommandStorage().CanRebindStack(oldStackID, path) {
		return ws.rejectSave(callback, fmt.Errorf("cannot move map history to Save As destination %q", path))
	}
	request, err := ws.captureSaveRequest(path, expected, rebind)
	if err != nil {
		return ws.rejectSave(callback, err)
	}
	ws.saveRequestID++
	request.id = ws.saveRequestID
	request.lifetime = ws.saveLifetime
	// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
	// Only an explicit save of the existing file can re-key it; Save As writes a new file.
	confirmContext, cancelConfirm := context.WithCancel(context.Background())
	accepted := new(int)
	if !rebind {
		request.config.ConfirmKeyLengthChange = ws.keyLengthConfirmer(confirmContext, accepted)
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - KEY_LENGTH_WARNING - ORIGINAL: job := &saveJob{request: request}
	job := &saveJob{request: request, cancel: cancelConfirm, acceptedKeyLength: accepted}
	if callback != nil {
		job.callbacks = append(job.callbacks, callback)
	}
	ws.activeSave = job
	go func() {
		result := runSaveWorker(request)
		ws.app.RunLater(func() { ws.finishSave(job, result) })
	}()
	return true
}

func (ws *WsMap) captureSaveRequest(path string, expected diskversion.State, rebind bool) (saveRequest, error) {
	captured, capture, err := ws.paneMap.Editor().CaptureSaveSnapshot(context.Background())
	if err != nil {
		return saveRequest{}, err
	}
	environment := ws.app.LoadedEnvironment()
	if environment == nil {
		return saveRequest{}, fmt.Errorf("loaded environment is unavailable")
	}
	source := ws.paneMap.Dmm()
	metadata := mapsave.Metadata{Name: source.Name, Path: source.Path, Backup: source.Backup}
	if rebind {
		readable, relErr := filepath.Rel(environment.RootDir, path)
		if relErr != nil {
			readable = path
		}
		metadata.Name = filepath.Base(path)
		metadata.Path = dmmap.DmmPath{Readable: readable, Absolute: path}
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
	// APHELION EDIT ADDITION START - JOIN INTO NEW DOCUMENT
	if ws.untitled && saveFormat == dmmsave.FormatInitial {
		saveFormat = dmmsave.FormatTGM // No existing file to mirror.
	}
	// APHELION EDIT ADDITION END
	return saveRequest{
		stackID:     ws.CommandStackId(),
		path:        path,
		expected:    expected,
		rebind:      rebind,
		metadata:    metadata,
		captured:    captured,
		capture:     capture,
		environment: environment,
		config: dmmsave.Config{
			Format:            saveFormat,
			SanitizeVariables: editorPrefs.SanitizeVariables,
		},
	}, nil
}

func runSaveWorker(request saveRequest) saveWorkerResult {
	return runSaveWorkerWithBudget(request, resources.DefaultBudget())
}

func runSaveWorkerWithBudget(request saveRequest, budget *resources.Budget) saveWorkerResult {
	if request.captured == nil {
		return saveWorkerResult{err: fmt.Errorf("save snapshot capture is unavailable")}
	}
	reservation, err := budget.Reserve(estimateSaveWorkerBytes(request.captured.EstimatedBytes()))
	if err != nil {
		return saveWorkerResult{err: fmt.Errorf("admit map save memory: %w", err)}
	}
	defer reservation.Release()

	snapshot := request.captured.Snapshot()
	mapHash, err := snapshot.Hash()
	if err != nil {
		return saveWorkerResult{err: fmt.Errorf("hash captured map revision: %w", err)}
	}
	document, err := mapsave.Project(snapshot, request.metadata)
	if err != nil {
		return saveWorkerResult{err: err}
	}
	// The projection is private to this worker; Save may sanitize it in place.
	savedDiskState, err := dmmsave.SaveOwnedVWithDiskState(request.environment, document, request.path, request.config, request.expected)
	if err != nil {
		result := saveWorkerResult{err: err}
		if errors.Is(err, diskversion.ErrConflict) {
			state, stateErr := diskversion.Capture(request.path)
			if stateErr != nil {
				result.err = fmt.Errorf("capture current disk state after save conflict: %w", stateErr)
			} else {
				result.conflictState = &state
			}
		}
		return result
	}
	return saveWorkerResult{diskState: savedDiskState, mapHash: mapHash}
}

func estimateSaveWorkerBytes(snapshotBytes uint64) uint64 {
	if snapshotBytes > ^uint64(0)/3 {
		return ^uint64(0)
	}
	// Admission covers the cloned model snapshot, detached DMM projection, and
	// serializer staging, all of which coexist during save preparation.
	return snapshotBytes * 3
}

func (ws *WsMap) finishSave(job *saveJob, result saveWorkerResult) {
	if ws.activeSave != job {
		return
	}
	ws.activeSave = nil
	if job.cancel != nil { // APHELION EDIT ADDITION - KEY_LENGTH_WARNING
		job.cancel()
	}
	request := job.request
	if ws.disposed || ws.saveLifetime != request.lifetime {
		ws.completeSaveCallbacks(job.callbacks, false)
		return
	}
	// APHELION EDIT ADDITION START - KEY_LENGTH_WARNING
	if errors.Is(result.err, dmmsave.ErrKeyLengthChangeDeclined) {
		ws.completeSaveCallbacks(job.callbacks, false) // The user's choice, not a failure.
		return
	}
	// APHELION EDIT ADDITION END
	if result.err != nil {
		if result.conflictState != nil && !request.rebind {
			ws.showDiskConflict(*result.conflictState)
		} else if request.rebind && errors.Is(result.err, diskversion.ErrConflict) {
			ws.saveFailed(fmt.Errorf("Save As destination changed while saving; choose a new file name: %w", result.err))
		} else {
			ws.saveFailed(result.err)
		}
		ws.completeSaveCallbacks(job.callbacks, false)
		return
	}

	if ws.CommandStackId() != request.stackID {
		ws.saveFailed(fmt.Errorf("map path changed while saving %q", request.path))
		ws.completeSaveCallbacks(job.callbacks, false)
		return
	}
	source := ws.paneMap.Dmm()
	if request.rebind {
		if !ws.paneMap.Editor().SaveCaptureAttached(request.capture) {
			ws.saveFailed(fmt.Errorf("map attachment changed while saving to %q", request.path))
			ws.completeSaveCallbacks(job.callbacks, false)
			return
		}
		if !ws.app.CommandStorage().CanRebindStack(request.stackID, request.path) || !ws.app.CommandStorage().RebindStack(request.stackID, request.path) {
			ws.saveFailed(fmt.Errorf("map history changed while saving to %q", request.path))
			ws.completeSaveCallbacks(job.callbacks, false)
			return
		}
		source.Name = request.metadata.Name
		source.Path = request.metadata.Path
		ws.untitled = false // APHELION EDIT ADDITION - JOIN INTO NEW DOCUMENT
	}
	if job.acceptedKeyLength != nil && *job.acceptedKeyLength > ws.approvedKeyLength { // APHELION EDIT ADDITION - KEY_LENGTH_WARNING
		ws.approvedKeyLength = *job.acceptedKeyLength
	}
	source.DiskState = result.diskState
	ws.savedMapHash = result.mapHash
	ws.savedGeneration = request.capture.Generation
	ws.savedRevision = request.capture.Revision
	ws.diskConflict = false
	stackID := ws.CommandStackId()
	ws.pendingSaveAck = &saveAcknowledgement{
		id:        request.id,
		lifetime:  request.lifetime,
		capture:   request.capture,
		stackID:   stackID,
		callbacks: job.callbacks,
	}
	ws.tryCompleteSaveAcknowledgement()

	if job.repeat && ws.pendingSaveAck == nil && !ws.disposed {
		generation, revision := ws.paneMap.Editor().SaveVersion()
		if generation != request.capture.Generation || revision != request.capture.Revision {
			ws.saveAtPath(ws.CommandStackId(), source.DiskState, false, nil)
		}
	}
}

func (ws *WsMap) tryCompleteSaveAcknowledgement() {
	ack := ws.pendingSaveAck
	if ack == nil {
		return
	}
	if ws.disposed || ws.saveLifetime != ack.lifetime || ws.saveRequestID != ack.id || ws.CommandStackId() != ack.stackID || !ws.paneMap.Editor().SaveCaptureAttached(ack.capture) {
		ws.pendingSaveAck = nil
		ws.completeSaveCallbacks(ack.callbacks, false)
		return
	}
	generation, revision := ws.paneMap.Editor().SaveVersion()
	if generation != ack.capture.Generation || revision < ack.capture.Revision {
		return
	}
	if revision > ack.capture.Revision || !ws.paneMap.Editor().SaveCaptureReady(ack.capture) {
		ws.pendingSaveAck = nil
		ws.completeSaveCallbacks(ack.callbacks, false)
		return
	}
	ws.app.CommandStorage().ForceBalance(ack.stackID)
	ws.pendingSaveAck = nil
	ws.completeSaveCallbacks(ack.callbacks, true)
}

func (ws *WsMap) completeSaveCallbacks(callbacks []func(bool), saved bool) {
	for _, callback := range callbacks {
		if callback != nil {
			callback(saved)
		}
	}
}

func (ws *WsMap) rejectSave(callback func(bool), err error) bool {
	ws.saveFailed(err)
	if callback != nil {
		callback(false)
	}
	return false
}

// keyLengthConfirmer runs on the save worker. The worker owns an immutable
// capture, so waiting for the answer cannot race with edits; it only holds the
// admitted save open. The dialog is raised on the UI thread.
func (ws *WsMap) keyLengthConfirmer(ctx context.Context, accepted *int) func(dmmsave.KeyLengthChange) bool {
	return func(change dmmsave.KeyLengthChange) bool {
		answer := make(chan bool, 1)
		ws.app.RunLater(func() {
			if ctx.Err() != nil {
				return
			}
			// The save stages against the load-time backup, so a file already re-keyed
			// by an approved save in this session would otherwise prompt again.
			if change.Plan.Required <= ws.approvedKeyLength {
				answer <- true
				*accepted = change.Plan.Required
				return
			}
			present := ws.presentKeyLengthChange
			if present == nil {
				present = presentKeyLengthDialog
			}
			present(change, func(confirmed bool) {
				if confirmed {
					*accepted = change.Plan.Required
				}
				select {
				case answer <- confirmed:
				default:
				}
			})
		})
		select {
		case confirmed := <-answer:
			return confirmed
		case <-ctx.Done():
			ws.app.RunLater(func() { dialog.Close(dialog.TypeCustom{Title: keyLengthDialogTitle(change.Path)}) })
			return false
		}
	}
}

func keyLengthDialogTitle(path string) string { return "Key length will change##" + path }

func keyLengthWarning(change dmmsave.KeyLengthChange) (string, string) {
	plan := change.Plan
	return fmt.Sprintf("Saving will change the key length from %d to %d; every tile key in %s will change (large diff).", plan.Current, plan.Required, filepath.Base(change.Path)),
		fmt.Sprintf("Unique tile contents: %d (capacity at %d: %d)", plan.Unique, plan.Current, plan.CurrentCapacity)
}

func presentKeyLengthDialog(change dmmsave.KeyLengthChange, answer func(bool)) {
	warning, detail := keyLengthWarning(change)
	choose := func(confirmed bool) func() {
		return func() {
			imgui.CloseCurrentPopup()
			answer(confirmed)
		}
	}
	dialog.Open(dialog.TypeCustom{
		Title: keyLengthDialogTitle(change.Path),
		Layout: w.Layout{
			w.Text(warning),
			w.Text(detail),
			w.Button("Save anyway", choose(true)),
			w.SameLine(),
			w.Button("Cancel", choose(false)),
		},
		CloseButton: false,
	})
}

func (ws *WsMap) showDiskConflict(state diskversion.State) {
	ws.diskConflict = true
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
}

func (ws *WsMap) overwriteAfterConflict(state diskversion.State) bool {
	return ws.saveUsingDiskState(state)
}

func (ws *WsMap) overwriteAfterConflictAsync(state diskversion.State, callback func(bool)) {
	ws.saveAtPath(ws.CommandStackId(), state, false, callback)
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
	// APHELION EDIT CHANGE - SAVE AS REPLACE - ORIGINAL: ws.saveAsTo(path)
	ws.saveAsConfirmingReplace(path)
}

// APHELION EDIT ADDITION START - SAVE AS REPLACE

// SaveAs asks for a destination and saves the map there.
func (ws *WsMap) SaveAs() { ws.chooseSaveAs() }

// saveAsConfirmingReplace saves to path, asking first when it already exists.
// Choosing the map's own file is an ordinary save.
func (ws *WsMap) saveAsConfirmingReplace(path string) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		ws.rejectSave(nil, fmt.Errorf("resolve Save As destination: %w", err))
		return
	}
	if !ws.untitled && filepath.Clean(absolutePath) == filepath.Clean(ws.CommandStackId()) {
		ws.Save()
		return
	}
	state, err := diskversion.Capture(absolutePath)
	if err != nil {
		ws.rejectSave(nil, fmt.Errorf("inspect Save As destination %q: %w", absolutePath, err))
		return
	}
	if !state.Exists() {
		ws.saveAsTo(absolutePath)
		return
	}
	dialog.Open(dialog.TypeCustom{
		Title: "Replace existing map?##" + absolutePath,
		Layout: w.Layout{
			w.Text("This file already exists. Replace it with the current editor state?"),
			w.Text(absolutePath),
			w.Button("Replace", func() {
				imgui.CloseCurrentPopup()
				ws.app.RunLater(func() { ws.saveAsReplacingAsync(absolutePath, state, nil) })
			}),
			w.SameLine(),
			w.Button("Cancel", imgui.CloseCurrentPopup),
		},
		CloseButton: false,
	})
}

// saveAsReplacingAsync replaces an existing destination the user confirmed.
// expected is the destination's disk state when they confirmed; if the file
// changes before the atomic replace, the save is refused instead.
func (ws *WsMap) saveAsReplacingAsync(absolutePath string, expected diskversion.State, callback func(bool)) bool {
	if !ws.app.CommandStorage().CanRebindStack(ws.CommandStackId(), absolutePath) {
		return ws.rejectSave(callback, fmt.Errorf("%q is open in another tab; close it before replacing it", absolutePath))
	}
	return ws.saveAtPath(absolutePath, expected, true, callback)
}

// APHELION EDIT ADDITION END

func (ws *WsMap) saveAsTo(path string) bool {
	return ws.saveAsToAsync(path, nil)
}

func (ws *WsMap) saveAsToAsync(path string, callback func(bool)) bool {
	if filepath.Ext(path) == "" {
		path += ".dmm"
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return ws.rejectSave(callback, fmt.Errorf("resolve Save As destination: %w", err))
	}
	if _, err := os.Lstat(absolutePath); err == nil {
		return ws.rejectSave(callback, fmt.Errorf("Save As destination already exists; choose a new file name: %q", absolutePath))
	} else if !errors.Is(err, os.ErrNotExist) {
		return ws.rejectSave(callback, fmt.Errorf("inspect Save As destination %q: %w", absolutePath, err))
	}
	return ws.saveAtPath(absolutePath, diskversion.Absent(), true, callback)
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - ATOMIC_SAVE
func (ws *WsMap) saveFailed(err error) {
	log.Error().Err(err).Msg("unable to save map workspace")
	ws.app.RunLater(func() { util.ShowErrorDialog("Unable to save the map: " + err.Error()) })
}

// HasUnsavedChanges checks authority when a close decision is made. The tab
// label uses a cheap version check instead of hashing the map every frame.
func (ws *WsMap) HasUnsavedChanges() bool {
	if ws.untitled { // APHELION EDIT ADDITION - JOIN INTO NEW DOCUMENT: never saved, so closing must offer Save As.
		return true
	}
	if ws.activeSave != nil || ws.pendingSaveAck != nil || ws.diskConflict || ws.app.CommandStorage().IsModified(ws.CommandStackId()) {
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
