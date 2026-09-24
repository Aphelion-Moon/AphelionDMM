// APHELION EDIT ADDITION START - LOCAL RESIZE
package editor

import (
	"context"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/command"
)

type resizeCheckpoint struct {
	execution         *executor.Local
	historyGeneration uint64
	boundaryHash      string
}

// ResizeMap is exclusive local maintenance. A new size has its own local
// document/history; v1 network operations still cannot change dimensions.
func (e *Editor) ResizeMap(x, y, z int) error {
	if !e.CanChangeMapSize() {
		return fmt.Errorf("finish the current edit and use a healthy local map before resizing")
	}
	if x == e.authoritative.MaxX && y == e.authoritative.MaxY && z == e.authoritative.MaxZ {
		return nil
	}
	if _, err := (model.Snapshot{MaxX: x, MaxY: y, MaxZ: z}).CellCount(); err != nil {
		return err
	}
	local := e.executor.(*executor.Local)
	before, err := local.Snapshot(context.Background())
	if err != nil {
		return err
	}
	environment := e.app.LoadedEnvironment()
	hash, err := mapadapter.EnvironmentHash(environment)
	if err != nil {
		return err
	}
	if hash != before.EnvironmentHash {
		return fmt.Errorf("resize map: the loaded environment changed")
	}
	var turf, area string
	if world := environment.Objects["/world"]; world != nil && world.Vars != nil {
		turf, _ = world.Vars.Value("turf")
		area, _ = world.Vars.Value("area")
	}
	after, err := editing.Resize(before, x, y, z, turf, area)
	if err != nil {
		return err
	}
	document, err := engine.NewUnsharedDocument(after)
	if err != nil {
		return err
	}
	next, err := executor.NewLocal(document, e.actorID)
	if err != nil {
		return err
	}
	beforeHash, err := before.Hash()
	if err != nil {
		return err
	}
	afterHash, err := after.Hash()
	if err != nil {
		return err
	}
	previous := resizeCheckpoint{execution: local, historyGeneration: e.historyGeneration, boundaryHash: beforeHash}
	following := resizeCheckpoint{execution: next, boundaryHash: afterHash}
	if err := e.installResizeCheckpoint(previous, &following); err != nil {
		return err
	}
	e.history.Push(command.MakeAsync("Set Map Size", func(complete func(error)) {
		err := e.installResizeCheckpoint(following, &previous)
		if err != nil {
			e.reportCollaborationError("Unable to undo map resize", err)
		}
		complete(err)
	}, func(complete func(error)) {
		err := e.installResizeCheckpoint(previous, &following)
		if err != nil {
			e.reportCollaborationError("Unable to redo map resize", err)
		}
		complete(err)
	}))
	return nil
}

func (e *Editor) installResizeCheckpoint(expected resizeCheckpoint, target *resizeCheckpoint) error {
	if e.historyGeneration != expected.historyGeneration || !e.CanChangeMapSize() {
		return fmt.Errorf("map resize belongs to an inactive local history or unfinished edit")
	}
	current, err := e.executor.Snapshot(context.Background())
	if err != nil {
		return err
	}
	currentHash, err := current.Hash()
	if err != nil {
		return err
	}
	if currentHash != expected.boundaryHash {
		return fmt.Errorf("map changed since the resize history boundary")
	}
	snapshot, err := target.execution.Snapshot(context.Background())
	if err != nil {
		return err
	}
	targetHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	if targetHash != target.boundaryHash {
		return fmt.Errorf("retained resize history changed while inactive")
	}
	environment := e.app.LoadedEnvironment()
	hash, err := mapadapter.EnvironmentHash(environment)
	if err != nil {
		return err
	}
	if hash != snapshot.EnvironmentHash {
		return fmt.Errorf("map resize belongs to another environment")
	}
	// Apply into a separate display map. Validation/allocation happens before
	// switching authority, dimensions, active level or command ownership.
	candidate := *e.dmm
	if err := mapadapter.ApplyWithEnvironment(&candidate, snapshot, environment); err != nil {
		return err
	}
	e.resetAttachment() // Callback fencing is monotonic even when history resumes.
	if target.historyGeneration == 0 {
		target.historyGeneration = e.historyGeneration
	}
	e.historyGeneration = target.historyGeneration
	e.executor = target.execution
	e.documentID = snapshot.DocumentID
	e.collaborationErr = nil
	*e.dmm = candidate
	e.setAuthoritative(snapshot)
	if e.pMap.ActiveLevel() > snapshot.MaxZ {
		e.pMap.SetActiveLevel(snapshot.MaxZ)
	}
	e.pMap.Snapshot().Sync()
	e.updateAreasZones()
	e.pMap.OnMapSizeChange()
	e.dmm.PersistPrefabs()
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	return nil
}

// APHELION EDIT ADDITION END
