// APHELION EDIT ADDITION START - INCREMENTAL COLLABORATION
package editor

import (
	"context"
	"fmt"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

type presentationExecutor interface {
	executor.Executor
	TakePresentationUpdate() *client.PresentationUpdate
}

// CollaborationPublicationStats distinguishes delta installation from the
// explicit recovery boundary. Read only on the editor owner, like MapViewVersion.
type CollaborationPublicationStats struct {
	Publications       uint64
	ReconstructedTiles uint64
	DirtyTiles         uint64
	FullReplacements   uint64
}

func (e *Editor) CollaborationPublicationStats() CollaborationPublicationStats {
	return e.presentationStats
}

func (e *Editor) CollaborationSynchronizing() bool {
	if e.presentationUpdate != nil || len(e.unresolvedSubmissions) != 0 {
		return true
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return true
	}
	if pending, ok := e.executor.(interface{ HasPresentationUpdate() bool }); ok {
		return pending.HasPresentationUpdate()
	}
	return false
}

func (e *Editor) processPresentationUpdate(execution presentationExecutor) {
	if e.presentationUpdate == nil {
		e.presentationUpdate = execution.TakePresentationUpdate()
	}
	update := e.presentationUpdate
	if update == nil {
		return
	}
	if update.Sequence <= e.presentationSequence {
		e.presentationUpdate = nil
		return
	}
	if err := e.installPresentationUpdate(update); err != nil {
		// No cursor advancement or queue drain on failure. Preserve the old
		// coherent map and the failed update for explicit recovery/reattachment.
		e.collaborationErr = err
		e.reportCollaborationError("Unable to display collaborative changes", err)
		return
	}
	e.presentationSequence = update.Sequence
	e.presentationUpdate = nil
	e.presentationStats.Publications++
	e.revalidateSelectionMovePreview()
}

type preparedCollaborationTile struct {
	coord     model.Coord
	instances dmmap.Instances
	replace   bool
	derived   model.TileChange
}

func (e *Editor) installPresentationUpdate(update *client.PresentationUpdate) error {
	defer uistage.Begin(uistage.Refresh).End()
	defer resources.ChargeFrameWork(time.Now())
	if update.DocumentID != e.authoritative.DocumentID || update.EnvironmentHash != e.authoritative.EnvironmentHash {
		return fmt.Errorf("collaboration publication belongs to another document or environment")
	}
	if update.Revision < e.authoritative.Revision {
		return fmt.Errorf("collaboration publication precedes displayed authority")
	}
	if update.Replacement != nil {
		return e.installPresentationReplacement(update)
	}
	// NetworkExecutor has validated complete batches and identity ownership.
	// Check the attachment's dimensions and stage fallible tile conversion before
	// touching any live tile, compatibility snapshot, or authority index.
	for _, tile := range update.Authoritative {
		if !e.dmm.HasTile(util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}) {
			return fmt.Errorf("authoritative publication coordinate is outside the attached map")
		}
	}
	prepared := make([]preparedCollaborationTile, 0, len(update.Display))
	initial := e.pMap.Snapshot().Initial()
	for _, tile := range update.Display {
		point := util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}
		if !e.dmm.HasTile(point) {
			return fmt.Errorf("display publication coordinate is outside the attached map")
		}
		current, err := mapadapter.CaptureTile(e.dmm.GetTile(point))
		if err != nil {
			return err
		}
		before := current
		if initial != nil && initial.HasTile(point) {
			before, err = mapadapter.CaptureTile(initial.GetTile(point))
			if err != nil {
				return err
			}
		}
		if current.Equal(tile.State) && before.Equal(tile.State) {
			continue
		}
		item := preparedCollaborationTile{coord: tile.Coord, replace: !current.Equal(tile.State), derived: model.TileChange{Coord: tile.Coord, Before: before, After: tile.State}}
		if item.replace {
			item.instances, err = e.preparePasteTileState(tile.Coord, tile.State)
			if err != nil {
				return err
			}
		}
		prepared = append(prepared, item)
	}
	coords := make([]model.Coord, 0, len(prepared))
	levels := make(map[int][]util.Point)
	for _, item := range prepared {
		point := util.Point{X: item.coord.X, Y: item.coord.Y, Z: item.coord.Z}
		if item.replace {
			e.dmm.GetTile(point).Set(item.instances)
			e.presentationStats.ReconstructedTiles++
		} else {
			for _, instance := range e.dmm.GetTile(point).Instances() {
				instance.SetPrefab(dmmap.PrefabStorage.Put(instance.Prefab()))
			}
		}
		e.updateAreaDelta(item.derived)
		coords = append(coords, item.coord)
		levels[item.coord.Z] = append(levels[item.coord.Z], point)
	}
	for _, tile := range update.Authoritative {
		e.setAuthoritativeTile(tile.Coord, tile.State)
	}
	e.authoritative.Revision = update.Revision
	if len(coords) == 0 {
		return nil
	}
	e.mapViewGeneration++
	e.presentationStats.DirtyTiles += uint64(len(coords))
	e.syncPasteSnapshot(coords)
	// Publication is already on the graphics owner. Finish geometry for this
	// model generation before input/picking can observe it; empty means no work.
	if renderer := e.pMap.Canvas().Render(); renderer != nil {
		renderer.BeginUpdateBatch()
		for level, points := range levels {
			renderer.UpdateBucketV(e.dmm, level, points)
		}
		renderer.EndUpdateBatch(e.dmm)
	}
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	return nil
}

func (e *Editor) installPresentationReplacement(update *client.PresentationUpdate) error {
	// Recovery can be coalesced with later accepted edits and pending intent.
	// Stage both layers on a detached DMM so even a failed replacement leaves
	// the previous display and its indexes intact.
	authority := model.CloneSnapshot(*update.Replacement)
	if authority.DocumentID != update.DocumentID || authority.EnvironmentHash != update.EnvironmentHash {
		return fmt.Errorf("recovery baseline belongs to another document or environment")
	}
	positions := make(map[model.Coord]int, len(authority.Tiles))
	for i, tile := range authority.Tiles {
		positions[tile.Coord] = i
	}
	for _, tile := range update.Authoritative {
		if i, ok := positions[tile.Coord]; ok {
			authority.Tiles[i] = tile
		} else {
			positions[tile.Coord] = len(authority.Tiles)
			authority.Tiles = append(authority.Tiles, tile)
		}
	}
	authority.Revision = update.Revision
	hash, err := authority.Hash()
	if err != nil {
		return fmt.Errorf("recovery publication authority failed integrity validation: %v", err)
	}
	if hash != update.MapHash {
		return fmt.Errorf("recovery publication authority hash mismatch")
	}
	candidate := *e.dmm
	if err := mapadapter.ApplyWithEnvironment(&candidate, authority, e.app.LoadedEnvironment()); err != nil {
		return err
	}
	staging := &Editor{app: e.app, dmm: &candidate}
	for _, tile := range update.Display {
		if err := staging.applyPasteTileState(tile.Coord, tile.State); err != nil {
			return err
		}
	}
	*e.dmm = candidate
	e.setAuthoritative(authority)
	e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, authority)
	e.presentationStats.FullReplacements++
	log.Debug().Uint64("sequence", update.Sequence).Msg("Collaboration full replacement: reconnect snapshot")
	return nil
}

func (e *Editor) operationForChanges(execution executor.Executor, changes []model.TileChange) (model.Operation, error) {
	if capturer, ok := execution.(interface {
		CaptureProjection(context.Context) (client.ProjectionCapture, error)
	}); ok {
		capture, err := capturer.CaptureProjection(context.Background())
		if err != nil {
			return model.Operation{}, err
		}
		document, revision, environment, hash, err := capture.OperationBase()
		if err != nil {
			return model.Operation{}, err
		}
		id, err := model.NewOperationID()
		if err != nil {
			return model.Operation{}, err
		}
		return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: document, ActorID: e.actorID, OperationID: id, BaseRevision: revision, EnvironmentHash: environment, BaseMapHash: hash, Kind: model.OperationKindTileChange, Changes: changes}, nil
	}
	snapshot, err := execution.Snapshot(context.Background())
	if err != nil {
		return model.Operation{}, err
	}
	return e.forwardOperation(snapshot, changes)
}

func (e *Editor) submitPresentationPaste(p *pasteSession, execution executor.Executor, changes []model.TileChange) {
	operation, err := e.operationForChanges(execution, changes)
	if err != nil {
		p.err, p.intent = err, nil
		return
	}
	p.phase = pasteResolving
	e.retainPasteCommit(p)
	generation := e.attachmentGeneration
	complete := func(accepted model.AcceptedOperation, executeErr error) {
		e.app.RunLater(func() {
			defer e.finishPasteCommit(p)
			if generation != e.attachmentGeneration || e.paste != p {
				return
			}
			if executeErr != nil {
				p.err, p.intent, p.phase = executeErr, nil, pasteReady
				return
			}
			coords := make([]model.Coord, len(accepted.Changes))
			for i, change := range accepted.Changes {
				coords[i] = change.Coord
			}
			e.pushAcceptedCommand(execution, "Paste Tiles", accepted, accepted.Changes, p.level, coords, p.selectionOutcome)
			e.finishPaste(p, true)
			e.ProcessCollaborationUpdates()
		})
	}
	if asynchronous, ok := execution.(executor.AsyncExecutor); ok {
		if err := asynchronous.ExecuteAsync(context.Background(), operation, complete); err != nil {
			complete(model.AcceptedOperation{}, err)
		}
	} else {
		accepted, err := execution.Execute(context.Background(), operation)
		complete(accepted, err)
	}
}

func (e *Editor) prepareHistoryInverse(execution executor.Executor, target model.OperationID, generation uint64, complete func(model.Operation, error)) {
	if _, incremental := execution.(presentationExecutor); !incremental {
		inverse, err := execution.BuildInverse(context.Background(), target)
		complete(inverse, err)
		return
	}
	// Inverse preparation needs retained accepted history and current-value
	// checks. It may wait for verification, so keep it off the input owner.
	go func() {
		inverse, err := execution.BuildInverse(context.Background(), target)
		e.app.RunLater(func() {
			if generation != e.historyGeneration || execution != e.executor {
				complete(model.Operation{}, fmt.Errorf("editor attachment changed"))
				return
			}
			complete(inverse, err)
		})
	}()
}

// APHELION EDIT ADDITION END
