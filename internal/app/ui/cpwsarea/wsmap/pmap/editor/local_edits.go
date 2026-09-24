// APHELION EDIT ADDITION START - LOCAL EDIT ADAPTER
package editor

import (
	"context"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/command"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// This capability is selected only under explicit unshared ownership. A
// disconnected or single-participant session never acquires it implicitly.
type localEditExecutor interface {
	executor.Executor
	LocalVersion(context.Context) (engine.LocalVersion, error)
	ApplyLocal(context.Context, engine.LocalRequest) (engine.LocalAcceptance, error)
}

func (e *Editor) commitLocal(execution localEditExecutor, message string, changes []model.TileChange, selectionOutcome func(bool), repeatAccepted func()) {
	version, err := execution.LocalVersion(context.Background())
	if err == nil && (version.DocumentID != e.authoritative.DocumentID || version.Revision != e.authoritative.Revision) {
		err = fmt.Errorf("local authority changed before submission")
	}
	var accepted engine.LocalAcceptance
	if err == nil {
		accepted, err = execution.ApplyLocal(context.Background(), engine.LocalRequest{Version: version, Changes: changes})
	}
	if err != nil {
		selectionApplied(selectionOutcome, false)
		e.rejectSpeculation(execution, err)
		return
	}
	e.pendingChanges = make(map[model.Coord]model.TileState)
	if len(accepted.Changes) == 0 {
		selectionApplied(selectionOutcome, false)
		return
	}
	if err := e.installLocalAcceptance(accepted, false); err != nil {
		e.reportCollaborationError("Unable to display accepted edit", err)
		return
	}
	if repeatAccepted != nil {
		repeatAccepted()
	}
	selectionApplied(selectionOutcome, true)
	e.pushLocalCommand(execution, message, accepted.Changes, selectionOutcome)
}

func (e *Editor) pushLocalCommand(execution localEditExecutor, message string, changes []model.TileChange, selectionOutcome func(bool)) {
	forward := model.CloneOperation(model.Operation{Changes: changes}).Changes
	backward := make([]model.TileChange, len(forward))
	for i, change := range forward {
		backward[i] = model.TileChange{Coord: change.Coord, Before: change.After, After: change.Before}
	}
	generation := e.historyGeneration
	apply := func(delta []model.TileChange, applied bool, complete func(error)) {
		if generation != e.historyGeneration || e.executor != execution || e.sessionOwned {
			complete(fmt.Errorf("editor ownership changed"))
			return
		}
		if e.HasPastePlacement() {
			complete(fmt.Errorf("confirm or cancel paste before undo/redo"))
			return
		}
		version, err := execution.LocalVersion(context.Background())
		if err == nil {
			var accepted engine.LocalAcceptance
			accepted, err = execution.ApplyLocal(context.Background(), engine.LocalRequest{Version: version, Changes: delta})
			if err == nil {
				err = e.installLocalAcceptance(accepted, true)
			}
		}
		if err != nil {
			e.reportCollaborationError("Unable to apply local history", err)
		} else {
			selectionApplied(selectionOutcome, applied)
		}
		complete(err)
	}
	if !e.history.Push(command.MakeAsync(message, func(complete func(error)) { apply(backward, false, complete) }, func(complete func(error)) { apply(forward, true, complete) })) {
		e.collaborationErr = fmt.Errorf("map command history was disposed")
		e.reportCollaborationError("Unable to record map change", e.collaborationErr)
	}
}

// installLocalAcceptance updates only accepted tiles and their derived data.
// Full installs remain the explicit attach/recovery/resize boundary.
func (e *Editor) installLocalAcceptance(accepted engine.LocalAcceptance, applyDisplay bool) error {
	if accepted.DocumentID != e.authoritative.DocumentID || accepted.Revision != e.authoritative.Revision+1 {
		return fmt.Errorf("local acceptance is not contiguous with displayed authority")
	}
	coords := make([]model.Coord, 0, len(accepted.Changes))
	points := make([]util.Point, 0, len(accepted.Changes))
	areasChanged := false
	for _, change := range accepted.Changes {
		if applyDisplay {
			if err := e.applyPasteTileState(change.Coord, change.After); err != nil {
				return err
			}
		}
		point := util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z}
		// Persist only touched values, preserving collision-adjusted references.
		for _, instance := range e.dmm.GetTile(point).Instances() {
			instance.SetPrefab(dmmap.PrefabStorage.Put(instance.Prefab()))
		}
		state := model.CloneTileState(change.After)
		e.authoritativeTiles[change.Coord] = state
		if index, exists := e.authoritativePositions[change.Coord]; exists {
			e.authoritative.Tiles[index].State = state
		} else {
			e.authoritativePositions[change.Coord] = len(e.authoritative.Tiles)
			e.authoritative.Tiles = append(e.authoritative.Tiles, model.Tile{Coord: change.Coord, State: state})
		}
		areasChanged = areasChanged || !areaState(change.Before).Equal(areaState(change.After))
		coords = append(coords, change.Coord)
		points = append(points, point)
	}
	e.authoritative.Revision = accepted.Revision
	e.mapViewGeneration++
	e.syncPasteSnapshot(coords)
	if areasChanged {
		e.updateAreasZones()
	}
	e.updateBucket(e.pMap.ActiveLevel(), points)
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	return nil
}

func areaState(state model.TileState) model.TileState {
	var result model.TileState
	for _, prefab := range state.Prefabs {
		if dm.IsPath(prefab.Path, "/area") {
			result.Prefabs = append(result.Prefabs, prefab)
		}
	}
	return result
}
// APHELION EDIT ADDITION END
