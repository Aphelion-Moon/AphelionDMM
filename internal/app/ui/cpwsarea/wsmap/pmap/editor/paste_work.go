// APHELION EDIT ADDITION START - BOUNDED PASTE PREVIEW
package editor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const (
	pasteUIChunkTiles = 384
	pasteUIChunkTime  = 2 * time.Millisecond
)

type pastePhase uint8

const (
	pastePreparing pastePhase = iota
	pastePreflight
	pasteApplying
	pasteReady
	pasteRollbackWaiting
	pasteRollbackApplying
	pasteResolving
)

type pasteWorkKind uint8

const (
	pastePrepareWork pasteWorkKind = iota
	pasteRollbackWork
)

type pastePatch struct {
	coord model.Coord
	state model.TileState
}

type pasteSourceFactory func(context.Context) ([]dmmap.Tile, func(string) bool, *resources.Reservation, error)

type pasteWorkResult struct {
	kind               pasteWorkKind
	generation         uint64
	request            uint64
	operation          model.Operation
	source             []dmmap.Tile
	visible            func(string) bool
	sourceReady        bool
	sourcePrepared     bool
	sourceCopyAdmitted bool
	snapshot           model.Snapshot
	stateIndex         map[model.Coord]model.TileState
	positions          map[model.Coord]int
	patches            []pastePatch
	coords             []model.Coord
	bounds             util.Bounds
	reservation        *resources.Reservation
	sourceReservation  *resources.Reservation
	err                error
}

type pasteSession struct {
	generation         uint64
	level              int
	source             []dmmap.Tile
	visible            func(string) bool
	sourceFactory      pasteSourceFactory
	releaseSource      func()
	sourceReservation  *resources.Reservation
	sourcePrepared     bool
	sourceCopyAdmitted bool
	identities         map[model.StableID]model.StableID
	target             util.Point
	request            uint64
	transforms         []editing.PlacementTransform
	phase              pastePhase
	err                error

	workerCancel context.CancelFunc
	workerBusy   bool
	commitWork   int // Guarded by resultMu; retains proposal buffers through durable completion.
	results      chan pasteWorkResult
	resultMu     sync.Mutex
	discarded    bool
	progress     atomic.Int64
	total        atomic.Int64

	operation               model.Operation // Last complete preview; also the exact commit body.
	hasPreview              bool
	previewCoords           []model.Coord
	rollbackCoords          []model.Coord
	displayedRequest        uint64
	bounds                  util.Bounds
	baseSnapshot            model.Snapshot
	stateIndex              map[model.Coord]model.TileState
	positions               map[model.Coord]int
	candidate               pasteWorkResult
	reservation             *resources.Reservation
	patches                 []pastePatch
	preflightIndex          int
	patchIndex              int
	cancelRequested         bool
	commitError             error
	selectionOutcome        func(bool)
	releaseOwnershipPending bool
	ownershipReleased       bool
}

func (e *Editor) beginPasteProposal(source []dmmap.Tile, visible func(string) bool, target util.Point) error {
	if e.executor == nil || visible == nil || len(source) == 0 {
		return fmt.Errorf("paste requires an available map and a nonempty selection")
	}
	return e.beginPasteSession(source, visible, nil, nil, len(source), target)
}

func (e *Editor) beginPasteProposalFromFactory(tileCount int, factory pasteSourceFactory, release func(), target util.Point) error {
	if e.executor == nil || factory == nil || tileCount < 1 {
		return fmt.Errorf("paste requires an available map and a nonempty selection")
	}
	return e.beginPasteSession(nil, nil, factory, release, tileCount, target)
}

func (e *Editor) beginPasteSession(source []dmmap.Tile, visible func(string) bool, factory pasteSourceFactory, release func(), tileCount int, target util.Point) error {
	if target.Z != e.pMap.ActiveLevel() {
		return fmt.Errorf("paste target must be on the selected level")
	}
	p := &pasteSession{
		generation:    e.attachmentGeneration,
		level:         e.pMap.ActiveLevel(),
		source:        source,
		visible:       visible,
		sourceFactory: factory,
		releaseSource: release,
		target:        target,
		request:       1,
		phase:         pastePreparing,
		results:       make(chan pasteWorkResult, 1),
		identities:    make(map[model.StableID]model.StableID),
	}
	p.total.Store(int64(tileCount))
	e.paste = p
	e.mapViewGeneration++
	e.startPasteWorker(p, pastePrepareWork)
	return nil
}

// UpdatePastePlacement is called from ToolGrab's input path. It changes only
// request metadata; proposal work and DMM mutation happen outside this call.
func (e *Editor) UpdatePastePlacement(target util.Point) (util.Bounds, bool, error) {
	p := e.paste
	if p == nil || p.generation != e.attachmentGeneration {
		return util.Bounds{}, false, fmt.Errorf("paste belongs to an old map attachment")
	}
	if e.collaborationErr != nil {
		p.err = e.collaborationErr
		return p.bounds, false, e.collaborationErr
	}
	if p.phase == pasteResolving || p.phase == pasteRollbackWaiting || p.phase == pasteRollbackApplying {
		return util.Bounds{}, false, fmt.Errorf("paste is resolving")
	}
	if target.Z != p.level || target.Z != e.pMap.ActiveLevel() {
		p.err = fmt.Errorf("paste target must be on the selected level")
		return util.Bounds{}, false, p.err
	}
	// An invalid intermediate cursor position does not invalidate a preview at
	// the last valid target. Clear its transient error if the cursor returns.
	if p.target == target && p.phase == pasteReady && p.displayedRequest == p.request {
		p.err = nil
	}
	if target != p.target {
		p.target = target
		p.request++
		p.err = fmt.Errorf("preparing paste preview")
		if !p.workerBusy && p.phase != pasteApplying {
			if p.phase == pastePreflight {
				e.discardPreparedPasteCandidate(p)
			}
			p.phase = pastePreparing
			e.startPasteWorker(p, pastePrepareWork)
		}
	}
	if p.phase == pasteReady && p.displayedRequest == p.request && p.err == nil {
		return p.bounds, true, nil
	}
	if p.err != nil {
		return p.bounds, false, p.err
	}
	if p.phase == pasteApplying {
		return p.bounds, false, fmt.Errorf("applying paste preview")
	}
	return p.bounds, false, fmt.Errorf("preparing paste preview")
}

func (e *Editor) TransformPreparedPastePlacement(transform editing.PlacementTransform) error {
	p := e.paste
	if p == nil || p.phase == pasteResolving || p.cancelRequested || p.generation != e.attachmentGeneration {
		return fmt.Errorf("no active paste placement")
	}
	if e.collaborationErr != nil {
		p.err = e.collaborationErr
		return e.collaborationErr
	}
	if transform < editing.PlacementRotateRight || transform > editing.PlacementMirrorVertical {
		return fmt.Errorf("unknown paste transform")
	}
	p.transforms = append(p.transforms, transform)
	p.request++
	p.err = fmt.Errorf("preparing transformed paste preview")
	if !p.workerBusy && p.phase != pasteApplying {
		if p.phase == pastePreflight {
			e.discardPreparedPasteCandidate(p)
		}
		p.phase = pastePreparing
		e.startPasteWorker(p, pastePrepareWork)
	}
	return nil
}

func (e *Editor) ConfirmPastePlacement() bool {
	p := e.paste
	if p == nil || p.phase != pasteReady || p.displayedRequest != p.request || p.err != nil || p.generation != e.attachmentGeneration || e.pMap.ActiveLevel() != p.level {
		return false
	}
	if len(p.operation.Changes) == 0 {
		p.cancelRequested = true
		p.selectionOutcome = e.selectionOutcome
		e.beginPasteRollback(p)
		return true
	}
	execution := e.executor
	operation := p.operation // Immutable: preview and the submitted body are the same proposal.
	coords := p.previewCoords
	selectionOutcome := e.selectionOutcome
	activeLevel := p.level
	p.selectionOutcome = selectionOutcome
	p.phase = pasteResolving
	p.err = fmt.Errorf("paste commit is resolving")
	e.retainPasteCommitWork(p)
	generation := e.attachmentGeneration
	e.unresolvedSubmissions[operation.OperationID] = struct{}{}
	complete := func(accepted model.AcceptedOperation, executeErr error) {
		if executeErr != nil {
			e.app.RunLater(func() {
				e.finishPasteCommitWork(p)
				if generation != e.attachmentGeneration || e.paste != p {
					e.releasePasteOwnership(p)
					return
				}
				if _, pending := e.unresolvedSubmissions[operation.OperationID]; !pending {
					return
				}
				delete(e.unresolvedSubmissions, operation.OperationID)
				p.commitError = executeErr
				p.cancelRequested = true
				p.phase = pasteReady
				e.beginPasteRollback(p)
			})
			return
		}
		// Acceptance metadata can be the size of a complete Z level. Build it
		// away from the UI callback; that callback only adopts the prepared view.
		go func() {
			snapshot, stateIndex, redoChanges := acceptedPasteMetadata(p, accepted.Revision)
			e.app.RunLater(func() {
				e.finishPasteCommitWork(p)
				if generation != e.attachmentGeneration || e.paste != p {
					e.releasePasteOwnership(p)
					return
				}
				if _, pending := e.unresolvedSubmissions[operation.OperationID]; !pending {
					return
				}
				delete(e.unresolvedSubmissions, operation.OperationID)
				e.acceptPaste(p, execution, accepted, snapshot, stateIndex, redoChanges, activeLevel, coords, selectionOutcome)
			})
		}()
	}
	if asynchronous, ok := execution.(executor.AsyncExecutor); ok {
		if err := asynchronous.ExecuteAsync(context.Background(), operation, complete); err != nil {
			e.finishPasteCommitWork(p)
			delete(e.unresolvedSubmissions, operation.OperationID)
			p.commitError = err
			p.cancelRequested = true
			p.phase = pasteReady
			e.beginPasteRollback(p)
		}
		return true
	}
	// The local executor is synchronous by contract. Run it off the UI thread for
	// a complete-level paste; RunLater keeps all display and history mutations on
	// the UI thread while the operation remains unresolved.
	go func() {
		accepted, err := execution.Execute(context.Background(), operation)
		complete(accepted, err)
	}()
	return true
}

func (e *Editor) CancelPastePlacement() {
	p := e.paste
	if p == nil || p.generation != e.attachmentGeneration {
		return
	}
	if p.phase == pasteResolving || p.phase == pasteRollbackWaiting || p.phase == pasteRollbackApplying {
		return // Once execution begins the durable outcome must be resolved.
	}
	if !p.hasPreview && p.patchIndex == 0 {
		if p.workerCancel != nil {
			p.workerCancel()
		}
		e.finishPasteWithoutRestore(p)
		return
	}
	p.cancelRequested = true
	if p.workerCancel != nil {
		p.workerCancel()
	}
	if !p.workerBusy && p.phase != pasteApplying {
		e.beginPasteRollback(p)
	}
}

func (e *Editor) PastePlacementClosed() bool {
	return e.paste == nil || e.paste.generation != e.attachmentGeneration
}

func (e *Editor) PastePlacementProgress() string {
	p := e.paste
	if p == nil {
		return ""
	}
	switch p.phase {
	case pastePreparing:
		return fmt.Sprintf("Preparing paste: %d / %d tiles", p.progress.Load(), p.total.Load())
	case pastePreflight:
		return fmt.Sprintf("Checking paste destinations: %d / %d tiles", p.preflightIndex, len(p.patches))
	case pasteApplying:
		return fmt.Sprintf("Applying preview: %d / %d tiles", p.patchIndex, len(p.patches))
	case pasteRollbackWaiting, pasteRollbackApplying:
		return fmt.Sprintf("Restoring map: %d / %d tiles", p.patchIndex, len(p.patches))
	case pasteResolving:
		return "Paste commit is resolving. Keep this map open until the result appears."
	default:
		return ""
	}
}

// ProcessPasteWork advances proposal results and display patches from the pane
// frame. It is the only path that applies paste tile states or invalidates GL.
func (e *Editor) ProcessPasteWork() {
	p := e.paste
	if p == nil {
		return
	}
	if p.generation != e.attachmentGeneration {
		e.discardPasteWithoutRestore()
		return
	}
	if p.phase != pasteResolving && p.phase != pasteRollbackWaiting && p.phase != pasteRollbackApplying && e.pMap.ActiveLevel() != p.level {
		e.CancelPastePlacement()
	}
	if p.workerBusy {
		select {
		case result := <-p.results:
			p.workerBusy = false
			if p.workerCancel != nil {
				p.workerCancel()
				p.workerCancel = nil
			}
			e.acceptPasteWorkResult(p, result)
		default:
		}
	}
	if p.cancelRequested && (p.phase == pastePreflight || p.phase == pasteApplying) {
		e.beginPasteRollback(p)
	}
	switch p.phase {
	case pastePreflight:
		e.preflightPasteChunk(p)
	case pasteApplying, pasteRollbackApplying:
		e.applyPasteChunk(p)
	}
}

func (e *Editor) acceptPasteWorkResult(p *pasteSession, result pasteWorkResult) {
	if e.paste != p || result.generation != e.attachmentGeneration {
		releasePasteResult(&result)
		return
	}
	if result.sourceReady {
		if p.cancelRequested {
			result.sourceReservation.Release()
			result.sourceReservation = nil
		} else {
			p.source = result.source
			p.visible = result.visible
			p.sourcePrepared = result.sourcePrepared
			p.sourceCopyAdmitted = result.sourceCopyAdmitted
			p.sourceReservation = result.sourceReservation
			result.sourceReservation = nil
			p.sourceFactory = nil
			if p.releaseSource != nil {
				p.releaseSource()
				p.releaseSource = nil
			}
			p.total.Store(int64(len(p.source)))
		}
	}
	if result.kind == pasteRollbackWork {
		if result.err != nil {
			releasePasteResult(&result)
			e.finishPasteRollbackFailure(p, result.err)
			return
		}
		p.reservation = result.reservation
		p.candidate = result
		p.patches, p.patchIndex = result.patches, 0
		p.phase = pasteRollbackApplying
		if len(p.patches) == 0 {
			e.finishPasteRollback(p)
		}
		return
	}
	if p.cancelRequested {
		releasePasteResult(&result)
		if len(p.rollbackCoords) == 0 && !p.hasPreview && p.patchIndex == 0 {
			e.finishPasteRollback(p)
		} else if !p.workerBusy {
			e.startPasteRollbackWorker(p)
		}
		return
	}
	if result.request != p.request {
		releasePasteResult(&result)
		p.phase = pastePreparing
		p.err = fmt.Errorf("preparing paste preview")
		e.startPasteWorker(p, pastePrepareWork)
		return
	}
	if result.err != nil {
		releasePasteResult(&result)
		p.phase = pasteReady
		p.err = result.err
		return
	}
	if p.reservation != nil {
		p.reservation.Release()
	}
	p.reservation = result.reservation
	p.candidate = result
	p.patches, p.patchIndex = result.patches, 0
	p.preflightIndex = 0
	p.phase = pastePreflight
	p.err = nil
	if p.cancelRequested {
		e.beginPasteRollback(p)
		return
	}
	if len(p.patches) == 0 {
		p.phase = pasteApplying
		e.finishPastePreview(p)
	}
}

func (e *Editor) preflightPasteChunk(p *pasteSession) {
	started := time.Now()
	count := 0
	for p.preflightIndex < len(p.patches) && count < pasteUIChunkTiles {
		patch := p.patches[p.preflightIndex]
		point := util.Point{X: patch.coord.X, Y: patch.coord.Y, Z: patch.coord.Z}
		if !e.dmm.HasTile(point) {
			e.failPastePreflight(p, fmt.Errorf("paste preview coordinate (%d,%d,%d) is outside the map", point.X, point.Y, point.Z))
			return
		}
		// CaptureTile assigns missing durable IDs as part of validation, so
		// validate a detached copy to keep a failed preflight side-effect free.
		detached := e.dmm.GetTile(point).Copy()
		if _, err := mapadapter.CaptureTile(&detached); err != nil {
			e.failPastePreflight(p, err)
			return
		}
		p.preflightIndex++
		count++
		if time.Since(started) >= pasteUIChunkTime {
			break
		}
	}
	if p.preflightIndex == len(p.patches) {
		p.phase = pasteApplying
		p.patchIndex = 0
		p.err = nil
	}
}

func (e *Editor) failPastePreflight(p *pasteSession, err error) {
	if e.paste != p {
		return
	}
	e.collaborationErr = err
	p.err = err
	p.phase = pasteReady
	e.discardPreparedPasteCandidate(p)
}

func (e *Editor) discardPreparedPasteCandidate(p *pasteSession) {
	if p == nil {
		return
	}
	e.releasePasteReservation(p)
	p.candidate = pasteWorkResult{}
	p.patches = nil
	p.patchIndex = 0
	p.preflightIndex = 0
}

func (e *Editor) applyPasteChunk(p *pasteSession) {
	started := time.Now()
	coords := make([]util.Point, 0, pasteUIChunkTiles)
	count := 0
	for p.patchIndex < len(p.patches) && count < pasteUIChunkTiles {
		patch := p.patches[p.patchIndex]
		if err := e.applyPasteTileState(patch.coord, patch.state); err != nil {
			if p.phase == pasteRollbackApplying {
				e.finishPasteRollbackFailure(p, err)
			} else {
				p.commitError = err
				p.cancelRequested = true
				e.beginPasteRollback(p)
			}
			return
		}
		coords = append(coords, util.Point{X: patch.coord.X, Y: patch.coord.Y, Z: patch.coord.Z})
		p.patchIndex++
		count++
		if time.Since(started) >= pasteUIChunkTime {
			break
		}
	}
	if len(coords) != 0 {
		e.mapViewGeneration++
		e.pMap.Canvas().Render().UpdateBucketV(e.dmm, p.level, coords)
	}
	if p.patchIndex != len(p.patches) {
		return
	}
	if p.phase == pasteRollbackApplying {
		e.finishPasteRollback(p)
	} else {
		e.finishPastePreview(p)
	}
}

func (e *Editor) finishPastePreview(p *pasteSession) {
	if e.paste != p {
		return
	}
	p.operation = p.candidate.operation
	p.previewCoords = p.candidate.coords
	p.displayedRequest = p.candidate.request
	p.hasPreview = true
	p.err = nil
	p.phase = pasteReady
	p.bounds = p.candidate.bounds
	p.baseSnapshot = p.candidate.snapshot
	p.stateIndex = p.candidate.stateIndex
	p.positions = p.candidate.positions
	e.adoptAuthoritative(p.candidate.snapshot, p.candidate.stateIndex)
	p.candidate = pasteWorkResult{}
	p.patches = nil
	p.patchIndex = 0
	if p.cancelRequested {
		e.beginPasteRollback(p)
		return
	}
	if p.displayedRequest != p.request {
		p.phase = pastePreparing
		p.err = fmt.Errorf("preparing paste preview")
		e.startPasteWorker(p, pastePrepareWork)
	}
}

func (e *Editor) beginPasteRollback(p *pasteSession) {
	if e.paste != p || p.phase == pasteResolving {
		return
	}
	p.phase = pasteRollbackWaiting
	p.err = fmt.Errorf("restoring map")
	p.rollbackCoords = append(p.rollbackCoords[:0], p.previewCoords...)
	limit := min(p.patchIndex, len(p.patches))
	for _, patch := range p.patches[:limit] {
		p.rollbackCoords = append(p.rollbackCoords, patch.coord)
	}
	p.rollbackCoords = uniqueCoords(p.rollbackCoords)
	if p.workerCancel != nil {
		p.workerCancel()
	}
	if !p.workerBusy {
		if len(p.rollbackCoords) == 0 {
			e.finishPasteRollback(p)
			return
		}
		e.startPasteRollbackWorker(p)
	}
}

func (e *Editor) startPasteRollbackWorker(p *pasteSession) {
	if p.workerBusy || e.executor == nil {
		return
	}
	p.phase = pasteRollbackWaiting
	e.startPasteWorker(p, pasteRollbackWork)
}

func (e *Editor) finishPasteRollback(p *pasteSession) {
	if e.paste != p {
		return
	}
	if len(p.rollbackCoords) == 0 && !p.hasPreview && p.patchIndex == 0 {
		e.finishPasteWithoutRestore(p)
		return
	}
	snapshot, stateIndex := p.candidate.snapshot, p.candidate.stateIndex
	if snapshot.DocumentID == "" && p.hasPreview {
		snapshot, stateIndex = p.baseSnapshot, p.stateIndex
	}
	if snapshot.DocumentID != "" {
		e.adoptAuthoritative(snapshot, stateIndex)
	}
	if snapshot.DocumentID == "" && len(p.rollbackCoords) == 0 {
		selectionApplied(p.selectionOutcome, false)
		commitErr := p.commitError
		e.releasePasteOwnership(p)
		e.paste = nil
		if commitErr != nil {
			e.reportCollaborationError("Unable to apply map change", commitErr)
		}
		return
	}
	e.syncPasteSnapshot(p.rollbackCoords)
	e.updateAreasZones()
	e.dmm.PersistPrefabs()
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	selectionApplied(p.selectionOutcome, false)
	commitErr := p.commitError
	e.releasePasteOwnership(p)
	e.paste = nil
	if commitErr != nil {
		e.reportCollaborationError("Unable to apply map change", commitErr)
	}
}

func (e *Editor) finishPasteWithoutRestore(p *pasteSession) {
	if e.paste != p {
		return
	}
	selectionApplied(p.selectionOutcome, false)
	commitErr := p.commitError
	e.discardPasteWork(p)
	e.releasePasteOwnership(p)
	e.paste = nil
	if commitErr != nil {
		e.reportCollaborationError("Unable to apply map change", commitErr)
	}
}

func (e *Editor) finishPasteRollbackFailure(p *pasteSession, err error) {
	if e.paste != p {
		return
	}
	e.releasePasteOwnership(p)
	e.collaborationErr = fmt.Errorf("restore paste preview: %w", err)
	selectionApplied(p.selectionOutcome, false)
	e.reportCollaborationError("Unable to restore paste preview", e.collaborationErr)
	// Leave the display available to the explicit local-recovery flow.
	e.paste = nil
}

func (e *Editor) acceptPaste(p *pasteSession, execution executor.Executor, accepted model.AcceptedOperation, snapshot model.Snapshot, stateIndex map[model.Coord]model.TileState, acceptedChanges []model.TileChange, activeLevel int, coords []model.Coord, selectionOutcome func(bool)) {
	if e.paste != p {
		return
	}
	if snapshot.Revision < accepted.Revision {
		e.finishPasteRollbackFailure(p, fmt.Errorf("accepted paste snapshot revision precedes its acknowledgement"))
		return
	}
	e.adoptAuthoritative(snapshot, stateIndex)
	e.releasePasteOwnership(p)
	e.paste = nil
	e.refreshLargePasteView(activeLevel, coords, snapshot)
	selectionApplied(selectionOutcome, true)
	e.pushAcceptedCommand(execution, "Paste Tiles", accepted, acceptedChanges, activeLevel, coords, selectionOutcome)
}

func (e *Editor) refreshLargePasteView(_ int, coords []model.Coord, _ model.Snapshot) {
	e.syncPasteSnapshot(coords)
	e.updateAreasZones()
	e.dmm.PersistPrefabs()
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
}

func (e *Editor) adoptAuthoritative(snapshot model.Snapshot, states map[model.Coord]model.TileState) {
	e.mapViewGeneration++
	e.authoritative = snapshot
	e.authoritativeTiles = states
	e.authoritativePositions = make(map[model.Coord]int, len(snapshot.Tiles))
	for index, tile := range snapshot.Tiles {
		e.authoritativePositions[tile.Coord] = index
	}
}

func (e *Editor) discardPasteWithoutRestore() {
	p := e.paste
	if p == nil {
		return
	}
	if p.workerCancel != nil {
		p.workerCancel()
	}
	e.discardPasteWork(p)
	e.releasePasteOwnership(p)
	e.paste = nil
}

func (e *Editor) discardPasteWork(p *pasteSession) {
	if p == nil {
		return
	}
	if p.workerCancel != nil {
		p.workerCancel()
	}
	p.resultMu.Lock()
	p.discarded = true
	select {
	case result := <-p.results:
		releasePasteResult(&result)
		p.workerBusy = false
	default:
	}
	if p.workerBusy || p.commitWork != 0 {
		p.releaseOwnershipPending = true
	}
	p.resultMu.Unlock()
}

func (e *Editor) releasePasteReservation(p *pasteSession) {
	if p.reservation != nil {
		p.reservation.Release()
		p.reservation = nil
	}
}

func (e *Editor) releasePasteOwnership(p *pasteSession) {
	if p == nil {
		return
	}
	p.resultMu.Lock()
	if p.ownershipReleased {
		p.resultMu.Unlock()
		return
	}
	if p.workerBusy || p.commitWork != 0 {
		p.releaseOwnershipPending = true
		p.resultMu.Unlock()
		return
	}
	releasePasteOwnershipNow(p)
	p.resultMu.Unlock()
}

func (e *Editor) retainPasteCommitWork(p *pasteSession) {
	if p == nil {
		return
	}
	p.resultMu.Lock()
	p.commitWork++
	p.resultMu.Unlock()
}

func (e *Editor) finishPasteCommitWork(p *pasteSession) {
	if p == nil {
		return
	}
	p.resultMu.Lock()
	if p.commitWork > 0 {
		p.commitWork--
	}
	if p.releaseOwnershipPending && !p.workerBusy && p.commitWork == 0 {
		releasePasteOwnershipNow(p)
	}
	p.resultMu.Unlock()
}

func releasePasteOwnershipNow(p *pasteSession) {
	if p == nil {
		return
	}
	p.ownershipReleased = true
	if p.reservation != nil {
		p.reservation.Release()
		p.reservation = nil
	}
	if p.sourceReservation != nil {
		p.sourceReservation.Release()
		p.sourceReservation = nil
	}
	if p.releaseSource != nil {
		p.releaseSource()
		p.releaseSource = nil
	}
	p.sourceFactory = nil
	p.source = nil
	p.visible = nil
	p.releaseOwnershipPending = false
}

func releasePasteResult(result *pasteWorkResult) {
	if result == nil {
		return
	}
	result.reservation.Release()
	result.reservation = nil
	result.sourceReservation.Release()
	result.sourceReservation = nil
}

func deliverPasteWorkResult(p *pasteSession, result pasteWorkResult) {
	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	if p.discarded {
		releasePasteResult(&result)
		if p.releaseOwnershipPending && p.commitWork == 0 {
			releasePasteOwnershipNow(p)
		}
		return
	}
	p.results <- result
}

func (e *Editor) applyPasteTileState(coord model.Coord, state model.TileState) error {
	point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
	if !e.dmm.HasTile(point) {
		return fmt.Errorf("paste preview coordinate (%d,%d,%d) is outside the map", coord.X, coord.Y, coord.Z)
	}
	instances := make(dmmap.Instances, 0, len(state.Prefabs))
	for _, saved := range state.Prefabs {
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
		prefab = dmmap.PrefabStorage.Put(prefab)
		instance := dmminstance.New(point, prefab)
		instance.SetStableID(string(saved.StableID))
		instances = append(instances, instance)
	}
	e.dmm.GetTile(point).Set(instances)
	return nil
}

func (e *Editor) startPasteWorker(p *pasteSession, kind pasteWorkKind) {
	if p == nil || p.workerBusy || e.executor == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.workerCancel = cancel
	p.workerBusy = true
	request, target := p.request, p.target
	generation := p.generation
	source := p.source
	visible := p.visible
	sourceFactory := p.sourceFactory
	sourceReservation := p.sourceReservation
	sourcePrepared := p.sourcePrepared
	sourceCopyAdmitted := p.sourceCopyAdmitted
	identities := p.identities
	transforms := append([]editing.PlacementTransform(nil), p.transforms...)
	previousCoords := append([]model.Coord(nil), p.previewCoords...)
	rollbackCoords := append([]model.Coord(nil), p.rollbackCoords...)
	candidateCoords := p.previewCoords
	if p.phase == pasteRollbackWaiting {
		candidateCoords = rollbackCoords
	}
	if kind == pastePrepareWork && p.hasPreview && p.operation.OperationID != "" {
		// The current preview has already been applied to the map. Keep just its
		// coordinates while the next immutable proposal is prepared; the exact
		// previous operation body is no longer needed after the request changes.
		p.operation = model.Operation{}
		p.baseSnapshot = model.Snapshot{}
		p.stateIndex = nil
		p.positions = nil
		e.releasePasteReservation(p)
	}
	if kind == pasteRollbackWork {
		p.operation = model.Operation{}
		p.candidate = pasteWorkResult{}
		p.baseSnapshot = model.Snapshot{}
		p.stateIndex = nil
		p.positions = nil
		p.patches = nil
		p.patchIndex = 0
		e.releasePasteReservation(p)
	}
	hasPreview := p.hasPreview
	// Published authority maps are immutable: all authority changes replace
	// the entire map pointer. Workers may retain this read-only version while a
	// later generation adopts a fresh map.
	known := e.authoritativeTiles
	authoritySnapshot := e.authoritative
	defaults := editing.PlacementDefaults{Area: dmmap.BaseArea, Turf: dmmap.BaseTurf}
	execution := e.executor
	actor := e.actorID
	workBudget := e.editWorkBudget()
	go func() {
		result := pasteWorkResult{kind: kind, generation: generation, request: request}
		budget := workBudget
		var err error
		if kind == pastePrepareWork && len(source) == 0 && sourceFactory != nil {
			source, visible, result.sourceReservation, err = sourceFactory(ctx)
			if err != nil {
				result.err = err
				deliverPasteWorkResult(p, result)
				return
			}
			result.sourceReady = true
			result.source = source
			result.visible = visible
			sourceReservation = result.sourceReservation
		}
		if kind == pastePrepareWork && len(source) == 0 {
			if sourceFactory == nil {
				result.err = fmt.Errorf("paste source is unavailable")
				deliverPasteWorkResult(p, result)
				return
			}
		}
		if kind == pastePrepareWork && (visible == nil || len(source) == 0) {
			if result.sourceReady {
				result.err = fmt.Errorf("paste source factory returned an empty selection")
			} else {
				result.err = fmt.Errorf("paste source factory returned an unadmitted or empty selection")
			}
			deliverPasteWorkResult(p, result)
			return
		}
		if kind == pastePrepareWork && !sourcePrepared && !sourceCopyAdmitted {
			estimatedCopy := editing.EstimatePlacementSourceCopyMemory(source)
			if sourceReservation == nil {
				sourceReservation, err = budget.Reserve(estimatedCopy)
			} else {
				needed := sourceReservation.Bytes() + estimatedCopy
				if needed < sourceReservation.Bytes() {
					needed = ^uint64(0)
				}
				err = sourceReservation.Resize(needed)
			}
			if err != nil {
				result.sourceReady = len(source) != 0
				result.source = source
				result.visible = visible
				result.sourceReservation = sourceReservation
				result.sourceCopyAdmitted = false
				result.err = err
				deliverPasteWorkResult(p, result)
				return
			}
			sourceCopyAdmitted = true
			prepared, prepareErr := preparePlacementSource(ctx, source)
			if prepareErr != nil {
				result.sourceReady = true
				result.source = source
				result.visible = visible
				result.sourceReservation = sourceReservation
				result.sourceCopyAdmitted = true
				result.err = prepareErr
				deliverPasteWorkResult(p, result)
				return
			}
			source = prepared
			sourcePrepared = true
			result.sourceReady = true
			result.sourcePrepared = true
			result.sourceCopyAdmitted = true
			result.source = source
			result.visible = visible
			result.sourceReservation = sourceReservation
		}
		var lease *resources.Reservation
		if kind == pasteRollbackWork {
			lease, err = budget.Reserve(editing.EstimateSnapshotIndexMemory(authoritySnapshot, len(candidateCoords)))
		} else {
			lease, err = editing.ReservePlacementMemory(authoritySnapshot, source, len(transforms), budget)
		}
		if err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.reservation = lease
		snapshot, err := execution.Snapshot(ctx)
		if err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.snapshot = snapshot
		var estimated uint64
		if kind == pasteRollbackWork {
			estimated = editing.EstimateSnapshotIndexMemory(snapshot, len(candidateCoords))
		} else {
			estimated = editing.EstimatePlacementMemory(snapshot, source, len(transforms))
		}
		if err := lease.Resize(estimated); err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.stateIndex, result.positions = indexSnapshot(snapshot)
		if err := ctx.Err(); err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		if kind == pasteRollbackWork {
			result.patches, result.err = buildRollbackPatches(ctx, known, result.stateIndex, candidateCoords)
			if result.err == nil {
				result.bounds = pasteBounds(source, target)
			}
			deliverPasteWorkResult(p, result)
			return
		}
		transformed := source
		for _, transform := range transforms {
			transformed, err = editing.TransformPlacementTemplate(ctx, transformed, transform)
			if err != nil {
				result.err = err
				deliverPasteWorkResult(p, result)
				return
			}
		}
		if err := lease.Resize(editing.EstimatePlacementMemory(snapshot, transformed, 0)); err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.operation, err = editing.BuildPlacementProposalReservedWithIdentities(ctx, snapshot, actor, transformed, visible, target, func(done, total int) {
			p.progress.Store(int64(done))
			p.total.Store(int64(total))
		}, lease, identities, defaults)
		if err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.coords = operationCoords(result.operation)
		result.patches, err = buildPreviewPatches(ctx, known, result.stateIndex, previousCoords, hasPreview, result.operation)
		if err != nil {
			result.err = err
			deliverPasteWorkResult(p, result)
			return
		}
		result.bounds = pasteBounds(transformed, target)
		deliverPasteWorkResult(p, result)
	}()
}

func (e *Editor) editWorkBudget() *resources.Budget {
	if e == nil || e.workBudget == nil {
		return resources.DefaultBudget()
	}
	return e.workBudget
}

func preparePlacementSource(ctx context.Context, source []dmmap.Tile) ([]dmmap.Tile, error) {
	prepared := make([]dmmap.Tile, len(source))
	seen := make(map[model.StableID]struct{})
	for tileIndex, sourceTile := range source {
		if tileIndex&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		tile := dmmap.Tile{Coord: sourceTile.Coord}
		instances := make(dmmap.Instances, 0, len(sourceTile.Instances()))
		for _, sourceInstance := range sourceTile.Instances() {
			if sourceInstance == nil || sourceInstance.Prefab() == nil || sourceInstance.Prefab().Vars() == nil {
				return nil, fmt.Errorf("paste source contains an invalid instance")
			}
			instance := sourceInstance.Copy()
			stableID := model.StableID(instance.StableID())
			if stableID == "" || stableID.Validate() != nil {
				stableID = ""
			}
			if _, duplicate := seen[stableID]; stableID != "" && duplicate {
				stableID = ""
			}
			for stableID == "" {
				generated, err := model.NewStableID()
				if err != nil {
					return nil, fmt.Errorf("assign paste source identity: %w", err)
				}
				if _, duplicate := seen[generated]; duplicate {
					continue
				}
				stableID = generated
			}
			seen[stableID] = struct{}{}
			instance.SetStableID(string(stableID))
			instances = append(instances, &instance)
		}
		tile.Set(instances)
		prepared[tileIndex] = tile
	}
	return prepared, nil
}

func indexSnapshot(snapshot model.Snapshot) (map[model.Coord]model.TileState, map[model.Coord]int) {
	states := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	positions := make(map[model.Coord]int, len(snapshot.Tiles))
	for index, tile := range snapshot.Tiles {
		states[tile.Coord] = tile.State
		positions[tile.Coord] = index
	}
	return states, positions
}

func buildPreviewPatches(ctx context.Context, known, target map[model.Coord]model.TileState, previousCoords []model.Coord, hasPrevious bool, next model.Operation) ([]pastePatch, error) {
	patches := make(map[model.Coord]model.TileState)
	for coord, state := range target {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		old, exists := known[coord]
		if !exists || !old.Equal(state) {
			patches[coord] = state
		}
	}
	if hasPrevious {
		for _, coord := range previousCoords {
			state, exists := target[coord]
			if !exists {
				return nil, fmt.Errorf("paste rollback coordinate is absent from the current snapshot")
			}
			patches[coord] = state
		}
	}
	for _, change := range next.Changes {
		patches[change.Coord] = change.After
	}
	return sortedPatches(patches), nil
}

func buildRollbackPatches(ctx context.Context, _ map[model.Coord]model.TileState, target map[model.Coord]model.TileState, touched []model.Coord) ([]pastePatch, error) {
	patches := make(map[model.Coord]model.TileState)
	for _, coord := range touched {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, exists := target[coord]
		if !exists {
			return nil, fmt.Errorf("paste rollback coordinate is absent from the current snapshot")
		}
		// The displayed tile may contain a partial preview even when target still
		// equals the last authoritative state. Every touched coordinate must be
		// actively restored from the fresh snapshot.
		patches[coord] = state
	}
	return sortedPatches(patches), nil
}

func uniqueCoords(coords []model.Coord) []model.Coord {
	seen := make(map[model.Coord]struct{}, len(coords))
	result := coords[:0]
	for _, coord := range coords {
		if _, exists := seen[coord]; exists {
			continue
		}
		seen[coord] = struct{}{}
		result = append(result, coord)
	}
	return result
}

func (e *Editor) syncPasteSnapshot(coords []model.Coord) {
	initial := e.pMap.Snapshot().Initial()
	if initial == nil {
		return
	}
	// Dimension replacement is a full-install boundary, not an ordinary edit.
	if initial.MaxX != e.dmm.MaxX || initial.MaxY != e.dmm.MaxY || initial.MaxZ != e.dmm.MaxZ || len(initial.Tiles) != len(e.dmm.Tiles) {
		e.pMap.Snapshot().Sync()
		return
	}
	for _, coord := range coords {
		point := util.Point{X: coord.X, Y: coord.Y, Z: coord.Z}
		current, before := e.dmm.GetTile(point), initial.GetTile(point)
		if current != nil && before != nil {
			before.Set(current.Instances().Copy())
		}
	}
}

func acceptedPasteMetadata(p *pasteSession, revision model.Revision) (model.Snapshot, map[model.Coord]model.TileState, []model.TileChange) {
	snapshot := p.baseSnapshot
	snapshot.Tiles = append([]model.Tile(nil), p.baseSnapshot.Tiles...)
	snapshot.Revision = revision
	states := make(map[model.Coord]model.TileState, len(p.stateIndex))
	for coord, state := range p.stateIndex {
		states[coord] = state
	}
	changes := p.operation.Changes
	redoChanges := make([]model.TileChange, len(changes))
	for index, change := range changes {
		if tileIndex, exists := p.positions[change.Coord]; exists && tileIndex >= 0 && tileIndex < len(snapshot.Tiles) {
			snapshot.Tiles[tileIndex].State = change.After
		}
		states[change.Coord] = change.After
		redoChanges[index] = change
	}
	return snapshot, states, redoChanges
}

func sortedPatches(patches map[model.Coord]model.TileState) []pastePatch {
	result := make([]pastePatch, 0, len(patches))
	for coord, state := range patches {
		result = append(result, pastePatch{coord: coord, state: state})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].coord.Z != result[j].coord.Z {
			return result[i].coord.Z < result[j].coord.Z
		}
		if result[i].coord.Y != result[j].coord.Y {
			return result[i].coord.Y < result[j].coord.Y
		}
		return result[i].coord.X < result[j].coord.X
	})
	return result
}

func pasteBounds(source []dmmap.Tile, target util.Point) util.Bounds {
	if len(source) == 0 {
		return util.Bounds{}
	}
	minX, minY, maxX, maxY := source[0].Coord.X, source[0].Coord.Y, source[0].Coord.X, source[0].Coord.Y
	for _, tile := range source[1:] {
		minX, minY = min(minX, tile.Coord.X), min(minY, tile.Coord.Y)
		maxX, maxY = max(maxX, tile.Coord.X), max(maxY, tile.Coord.Y)
	}
	return util.Bounds{
		X1: float32(target.X), Y1: float32(target.Y),
		X2: float32(target.X + maxX - minX), Y2: float32(target.Y + maxY - minY),
	}
}

func operationCoords(operation model.Operation) []model.Coord {
	coords := make([]model.Coord, len(operation.Changes))
	for index, change := range operation.Changes {
		coords[index] = change.Coord
	}
	return coords
}

func sortedVariableNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Keep this compile-time assertion near the adapter: ToolGrab never mutates a
// large placement on its input or GL callback paths.
var _ interface {
	UpdatePastePlacement(util.Point) (util.Bounds, bool, error)
	ConfirmPastePlacement() bool
	CancelPastePlacement()
	PastePlacementClosed() bool
} = (*Editor)(nil)

// APHELION EDIT ADDITION END
