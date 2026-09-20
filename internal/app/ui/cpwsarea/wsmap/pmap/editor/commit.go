package editor

import (
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	// APHELION EDIT REMOVAL - LOCAL RESIZE - ORIGINAL: "sdmm/internal/app/command"
	"sdmm/internal/app/window"
	"sdmm/internal/util"
)

/* APHELION EDIT REMOVAL START - LOCAL RESIZE
The settings panel now calls ResizeMap before changing the display map.
func (e *Editor) CommitMapSizeChange(oldMaxX, oldMaxY, oldMaxZ int) {
	initialMapTiles := e.pMap.Snapshot().Initial().Copy().Tiles // Remember initial tiles to restore them on undo.
	newMaxX, newMaxY, newMaxZ := e.dmm.MaxX, e.dmm.MaxY, e.dmm.MaxZ

	e.onMapSizeChange(e.dmm.MaxZ)

	e.app.CommandStorage().Push(command.Make("Set Map Size", func() {
		e.dmm.SetMapSize(oldMaxX, oldMaxY, oldMaxZ)
		e.dmm.Tiles = initialMapTiles
		e.onMapSizeChange(oldMaxZ)
	}, func() {
		e.dmm.SetMapSize(newMaxX, newMaxY, newMaxZ)
		e.onMapSizeChange(newMaxZ)
	}))
}

func (e *Editor) onMapSizeChange(maxZ int) {
	// Ensure we are on the visible level.
	if e.pMap.ActiveLevel() > maxZ {
		e.pMap.SetActiveLevel(maxZ)
	}
	e.pMap.Snapshot().Sync() // Do a full snapshots sync.
	e.pMap.OnMapSizeChange()
	e.updateAreasZones()
	// APHELION EDIT ADDITION START - COLLABORATION
	e.initializeCollaboration()
	// APHELION EDIT ADDITION END
}
APHELION EDIT REMOVAL END */

// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: CommitChanges used asynchronous snapshot comparison.
func (e *Editor) CommitOperation(commitMsg string) {
	// APHELION EDIT ADDITION START - COLLABORATION
	e.commitWithAuthority(commitMsg)
	// APHELION EDIT ADDITION END
}

/* APHELION EDIT REMOVAL START - OPERATION AUTHORITY
Retain the inherited snapshot-history implementation as provenance. Failed
authority must not select another commit engine.
// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: func (e *Editor) commitChanges(commitMsg string)
func (e *Editor) commitChangesLegacy(commitMsg string) {
	stateId, tilesToUpdate := e.pMap.Snapshot().Commit()

	// Do not push command if there is no tiles to update.
	if len(tilesToUpdate) == 0 {
		return
	}

	// Copy the value to pass it to the lambda.
	activeLevel := e.pMap.ActiveLevel()

	// Ensure that the user has updated visuals.
	e.updateAreasZones()
	e.updateBucket(activeLevel, tilesToUpdate)

	e.app.CommandStorage().Push(command.Make(commitMsg, func() {
		e.pMap.Snapshot().GoTo(stateId - 1)
		e.updateAreasZones()
		e.updateBucket(activeLevel, tilesToUpdate)
		e.dmm.PersistPrefabs()
		e.app.SyncPrefabs()
		e.app.SyncVarEditor()
	}, func() {
		e.pMap.Snapshot().GoTo(stateId)
		e.updateAreasZones()
		e.updateBucket(activeLevel, tilesToUpdate)
		e.dmm.PersistPrefabs()
		e.app.SyncPrefabs()
		e.app.SyncVarEditor()
	}))
}
APHELION EDIT REMOVAL END */

// We need to update bucket in the main thread, since it can have OpenGL operations.
// RunLater do that by running the job in th end of the frame.
func (e *Editor) updateBucket(activeLevel int, tilesToUpdate []util.Point) {
	// APHELION EDIT CHANGE - UI STAGE TRACE - ORIGINAL: window.RunLater(func() {
	window.RunLater(uistage.DeferredBucket(func() {
		e.pMap.Canvas().Render().UpdateBucketV(e.dmm, activeLevel, tilesToUpdate)
		// APHELION EDIT CHANGE - UI STAGE TRACE - ORIGINAL: })
	}))
}
