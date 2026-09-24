// APHELION EDIT ADDITION START - PURE SELECTION MOVE PRESENTATION
package editor

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type selectionMovePhase uint8

const (
	selectionMovePreparing selectionMovePhase = iota
	selectionMoveReady
	selectionMoveResolving
)

type selectionMoveWorkResult struct {
	payload     *editing.MovePayload
	reservation *resources.Reservation
	err         error
}

type localTileCapturer interface {
	CaptureTiles(context.Context) (engine.TileCapture, error)
}

type projectionCapturer interface {
	CaptureProjection(context.Context) (client.ProjectionCapture, error)
}

type selectionMoveSession struct {
	pose              *editing.SelectionMove
	selection         editing.Selection
	visible           func(string) bool
	defaults          editing.MoveDefaults
	generation        uint64
	documentID        model.DocumentID
	revision          model.Revision
	phase             selectionMovePhase
	preparing         bool
	intent            bool
	err               error
	payload           *editing.MovePayload
	reservation       *resources.Reservation
	workerCancel      context.CancelFunc
	workerBusy        bool
	results           chan selectionMoveWorkResult
	resultMu          sync.Mutex
	discarded         bool
	commitWork        int
	selectionOutcome  func(bool)
	repeatAccepted    func()
	pendingAtStart    bool
	sourceProjection  client.ProjectionCapture
	presentation      *render.Presentation
	presentationBuild *presentationBuild
}

func (e *Editor) BeginSelectionMovePreview(selection editing.Selection) (*editing.SelectionMove, error) {
	if e.localWork != nil || e.executor == nil || e.collaborationErr != nil || e.selectionMove != nil || e.selectionMovePreview != nil || e.paste != nil || len(e.pendingChanges) != 0 || e.mapViewClosed || selection.Level() != e.pMap.ActiveLevel() {
		return nil, fmt.Errorf("finish the current edit and select the visible level before moving")
	}
	pose, err := editing.NewSelectionMove(selection)
	if err != nil {
		return nil, err
	}
	reservation, err := e.editWorkBudget().Reserve(editing.EstimateMovePreparationMemory(selection.Len()))
	if err != nil {
		return nil, err
	}
	var sourceCapture engine.TileCapture
	hasSourceCapture := false
	var projectionCapture client.ProjectionCapture
	hasProjectionCapture := false
	if !e.sessionOwned {
		if capturer, ok := e.executor.(localTileCapturer); ok {
			sourceCapture, err = capturer.CaptureTiles(context.Background())
			if err != nil {
				reservation.Release()
				return nil, fmt.Errorf("capture committed selection source: %w", err)
			}
			if sourceCapture.DocumentID() != e.authoritative.DocumentID || sourceCapture.Revision() != e.authoritative.Revision {
				reservation.Release()
				return nil, fmt.Errorf("local authority changed before selection move")
			}
			hasSourceCapture = true
		} else if _, local := e.executor.(localEditExecutor); local {
			reservation.Release()
			return nil, fmt.Errorf("local executor cannot pin selection source tiles")
		}
	} else if capturer, ok := e.executor.(projectionCapturer); ok {
		projectionCapture, err = capturer.CaptureProjection(context.Background())
		if err != nil {
			reservation.Release()
			return nil, fmt.Errorf("capture collaborative selection source: %w", err)
		}
		hasProjectionCapture = true
	} else if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		reservation.Release()
		return nil, fmt.Errorf("collaborative executor cannot pin its speculative selection source")
	}
	defaults, err := selectionMoveDefaults()
	if err != nil {
		reservation.Release()
		return nil, err
	}
	filter := e.app.PathsFilter().Copy()
	session := &selectionMoveSession{
		pose: pose, selection: selection, visible: filter.IsVisiblePath, defaults: defaults,
		generation: e.attachmentGeneration, documentID: e.authoritative.DocumentID, revision: e.authoritative.Revision,
		phase: selectionMovePreparing, preparing: true, reservation: reservation,
		workerBusy: true, results: make(chan selectionMoveWorkResult, 1), selectionOutcome: e.selectionOutcome,
		pendingAtStart:   hasProjectionCapture && projectionCapture.HasPending(),
		sourceProjection: projectionCapture,
	}
	e.selectionMovePreview = session
	// Shared projections replace authoritativeTiles as a whole; local edits use
	// an engine-owned immutable tile capture because they update that map in place.
	base := e.authoritativeTiles
	ctx, cancel := context.WithCancel(context.Background())
	session.workerCancel = cancel
	go func() {
		projectionTiles := map[model.Coord]model.TileState(nil)
		var projectionBytes uint64
		if hasProjectionCapture && projectionCapture.HasPending() {
			projectionBytes = projectionCapture.EstimatedBytes()
			need := saturatingMoveBytes(editing.EstimateMovePreparationMemory(selection.Len()), projectionBytes)
			if err := reservation.Resize(need); err != nil {
				result := selectionMoveWorkResult{reservation: reservation, err: err}
				session.resultMu.Lock()
				defer session.resultMu.Unlock()
				if session.discarded {
					result.reservation.Release()
					return
				}
				session.results <- result
				return
			}
			visibleSnapshot, snapshotErr := projectionCapture.VisibleSnapshot()
			if snapshotErr != nil {
				result := selectionMoveWorkResult{reservation: reservation, err: snapshotErr}
				session.resultMu.Lock()
				defer session.resultMu.Unlock()
				if session.discarded {
					result.reservation.Release()
					return
				}
				session.results <- result
				return
			}
			projectionTiles = makeSnapshotTileIndex(visibleSnapshot)
			session.revision = projectionCapture.BaseRevision()
		}
		var sourceBytes uint64
		var captureErr error
		payload, compileErr := editing.CompileMovePayload(ctx, selection, session.visible, func(coord model.Coord) (model.TileState, bool) {
			if hasSourceCapture {
				size, ok := sourceCapture.EstimatedTileBytes(coord)
				if !ok {
					return model.TileState{}, false
				}
				sourceBytes = saturatingMoveBytes(sourceBytes, size)
				if err := reservation.Resize(editing.EstimateMovePreparationMemoryForSource(selection.Len(), sourceBytes)); err != nil {
					captureErr = err
					cancel()
					return model.TileState{}, false
				}
				return sourceCapture.Tile(coord)
			}
			if projectionTiles != nil {
				state, ok := projectionTiles[coord]
				if ok {
					sourceBytes = saturatingMoveBytes(sourceBytes, editing.EstimateMoveTileMemory(state))
					if err := reservation.Resize(saturatingMoveBytes(projectionBytes, editing.EstimateMovePreparationMemoryForSource(selection.Len(), sourceBytes))); err != nil {
						captureErr = err
						cancel()
						return model.TileState{}, false
					}
				}
				return state, ok
			}
			state, ok := base[coord]
			if ok {
				sourceBytes = saturatingMoveBytes(sourceBytes, editing.EstimateMoveTileMemory(state))
				if err := reservation.Resize(editing.EstimateMovePreparationMemoryForSource(selection.Len(), sourceBytes)); err != nil {
					captureErr = err
					cancel()
					return model.TileState{}, false
				}
			}
			return state, ok
		})
		if captureErr != nil {
			compileErr = captureErr
		}
		projectionTiles = nil
		if compileErr == nil {
			compileErr = reservation.Resize(editing.EstimateMovePayloadMemory(payload))
		}
		result := selectionMoveWorkResult{payload: payload, reservation: reservation, err: compileErr}
		session.resultMu.Lock()
		defer session.resultMu.Unlock()
		if session.discarded {
			result.reservation.Release()
			return
		}
		session.results <- result
	}()
	return pose, nil
}

func saturatingMoveBytes(current, extra uint64) uint64 {
	if current > math.MaxUint64-extra {
		return math.MaxUint64
	}
	return current + extra
}

func (e *Editor) PreviewSelectionMovePreview(move *editing.SelectionMove, shift util.Point) (util.Bounds, error) {
	session := e.selectionMovePreview
	if session == nil || session.pose != move || session.generation != e.attachmentGeneration {
		return util.Bounds{}, fmt.Errorf("selection move belongs to an old map attachment")
	}
	if e.pMap.ActiveLevel() != move.Level() {
		_ = e.FinishSelectionMovePreview(move, true)
		return move.Bounds(), fmt.Errorf("selection move cancelled after switching levels")
	}
	area, changed, err := move.Update(shift, e.dmm.MaxX, e.dmm.MaxY, e.pMap.ActiveLevel())
	if err != nil {
		return move.Bounds(), err
	}
	if session.err != nil {
		return move.Bounds(), session.err
	}
	if changed && session.presentation != nil {
		session.presentation.Anchor = util.Point{X: int(area.X1), Y: int(area.Y1), Z: move.Level()}
	}
	return area, nil
}

// FinishSelectionMovePreview retains release intent while the source subset is
// being copied; the owner frame completes it when preparation is ready.
func (e *Editor) FinishSelectionMovePreview(move *editing.SelectionMove, cancel bool) error {
	session := e.selectionMovePreview
	if session == nil || session.pose != move {
		return nil
	}
	if !cancel {
		session.selectionOutcome = e.selectionOutcome
		session.repeatAccepted = e.repeatAccepted
	}
	if cancel && session.phase == selectionMoveResolving {
		return fmt.Errorf("selection move is already being applied")
	}
	if session.generation != e.attachmentGeneration {
		e.discardSelectionMovePreview(session)
		return fmt.Errorf("selection move belongs to an old map attachment")
	}
	if cancel {
		move.Finish()
		selectionApplied(session.selectionOutcome, false)
		e.discardSelectionMovePreview(session)
		return nil
	}
	if e.pMap.ActiveLevel() != move.Level() {
		move.Finish()
		selectionApplied(session.selectionOutcome, false)
		e.discardSelectionMovePreview(session)
		return fmt.Errorf("selection move cancelled after switching levels")
	}
	if session.err != nil {
		move.Finish()
		selectionApplied(session.selectionOutcome, false)
		e.discardSelectionMovePreview(session)
		return session.err
	}
	if session.preparing {
		session.intent = true
		session.selectionOutcome = e.selectionOutcome
		move.Finish()
		return nil
	}
	return e.submitSelectionMovePreview(session)
}

func (e *Editor) CancelSelectionMovePreview() {
	if session := e.selectionMovePreview; session != nil {
		if session.phase == selectionMoveResolving {
			return
		}
		_ = e.FinishSelectionMovePreview(session.pose, true)
	}
}

func (e *Editor) SelectionMovePreviewActive() bool { return e.selectionMovePreview != nil }

func (e *Editor) selectionMovePreviewPreparing() bool {
	return e.selectionMovePreview != nil && e.selectionMovePreview.preparing
}

func (e *Editor) selectionMovePreviewResolving() bool {
	return e.selectionMovePreview != nil && e.selectionMovePreview.phase == selectionMoveResolving
}

func (e *Editor) processSelectionMoveWork() {
	session := e.selectionMovePreview
	if session == nil {
		return
	}
	if session.generation != e.attachmentGeneration || e.mapViewClosed {
		e.discardSelectionMovePreview(session)
		return
	}
	if session.workerBusy {
		select {
		case result := <-session.results:
			session.workerBusy = false
			session.workerCancel = nil
			session.reservation = result.reservation
			session.preparing = false
			if result.err != nil {
				session.err = result.err
				session.phase = selectionMoveReady
				if session.intent {
					session.pose.Finish()
					selectionApplied(session.selectionOutcome, false)
					e.reportCollaborationError("Unable to prepare selection move", result.err)
					e.discardSelectionMovePreview(session)
				}
				return
			}
			session.payload = result.payload
			if session.documentID != e.authoritative.DocumentID {
				session.err = fmt.Errorf("selection source document changed")
			} else if !session.pendingAtStart && session.revision != e.authoritative.Revision {
				session.err = session.payload.ValidateSource(session.visible, e.authorityTile)
			}
			if session.err != nil {
				session.phase = selectionMoveReady
				if session.intent {
					session.pose.Finish()
					selectionApplied(session.selectionOutcome, false)
					e.reportCollaborationError("Unable to move selection", session.err)
					e.discardSelectionMovePreview(session)
				}
				return
			}
			session.phase = selectionMoveReady
			e.prepareSelectionMovePresentation(session)
		default:
		}
	}
	if session.payload == nil || session.err != nil || session.phase == selectionMoveResolving {
		return
	}
	if session.presentationBuild != nil && e.preparePresentationBuild(session.presentationBuild) {
		session.presentation = session.presentationBuild.presentation
		session.presentationBuild = nil
		if r := e.pMap.Canvas().Render(); r != nil {
			r.SetPresentation(session.presentation)
		}
	}
	if session.intent && !session.preparing && session.phase == selectionMoveReady {
		if err := e.submitSelectionMovePreview(session); err != nil {
			session.pose.Finish()
			selectionApplied(session.selectionOutcome, false)
			e.reportCollaborationError("Unable to move selection", err)
			e.discardSelectionMovePreview(session)
		}
	}
}

func (e *Editor) prepareSelectionMovePresentation(session *selectionMoveSession) {
	if session.payload == nil || session.err != nil {
		return
	}
	anchor := session.pose.Bounds()
	presentation := &render.Presentation{Anchor: util.Point{X: int(anchor.X1), Y: int(anchor.Y1), Z: session.pose.Level()}, IconSize: dmmap.WorldIconSize}
	presentation.Suppress = func(u unit.Unit) bool {
		instance := u.Instance()
		return session.visible(instance.Prefab().Path()) && session.payload.Suppresses(instance.Coord(), session.pose.Shift())
	}
	presentation.Visible = func(a render.Appearance) bool {
		if session.pose.Level() != e.pMap.ActiveLevel() || !session.visible(a.Path) {
			return false
		}
		if a.WorldSpace {
			source := util.Point{X: a.Coord.X + 1, Y: a.Coord.Y + 1, Z: session.pose.Level()}
			return session.selection.Contains(source) && !session.selection.Contains(source.Minus(session.pose.Shift()))
		}
		return true
	}
	sourceCount := session.payload.TileCount()
	session.presentationBuild = &presentationBuild{
		presentation: presentation,
		tileCount:    sourceCount * 2,
		instanceCount: func(tile int) int {
			if tile < sourceCount {
				_, prefabs := session.payload.Tile(tile)
				return len(prefabs)
			}
			return len(moveSourceDefaults(session, tile-sourceCount))
		},
		appearance: func(tile, instance int) render.Appearance {
			if tile < sourceCount {
				coord, prefabs := session.payload.Tile(tile)
				appearanceInstance := e.movePresentationInstance(coord, prefabs[instance])
				return render.PrepareAppearance(coord, appearanceInstance, dmmap.WorldIconSize)
			}
			local, _ := session.payload.Tile(tile - sourceCount)
			bounds := session.selection.Bounds()
			coord := util.Point{X: int(bounds.X1) + local.X - 1, Y: int(bounds.Y1) + local.Y - 1, Z: session.pose.Level()}
			prefab := moveSourceDefaults(session, tile-sourceCount)[instance]
			prefab.StableID = ""
			appearance := render.PrepareAppearance(coord, e.movePresentationInstance(coord, prefab), dmmap.WorldIconSize)
			appearance.WorldSpace = true
			return appearance
		},
	}
}

// Source defaults replace only missing families. Hidden source contents remain
// in committed geometry and must not acquire an extra visible default.
func moveSourceDefaults(session *selectionMoveSession, tile int) []model.PrefabState {
	local, _ := session.payload.Tile(tile)
	bounds := session.selection.Bounds()
	coord := model.Coord{X: int(bounds.X1) + local.X - 1, Y: int(bounds.Y1) + local.Y - 1, Z: session.pose.Level()}
	original, _ := session.payload.OriginalSource(coord)
	area, turf := false, false
	for _, prefab := range original.Prefabs {
		if !session.visible(prefab.Path) {
			area = area || strings.HasPrefix(prefab.Path, "/area")
			turf = turf || strings.HasPrefix(prefab.Path, "/turf")
		}
	}
	result := make([]model.PrefabState, 0, 2)
	if !area {
		result = append(result, session.defaults.Area)
	}
	if !turf {
		result = append(result, session.defaults.Turf)
	}
	return result
}

func (e *Editor) movePresentationInstance(coord util.Point, saved model.PrefabState) *dmminstance.Instance {
	variables := &dmvars.MutableVariables{}
	for _, name := range sortedVariableNames(saved.Vars) {
		variables.Put(name, saved.Vars[name])
	}
	prefab := dmmprefab.New(dmmprefab.IdNone, saved.Path, variables.ToImmutable())
	if environment := e.app.LoadedEnvironment(); environment != nil {
		if object := environment.Objects[saved.Path]; object != nil {
			prefab.Vars().LinkParent(object.Vars)
		}
	}
	instance := dmminstance.New(coord, prefab)
	instance.SetStableID(string(saved.StableID))
	return instance
}

func (e *Editor) revalidateSelectionMovePreview() {
	session := e.selectionMovePreview
	if session == nil || session.preparing || session.payload == nil || session.err != nil || session.phase == selectionMoveResolving {
		return
	}
	// A pending-at-start preview is based on the speculative projection. Its
	// current coherent source is checked against the executor projection on
	// release, where both source and destination are available atomically.
	if session.pendingAtStart {
		return
	}
	if err := session.payload.ValidateSource(session.visible, e.authorityTile); err != nil {
		session.err = err
		session.presentation = nil
		session.presentationBuild = nil
		if r := e.pMap.Canvas().Render(); r != nil {
			r.SetPresentation(nil)
		}
		if session.intent {
			session.pose.Finish()
			selectionApplied(session.selectionOutcome, false)
			e.reportCollaborationError("Unable to move selection", err)
			e.discardSelectionMovePreview(session)
		}
	}
}

func (e *Editor) authorityTile(coord model.Coord) (model.TileState, bool) {
	state, ok := e.authoritativeTiles[coord]
	return state, ok
}

func (e *Editor) submitSelectionMovePreview(session *selectionMoveSession) error {
	if session == nil || session != e.selectionMovePreview || session.payload == nil || session.phase != selectionMoveReady {
		return fmt.Errorf("selection move is not ready")
	}
	if session.err != nil {
		return session.err
	}
	if err := session.payload.ValidateTarget(session.pose.Shift(), e.dmm.MaxX, e.dmm.MaxY, e.pMap.ActiveLevel()); err != nil {
		return err
	}
	if session.pose.Shift() == (util.Point{}) {
		session.pose.Finish()
		selectionApplied(session.selectionOutcome, false)
		e.discardSelectionMovePreview(session)
		return nil
	}
	session.phase = selectionMoveResolving
	session.pose.Finish()
	if local, ok := e.executor.(localEditExecutor); ok && !e.sessionOwned {
		base, payload, visible, shift := e.authoritativeTiles, session.payload, session.visible, session.pose.Shift()
		generation := e.attachmentGeneration
		e.retainSelectionMoveCommit(session)
		err := e.startLocalWork(local, true, editing.EstimateMovePayloadMemory(payload), func(ctx context.Context, _ *resources.Reservation) ([]model.TileChange, error) {
			return payload.BuildMoveChangesWithDefaults(ctx, shift, visible, func(coord model.Coord) (model.TileState, bool) {
				state, ok := base[coord]
				return state, ok
			}, session.defaults)
		}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
			defer e.finishSelectionMoveCommit(session)
			if generation != e.attachmentGeneration || e.selectionMovePreview != session {
				return
			}
			if err != nil {
				selectionApplied(session.selectionOutcome, false)
				e.reportCollaborationError("Unable to move selection", err)
				e.discardSelectionMovePreview(session)
				return
			}
			if len(accepted.Changes) != 0 {
				if session.repeatAccepted != nil {
					session.repeatAccepted()
				}
				selectionApplied(session.selectionOutcome, true)
				e.pushLocalCommandOwned(local, "Move Grabbed Area", accepted.Changes, backward, session.selectionOutcome)
			} else {
				selectionApplied(session.selectionOutcome, false)
			}
			e.discardSelectionMovePreview(session)
		})
		if err != nil {
			e.finishSelectionMoveCommit(session)
			session.phase = selectionMoveReady
			session.pose.Finish()
			selectionApplied(session.selectionOutcome, false)
			e.discardSelectionMovePreview(session)
			return err
		}
		return nil
	}
	e.retainSelectionMoveCommit(session)
	execution, generation, shift, level := e.executor, e.attachmentGeneration, session.pose.Shift(), session.pose.Level()
	visible, payload, actorID := session.visible, session.payload, e.actorID
	go func() {
		var operation model.Operation
		var changes []model.TileChange
		var err error
		if capturer, ok := execution.(projectionCapturer); ok {
			var projection client.ProjectionCapture
			projection, err = capturer.CaptureProjection(context.Background())
			if err == nil {
				need := saturatingMoveBytes(editing.EstimateMovePayloadMemory(payload), projection.EstimatedBytes())
				err = session.reservation.Resize(need)
			}
			var visibleSnapshot model.Snapshot
			if err == nil {
				visibleSnapshot, err = projection.VisibleSnapshot()
			}
			if err == nil {
				lookup := snapshotTile(visibleSnapshot)
				if session.pendingAtStart && projection.BaseRevision() == session.revision && payload.ValidateSource(visible, lookup) != nil {
					// A rejected dependency invalidated the speculative source. Retain
					// the exact original draft, including destinations changed by that
					// dependency, so its before-states remain reconstructable in order.
					need := saturatingMoveBytes(editing.EstimateMovePayloadMemory(payload), projection.EstimatedBytes())
					err = session.reservation.Resize(saturatingMoveBytes(need, session.sourceProjection.EstimatedBytes()))
					if err == nil {
						var original model.Snapshot
						original, err = session.sourceProjection.VisibleSnapshot()
						if err == nil {
							lookup = snapshotTile(original)
						}
					}
				}
				if err == nil {
					changes, err = payload.BuildMoveChangesWithDefaults(context.Background(), shift, visible, lookup, session.defaults)
				}
			}
			if err == nil {
				operation, err = selectionMoveOperationFromProjection(projection, changes, actorID)
			}
		} else {
			var snapshot model.Snapshot
			snapshot, err = execution.Snapshot(context.Background())
			if err == nil {
				changes, err = payload.BuildMoveChangesWithDefaults(context.Background(), shift, visible, snapshotTile(snapshot), session.defaults)
			}
			if err == nil {
				operation, err = selectionMoveOperation(snapshot, changes, actorID)
			}
		}
		if err == nil && len(changes) == 0 {
			e.app.RunLater(func() {
				if generation == e.attachmentGeneration && e.selectionMovePreview == session {
					selectionApplied(session.selectionOutcome, false)
					e.discardSelectionMovePreview(session)
				}
				e.finishSelectionMoveCommit(session)
			})
			return
		}
		if err == nil {
			// Whole-projection scratch is no longer needed once the sparse
			// operation is prepared. Keep admission for its retained copies only.
			need := editing.EstimateMovePayloadMemory(payload)
			for _, change := range changes {
				bytes := saturatingMoveBytes(editing.EstimateMoveTileMemory(change.Before), editing.EstimateMoveTileMemory(change.After))
				for range 3 {
					need = saturatingMoveBytes(need, bytes)
				}
			}
			err = session.reservation.Resize(need)
		}
		if err == nil {
			coords := make([]model.Coord, len(changes))
			for i, change := range changes {
				coords[i] = change.Coord
			}
			acceptedChanges := model.CloneOperation(model.Operation{Changes: changes}).Changes
			e.app.RunLater(func() {
				if generation != e.attachmentGeneration || e.selectionMovePreview != session {
					e.finishSelectionMoveCommit(session)
					return
				}
				e.dispatchSelectionMoveOperation(session, execution, operation, acceptedChanges, coords, level, generation)
			})
			return
		}
		e.app.RunLater(func() {
			defer e.finishSelectionMoveCommit(session)
			if generation == e.attachmentGeneration && e.selectionMovePreview == session {
				selectionApplied(session.selectionOutcome, false)
				e.reportCollaborationError("Unable to prepare selection move", err)
				e.discardSelectionMovePreview(session)
			}
		})
	}()
	return nil
}

func selectionMoveOperation(snapshot model.Snapshot, changes []model.TileChange, actorID model.ActorID) (model.Operation, error) {
	operationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, fmt.Errorf("create selection move operation id: %w", err)
	}
	baseHash, err := snapshot.Hash()
	if err != nil {
		return model.Operation{}, fmt.Errorf("hash selection move base: %w", err)
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      snapshot.DocumentID,
		ActorID:         actorID,
		OperationID:     operationID,
		BaseRevision:    snapshot.Revision,
		EnvironmentHash: snapshot.EnvironmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}, nil
}

func selectionMoveOperationFromProjection(projection client.ProjectionCapture, changes []model.TileChange, actorID model.ActorID) (model.Operation, error) {
	documentID, revision, environmentHash, baseHash, err := projection.OperationBase()
	if err != nil {
		return model.Operation{}, fmt.Errorf("read selection move base: %w", err)
	}
	operationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, fmt.Errorf("create selection move operation id: %w", err)
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      documentID,
		ActorID:         actorID,
		OperationID:     operationID,
		BaseRevision:    revision,
		EnvironmentHash: environmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}, nil
}

func snapshotTile(snapshot model.Snapshot) func(model.Coord) (model.TileState, bool) {
	index := makeSnapshotTileIndex(snapshot)
	return func(coord model.Coord) (model.TileState, bool) {
		state, ok := index[coord]
		return state, ok
	}
}

func makeSnapshotTileIndex(snapshot model.Snapshot) map[model.Coord]model.TileState {
	index := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	for _, tile := range snapshot.Tiles {
		index[tile.Coord] = tile.State
	}
	return index
}

func (e *Editor) dispatchSelectionMoveOperation(session *selectionMoveSession, execution executor.Executor, operation model.Operation, acceptedChanges []model.TileChange, coords []model.Coord, level int, generation uint64) {
	session.sourceProjection = client.ProjectionCapture{}
	e.unresolvedSubmissions[operation.OperationID] = struct{}{}
	complete := func(accepted model.AcceptedOperation, err error) {
		e.app.RunLater(func() {
			defer e.finishSelectionMoveCommit(session)
			if generation != e.attachmentGeneration || e.executor != execution {
				return
			}
			delete(e.unresolvedSubmissions, operation.OperationID)
			if err != nil {
				selectionApplied(session.selectionOutcome, false)
				if e.selectionMovePreview == nil {
					e.syncFromExecutor(execution, false, e.pMap.ActiveLevel(), coords)
					e.reportCollaborationError("Unable to apply map change", err)
				} else {
					e.rejectSpeculation(execution, err)
				}
				e.discardSelectionMovePreview(session)
				return
			}
			e.syncFromExecutor(execution, e.selectionMovePreview == nil, level, coords)
			if session.repeatAccepted != nil {
				session.repeatAccepted()
			}
			selectionApplied(session.selectionOutcome, true)
			e.pushAcceptedCommand(execution, "Move Grabbed Area", accepted, acceptedChanges, level, coords, session.selectionOutcome)
			e.discardSelectionMovePreview(session)
		})
	}
	if asynchronous, ok := execution.(executor.AsyncExecutor); ok {
		if err := asynchronous.ExecuteAsync(context.Background(), operation, complete); err != nil {
			delete(e.unresolvedSubmissions, operation.OperationID)
			selectionApplied(session.selectionOutcome, false)
			e.rejectSpeculation(execution, err)
			e.discardSelectionMovePreview(session)
			e.finishSelectionMoveCommit(session)
			return
		}
		e.discardSelectionMovePreview(session)
		return
	}
	go func() {
		accepted, err := execution.Execute(context.Background(), operation)
		complete(accepted, err)
	}()
	e.discardSelectionMovePreview(session)
}

func (e *Editor) retainSelectionMoveCommit(session *selectionMoveSession) {
	session.resultMu.Lock()
	session.commitWork++
	session.resultMu.Unlock()
}

func (e *Editor) finishSelectionMoveCommit(session *selectionMoveSession) {
	session.resultMu.Lock()
	defer session.resultMu.Unlock()
	session.commitWork--
	if session.discarded && session.commitWork == 0 && !session.workerBusy {
		session.reservation.Release()
	}
}

func (e *Editor) discardSelectionMovePreview(session *selectionMoveSession) {
	if session == nil {
		return
	}
	if session.workerCancel != nil {
		session.workerCancel()
	}
	session.resultMu.Lock()
	session.discarded = true
	select {
	case result := <-session.results:
		result.reservation.Release()
		session.workerBusy = false
	default:
	}
	if !session.workerBusy && session.commitWork == 0 {
		session.reservation.Release()
	}
	session.resultMu.Unlock()
	if e.selectionMovePreview == session {
		if r := e.pMap.Canvas().Render(); r != nil {
			r.SetPresentation(nil)
		}
		e.selectionMovePreview = nil
	}
}

func selectionMoveDefaults() (editing.MoveDefaults, error) {
	area, err := moveDefaultPrefab(dmmap.BaseArea)
	if err != nil {
		return editing.MoveDefaults{}, fmt.Errorf("read base area for move: %w", err)
	}
	turf, err := moveDefaultPrefab(dmmap.BaseTurf)
	if err != nil {
		return editing.MoveDefaults{}, fmt.Errorf("read base turf for move: %w", err)
	}
	return editing.MoveDefaults{Area: area, Turf: turf}, nil
}

func moveDefaultPrefab(prefab *dmmprefab.Prefab) (model.PrefabState, error) {
	if prefab == nil || prefab.Vars() == nil || prefab.Path() == "" {
		return model.PrefabState{}, fmt.Errorf("base prefab is unavailable")
	}
	state := model.PrefabState{Path: prefab.Path(), Vars: make(map[string]string, prefab.Vars().Len())}
	names := prefab.Vars().Iterate()
	sort.Strings(names)
	for _, name := range names {
		value, exists := prefab.Vars().Value(name)
		if !exists {
			return model.PrefabState{}, fmt.Errorf("base prefab variable %q has no value", name)
		}
		state.Vars[name] = value
	}
	return state, nil
}

// APHELION EDIT ADDITION END
