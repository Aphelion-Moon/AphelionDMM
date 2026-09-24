// APHELION EDIT ADDITION START - COLLABORATION
package editor

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/app/command"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

func (e *Editor) initializeCollaboration() {
	e.resetAttachment()
	if e.documentID == "" {
		documentID, err := model.NewDocumentID()
		if err != nil {
			e.collaborationErr = fmt.Errorf("initialize collaboration document: %w", err)
			return
		}
		e.documentID = documentID
	}
	if e.actorID == "" {
		actorID, err := model.NewActorID()
		if err != nil {
			e.collaborationErr = fmt.Errorf("initialize collaboration actor: %w", err)
			return
		}
		e.actorID = actorID
	}
	environmentHash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	if err != nil {
		e.collaborationErr = err
		return
	}
	snapshot, err := mapadapter.Import(e.dmm, e.documentID, environmentHash)
	if err != nil {
		e.collaborationErr = err
		return
	}
	document, err := engine.NewUnsharedDocument(snapshot)
	if err != nil {
		e.collaborationErr = err
		return
	}
	local, err := executor.NewLocal(document, e.actorID)
	if err != nil {
		e.collaborationErr = err
		return
	}
	e.executor = local
	e.sessionOwned = false
	e.collaborationErr = nil
	e.setAuthoritative(snapshot)
}

type projectionExecutor interface {
	executor.Executor
	ProjectionUpdates() <-chan client.Projection
}

type pendingExecutor interface {
	HasUnacknowledgedOperations() bool
}

// CollaborationSnapshot returns the committed map state used to start a collaboration session.
func (e *Editor) CollaborationSnapshot(ctx context.Context) (model.Snapshot, error) {
	if e.collaborationErr != nil {
		return model.Snapshot{}, fmt.Errorf("read collaboration snapshot: %w", e.collaborationErr)
	}
	if e.executor == nil {
		return model.Snapshot{}, fmt.Errorf("read collaboration snapshot: executor is unavailable")
	}
	if e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 {
		return model.Snapshot{}, fmt.Errorf("read collaboration snapshot: map has an uncommitted edit")
	}
	snapshot, err := e.executor.Snapshot(ctx)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("read collaboration snapshot: %w", err)
	}
	return model.CloneSnapshot(snapshot), nil
}

// AttachCollaborationExecutor replaces local compatibility mode with a synchronized executor.
func (e *Editor) AttachCollaborationExecutor(execution executor.Executor) error {
	if execution == nil {
		return fmt.Errorf("attach collaboration executor: executor is nil")
	}
	if e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 {
		return fmt.Errorf("attach collaboration executor: map has an uncommitted edit")
	}
	snapshot, err := execution.Snapshot(context.Background())
	if err != nil {
		return fmt.Errorf("attach collaboration executor: read snapshot: %w", err)
	}
	environmentHash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	if err != nil {
		return fmt.Errorf("attach collaboration executor: %w", err)
	}
	if snapshot.EnvironmentHash != environmentHash {
		return fmt.Errorf("attach collaboration executor: environment hash does not match the loaded project")
	}
	if err := mapadapter.ApplyWithEnvironment(e.dmm, snapshot, e.app.LoadedEnvironment()); err != nil {
		return fmt.Errorf("attach collaboration executor: %w", err)
	}
	e.resetAttachment()
	e.executor = execution
	e.sessionOwned = true
	e.collaborationErr = nil
	e.documentID = snapshot.DocumentID
	e.setAuthoritative(snapshot)
	e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, snapshot)
	return nil
}

// DetachCollaborationExecutor restores local compatibility mode from the synchronized snapshot.
func (e *Editor) DetachCollaborationExecutor(ctx context.Context) error {
	if e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 {
		return fmt.Errorf("detach collaboration executor: map has an uncommitted edit")
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return fmt.Errorf("detach collaboration executor: operations are awaiting acknowledgement")
	}
	snapshot, err := e.CollaborationSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("detach collaboration executor: %w", err)
	}
	document, err := engine.NewUnsharedDocument(snapshot)
	if err != nil {
		return fmt.Errorf("detach collaboration executor: %w", err)
	}
	local, err := executor.NewLocal(document, e.actorID)
	if err != nil {
		return fmt.Errorf("detach collaboration executor: %w", err)
	}
	if err := mapadapter.ApplyWithEnvironment(e.dmm, snapshot, e.app.LoadedEnvironment()); err != nil {
		return fmt.Errorf("detach collaboration executor: %w", err)
	}
	e.resetAttachment()
	e.executor = local
	e.sessionOwned = false
	e.setAuthoritative(snapshot)
	e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, snapshot)
	return nil
}

// ProcessCollaborationUpdates applies the latest network projection on the UI thread.
func (e *Editor) ProcessCollaborationUpdates() {
	// A fault without acquired captures still owns its damaged display. Keep
	// incoming projections queued until explicit recovery or attachment.
	if e.collaborationErr != nil {
		return
	}
	if e.selectionMove != nil && e.selectionMove.Level() != e.pMap.ActiveLevel() {
		e.FinishSelectionMove(e.selectionMove, true)
	}
	if e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 {
		return
	}
	execution, ok := e.executor.(projectionExecutor)
	if !ok {
		return
	}
	var latest *client.Projection
	for {
		select {
		case update := <-execution.ProjectionUpdates():
			projection := update
			latest = &projection
		default:
			if latest == nil {
				return
			}
			visible, err := latest.Visible()
			if err != nil {
				e.reportCollaborationError("Unable to project collaborative map", err)
				return
			}
			if err := mapadapter.ApplyWithEnvironment(e.dmm, visible, e.app.LoadedEnvironment()); err != nil {
				e.reportCollaborationError("Unable to project collaborative map", err)
				return
			}
			e.setAuthoritative(latest.Acknowledged)
			e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, visible)
			return
		}
	}
}

// RefreshCollaborationSnapshot applies the executor's authoritative snapshot on the UI thread.
func (e *Editor) RefreshCollaborationSnapshot(ctx context.Context) error {
	if e.collaborationErr != nil {
		return fmt.Errorf("inspect and explicitly discard the retained local edit before refreshing")
	}
	if e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 {
		return fmt.Errorf("refresh collaboration snapshot: map has an uncommitted edit")
	}
	if e.executor == nil {
		return fmt.Errorf("refresh collaboration snapshot: executor is unavailable")
	}
	snapshot, err := e.executor.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("refresh collaboration snapshot: %w", err)
	}
	if err := mapadapter.ApplyWithEnvironment(e.dmm, snapshot, e.app.LoadedEnvironment()); err != nil {
		return fmt.Errorf("refresh collaboration snapshot: %w", err)
	}
	e.setAuthoritative(snapshot)
	e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, snapshot)
	return nil
}

func (e *Editor) CanChangeMapSize() bool {
	_, local := e.executor.(*executor.Local)
	return local && e.history.Valid() && e.collaborationErr == nil && e.selectionMove == nil && e.paste == nil && len(e.pendingChanges) == 0 && len(e.unresolvedSubmissions) == 0
}

// TryBeginTileChange captures every target before a tool mutates the display.
// A failed batch releases only its newly acquired captures, retaining earlier
// gesture state and the capture fault that guards Save.
func (e *Editor) TryBeginTileChange(points ...util.Point) bool {
	if e.mapViewClosed || !e.history.Valid() || e.paste != nil || e.HasPastePlacement() {
		return false
	}
	acquired := make([]model.Coord, 0, len(points))
	for _, point := range points {
		coord := model.Coord{X: point.X, Y: point.Y, Z: point.Z}
		_, existed := e.pendingChanges[coord]
		e.BeginTileChange(point)
		_, captured := e.pendingChanges[coord]
		if e.executor == nil || e.collaborationErr != nil || !captured {
			for _, key := range acquired {
				delete(e.pendingChanges, key)
			}
			return false
		}
		if !existed {
			acquired = append(acquired, coord)
		}
	}
	return e.executor != nil && e.collaborationErr == nil
}

func (e *Editor) BeginTileChange(point util.Point) {
	defer uistage.Begin(uistage.CaptureTile).End()
	// Invalidate derived queries even when capture fails: inherited callers may
	// already be preparing a display edit. Queries defer while captures are open.
	e.mapViewGeneration++
	if e.collaborationErr != nil || e.executor == nil || e.paste != nil {
		return
	}
	coord := model.Coord{X: point.X, Y: point.Y, Z: point.Z}
	if _, exists := e.pendingChanges[coord]; exists {
		return
	}
	before, err := mapadapter.CaptureTile(e.dmm.GetTile(point))
	if err != nil {
		e.collaborationErr = err
		log.Error().Err(err).Msg("Unable to capture map change")
		return
	}
	e.pendingChanges[coord] = before
}

func (e *Editor) commitOperation(commitMessage string) {
	defer uistage.Begin(uistage.Commit).End()
	selectionOutcome := e.selectionOutcome
	repeatAccepted := e.repeatAccepted
	if len(e.pendingChanges) == 0 {
		selectionApplied(selectionOutcome, false)
		return
	}
	execution := e.executor
	coords := make([]model.Coord, 0, len(e.pendingChanges))
	for coord := range e.pendingChanges {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(left int, right int) bool {
		if coords[left].Z != coords[right].Z {
			return coords[left].Z < coords[right].Z
		}
		if coords[left].Y != coords[right].Y {
			return coords[left].Y < coords[right].Y
		}
		return coords[left].X < coords[right].X
	})
	changes := make([]model.TileChange, 0, len(coords))
	for _, coord := range coords {
		point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
		after, captureErr := mapadapter.CaptureTile(e.dmm.GetTile(point))
		if captureErr != nil {
			selectionApplied(selectionOutcome, false)
			e.rejectSpeculation(execution, captureErr)
			return
		}
		before := e.pendingChanges[coord]
		if before.Equal(after) {
			continue
		}
		changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
	}
	if len(changes) == 0 {
		e.pendingChanges = make(map[model.Coord]model.TileState)
		selectionApplied(selectionOutcome, false)
		return
	}
	// A restored/no-op gesture needs no full-map copy. Read authority only after
	// the touched tiles prove there is an operation to submit.
	if local, ok := execution.(localEditExecutor); ok && !e.sessionOwned {
		e.commitLocal(local, commitMessage, changes, selectionOutcome, repeatAccepted)
		return
	}
	base, err := execution.Snapshot(context.Background())
	if err != nil {
		selectionApplied(selectionOutcome, false)
		e.rejectSpeculation(execution, fmt.Errorf("read authoritative snapshot: %w", err))
		return
	}
	e.pendingChanges = make(map[model.Coord]model.TileState)
	operation, err := e.forwardOperation(base, changes)
	if err != nil {
		selectionApplied(selectionOutcome, false)
		e.rejectSpeculation(execution, err)
		return
	}
	acceptedChanges := model.CloneOperation(operation).Changes
	activeLevel := e.pMap.ActiveLevel()
	e.submitOperation(execution, operation, func(accepted model.AcceptedOperation) {
		if repeatAccepted != nil {
			repeatAccepted()
		}
		e.syncFromExecutor(execution, false, activeLevel, coords)
		selectionApplied(selectionOutcome, true)
		e.pushAcceptedCommand(execution, commitMessage, accepted, acceptedChanges, activeLevel, coords, selectionOutcome)
	}, selectionOutcome)
}

func (e *Editor) submitOperation(execution executor.Executor, operation model.Operation, accepted func(model.AcceptedOperation), selectionOutcome func(bool)) {
	defer uistage.Begin(uistage.Dispatch).End()
	generation := e.attachmentGeneration
	if asynchronous, ok := execution.(executor.AsyncExecutor); ok {
		e.unresolvedSubmissions[operation.OperationID] = struct{}{}
		err := asynchronous.ExecuteAsync(context.Background(), operation, func(result model.AcceptedOperation, executeErr error) {
			e.app.RunLater(func() {
				if generation != e.attachmentGeneration {
					return
				}
				if _, pending := e.unresolvedSubmissions[operation.OperationID]; !pending {
					return
				}
				delete(e.unresolvedSubmissions, operation.OperationID)
				if executeErr != nil {
					e.rejectSpeculation(execution, executeErr)
					selectionApplied(selectionOutcome, false)
					return
				}
				accepted(result)
			})
		})
		if err != nil {
			delete(e.unresolvedSubmissions, operation.OperationID)
			e.rejectSpeculation(execution, err)
			selectionApplied(selectionOutcome, false)
		}
		return
	}
	result, err := execution.Execute(context.Background(), operation)
	if err != nil {
		e.rejectSpeculation(execution, err)
		selectionApplied(selectionOutcome, false)
		return
	}
	accepted(result)
}

func (e *Editor) pushAcceptedCommand(execution executor.Executor, commitMessage string, accepted model.AcceptedOperation, acceptedChanges []model.TileChange, activeLevel int, coords []model.Coord, selectionOutcome func(bool)) {
	forwardID := accepted.OperationID
	generation := e.historyGeneration
	if !e.history.Push(command.MakeAsync(commitMessage, func(complete func(error)) {
		if generation != e.historyGeneration {
			complete(fmt.Errorf("editor attachment changed"))
			return
		}
		inverse, inverseErr := execution.BuildInverse(context.Background(), forwardID)
		if inverseErr != nil {
			e.reportCollaborationError("Unable to undo map change", inverseErr)
			complete(inverseErr)
			return
		}
		e.executeHistoryOperation(execution, inverse, func(_ model.AcceptedOperation, executeErr error) {
			if executeErr != nil {
				e.reportCollaborationError("Unable to undo map change", executeErr)
				complete(executeErr)
				return
			}
			e.syncFromExecutor(execution, true, activeLevel, coords)
			selectionApplied(selectionOutcome, false)
			complete(nil)
		})
	}, func(complete func(error)) {
		if generation != e.historyGeneration {
			complete(fmt.Errorf("editor attachment changed"))
			return
		}
		current, redoErr := execution.Snapshot(context.Background())
		if redoErr != nil {
			e.reportCollaborationError("Unable to redo map change", redoErr)
			complete(redoErr)
			return
		}
		redo, redoErr := e.forwardOperation(current, acceptedChanges)
		if redoErr != nil {
			e.reportCollaborationError("Unable to redo map change", redoErr)
			complete(redoErr)
			return
		}
		e.executeHistoryOperation(execution, redo, func(redone model.AcceptedOperation, executeErr error) {
			if executeErr != nil {
				e.reportCollaborationError("Unable to redo map change", executeErr)
				complete(executeErr)
				return
			}
			forwardID = redone.OperationID
			e.syncFromExecutor(execution, true, activeLevel, coords)
			selectionApplied(selectionOutcome, true)
			complete(nil)
		})
	})) {
		e.collaborationErr = fmt.Errorf("map command history was disposed")
		e.reportCollaborationError("Unable to record map change", e.collaborationErr)
	}
}

func (e *Editor) executeHistoryOperation(execution executor.Executor, operation model.Operation, complete func(model.AcceptedOperation, error)) {
	defer uistage.Begin(uistage.Dispatch).End()
	if e.HasPastePlacement() {
		complete(model.AcceptedOperation{}, fmt.Errorf("confirm or cancel paste before undo/redo"))
		return
	}
	generation := e.attachmentGeneration
	if asynchronous, ok := execution.(executor.AsyncExecutor); ok {
		e.unresolvedSubmissions[operation.OperationID] = struct{}{}
		err := asynchronous.ExecuteAsync(context.Background(), operation, func(result model.AcceptedOperation, executeErr error) {
			e.app.RunLater(func() {
				if generation != e.attachmentGeneration {
					complete(model.AcceptedOperation{}, fmt.Errorf("editor attachment changed"))
					return
				}
				delete(e.unresolvedSubmissions, operation.OperationID)
				complete(result, executeErr)
			})
		})
		if err != nil {
			delete(e.unresolvedSubmissions, operation.OperationID)
			complete(model.AcceptedOperation{}, err)
		}
		return
	}
	result, err := execution.Execute(context.Background(), operation)
	complete(result, err)
}

func (e *Editor) forwardOperation(snapshot model.Snapshot, changes []model.TileChange) (model.Operation, error) {
	operationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, fmt.Errorf("create operation id: %w", err)
	}
	baseHash, err := snapshot.Hash()
	if err != nil {
		return model.Operation{}, fmt.Errorf("hash operation base: %w", err)
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      snapshot.DocumentID,
		ActorID:         e.actorID,
		OperationID:     operationID,
		BaseRevision:    snapshot.Revision,
		EnvironmentHash: snapshot.EnvironmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}, nil
}

func (e *Editor) rejectSpeculation(execution executor.Executor, cause error) {
	e.syncFromExecutor(execution, true, e.pMap.ActiveLevel(), nil)
	e.reportCollaborationError("Unable to apply map change", cause)
}

func (e *Editor) syncFromExecutor(execution executor.Executor, apply bool, activeLevel int, coords []model.Coord) {
	snapshot, err := execution.Snapshot(context.Background())
	if err != nil {
		e.reportCollaborationError("Unable to synchronize map", err)
		return
	}
	if e.collaborationErr != nil {
		e.setAuthoritative(snapshot)
		return
	}
	if apply && e.selectionMove == nil && len(e.pendingChanges) == 0 {
		if err := mapadapter.ApplyWithEnvironment(e.dmm, snapshot, e.app.LoadedEnvironment()); err != nil {
			e.reportCollaborationError("Unable to synchronize map", err)
			return
		}
	}
	e.setAuthoritative(snapshot)
	e.refreshCollaborationView(activeLevel, coords, snapshot)
}

func (e *Editor) refreshCollaborationView(activeLevel int, coords []model.Coord, visible model.Snapshot) {
	defer uistage.Begin(uistage.Refresh).End()
	e.pMap.Snapshot().Sync()
	e.updateAreasZones()
	points := make([]util.Point, 0, len(coords))
	for _, coord := range coords {
		points = append(points, util.Point{X: coord.X, Y: coord.Y, Z: coord.Z})
	}
	if len(points) == 0 {
		for _, tile := range visible.Tiles {
			points = append(points, util.Point{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z})
		}
	}
	e.updateBucket(activeLevel, points)
	e.dmm.PersistPrefabs()
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
}

func (e *Editor) setAuthoritative(snapshot model.Snapshot) {
	e.mapViewGeneration++
	e.authoritative = model.CloneSnapshot(snapshot)
	e.authoritativeTiles = make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	e.authoritativePositions = make(map[model.Coord]int, len(snapshot.Tiles))
	for index, tile := range snapshot.Tiles {
		e.authoritativeTiles[tile.Coord] = model.CloneTileState(tile.State)
		e.authoritativePositions[tile.Coord] = index
	}
}

func (e *Editor) resetAttachment() {
	e.repeatTransforms.Clear()
	e.mapViewGeneration++
	e.discardPasteWithoutRestore()
	// An attachment reset may follow installation of a replacement snapshot.
	// Drop old preview ownership without restoring it over that new authority.
	if e.selectionMove != nil {
		e.selectionMove.Finish(false)
		e.selectionMove = nil
	}
	e.attachmentGeneration++
	e.historyGeneration = e.attachmentGeneration
	e.pendingChanges = make(map[model.Coord]model.TileState)
	e.unresolvedSubmissions = make(map[model.OperationID]struct{})
}

// Close fences queued UI completions before the pane releases its resources.
func (e *Editor) Close() {
	e.resetAttachment()
	e.mapViewClosed = true
	e.executor = nil
}

// SaveSnapshot captures authority only after gesture capture, async dispatch,
// and acknowledgement callbacks have completed on the UI thread.
func (e *Editor) SaveSnapshot(ctx context.Context) (model.Snapshot, error) {
	if e.collaborationErr != nil {
		return model.Snapshot{}, e.collaborationErr
	}
	if len(e.unresolvedSubmissions) != 0 {
		return model.Snapshot{}, fmt.Errorf("map changes are awaiting completion")
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return model.Snapshot{}, fmt.Errorf("map changes are awaiting acknowledgement")
	}
	return e.CollaborationSnapshot(ctx)
}

// SaveVersion and ChangedSinceSave are UI-thread version checks for tab labels.
// Close and Save still read the executor snapshot, including queued remote edits.
func (e *Editor) SaveVersion() (uint64, model.Revision) {
	return e.attachmentGeneration, e.authoritative.Revision
}

func (e *Editor) ChangedSinceSave(generation uint64, revision model.Revision) bool {
	return generation != e.attachmentGeneration || revision != e.authoritative.Revision ||
		e.selectionMove != nil || e.paste != nil || len(e.pendingChanges) != 0 || len(e.unresolvedSubmissions) != 0 || e.collaborationErr != nil
}

func (e *Editor) reportCollaborationError(message string, err error) {
	log.Error().Err(err).Msg(message)
	// An embedding app may own error presentation. This also lets workspace
	// verification exercise actual failure callbacks without native modal UI.
	if reporter, ok := e.app.(interface{ ReportCollaborationError(string, error) }); ok {
		reporter.ReportCollaborationError(message, err)
		return
	}
	util.ShowErrorDialog(message + ": " + err.Error())
}

// APHELION EDIT ADDITION END
