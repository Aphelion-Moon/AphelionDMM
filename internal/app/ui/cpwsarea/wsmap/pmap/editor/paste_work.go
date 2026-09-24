// APHELION EDIT ADDITION START - ISOLATED PASTE PRESENTATION
package editor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

const (
	pasteUIChunkTiles = 384
	pasteUIChunkTime  = 2 * time.Millisecond
)

type pastePhase uint8

const (
	pastePreparing pastePhase = iota
	pasteReady
	pasteResolving
)

type pasteSourceFactory func(context.Context) ([]dmmap.Tile, func(string) bool, *resources.Reservation, error)

type pasteWorkResult struct {
	request     uint64
	source      []dmmap.Tile
	visible     func(string) bool
	payload     *editing.PlacementPayload
	reservation *resources.Reservation
	err         error
}

type pasteSession struct {
	generation            uint64
	level                 int
	source                []dmmap.Tile
	visible               func(string) bool
	sourceFactory         pasteSourceFactory
	releaseSource         func()
	reservation           *resources.Reservation
	payload               *editing.PlacementPayload
	target                util.Point
	orientation           editing.Orientation
	preparedOrientation   editing.Orientation
	policy                editing.PastePolicy
	request               uint64
	prepared              uint64
	sourcePrepared        bool
	phase                 pastePhase
	err                   error
	bounds                util.Bounds
	intent                *util.Point
	selectionOutcome      func(bool)
	workerCancel          context.CancelFunc
	workerBusy            bool
	results               chan pasteWorkResult
	resultMu              sync.Mutex
	discarded             bool
	commitWork            int
	presentation          *render.Presentation
	preparingPresentation *render.Presentation
	presentationBuild     *presentationBuild
}

type presentationBuild struct {
	presentation  *render.Presentation
	tileCount     int
	instanceCount func(int) int
	appearance    func(int, int) render.Appearance
	tile          int
	instance      int
	finished      bool
}

func (e *Editor) beginPasteProposal(source []dmmap.Tile, visible func(string) bool, target util.Point) error {
	if len(source) == 0 || visible == nil {
		return fmt.Errorf("paste requires a nonempty selection")
	}
	return e.beginPasteSession(source, visible, nil, nil, target)
}

func (e *Editor) beginPasteProposalFromFactory(tileCount int, factory pasteSourceFactory, release func(), target util.Point) error {
	if tileCount < 1 || factory == nil {
		return fmt.Errorf("paste requires a nonempty selection")
	}
	return e.beginPasteSession(nil, nil, factory, release, target)
}

func (e *Editor) beginPasteSession(source []dmmap.Tile, visible func(string) bool, factory pasteSourceFactory, release func(), target util.Point) error {
	if e.executor == nil || target.Z != e.pMap.ActiveLevel() {
		return fmt.Errorf("paste requires an available selected level")
	}
	p := &pasteSession{generation: e.attachmentGeneration, level: target.Z, source: source, visible: visible, sourceFactory: factory, releaseSource: release, target: target, orientation: editing.IdentityOrientation(), policy: editing.PastePolicy{Channels: editing.AllChannels}, request: 1, phase: pastePreparing, results: make(chan pasteWorkResult, 1)}
	e.paste = p
	e.startPasteWorker(p)
	return nil
}

func (e *Editor) UpdatePastePlacement(target util.Point) (util.Bounds, bool, error) {
	p := e.paste
	if p == nil || p.generation != e.attachmentGeneration {
		return util.Bounds{}, false, fmt.Errorf("paste belongs to an old attachment")
	}
	if p.intent == nil && p.phase != pasteResolving {
		if target != p.target && p.sourcePrepared && p.prepared == p.request && !p.workerBusy {
			p.err = nil
		}
		p.target = target
	}
	if p.payload != nil {
		p.bounds = p.payload.Bounds(p.target)
		if p.presentation != nil {
			p.presentation.Anchor = p.target
		}
		if p.preparingPresentation != nil {
			p.preparingPresentation.Anchor = p.target
		}
	}
	if p.phase == pasteResolving {
		return p.bounds, false, fmt.Errorf("paste commit is resolving")
	}
	if p.err != nil {
		return p.bounds, false, p.err
	}
	if p.payload == nil || p.prepared != p.request {
		return p.bounds, false, fmt.Errorf("preparing paste source")
	}
	if err := p.payload.ValidateTarget(p.target, e.dmm.MaxX, e.dmm.MaxY, p.level); err != nil {
		return p.bounds, false, err
	}
	return p.bounds, true, nil
}

func (e *Editor) TransformPreparedPastePlacement(transform editing.PlacementTransform) error {
	p := e.paste
	if p == nil || p.intent != nil || p.phase == pasteResolving {
		return fmt.Errorf("no available paste placement")
	}
	if transform < editing.PlacementRotateRight || transform > editing.PlacementMirrorVertical {
		return fmt.Errorf("unknown paste transform")
	}
	next := p.orientation.Transform(transform)
	if next == p.orientation {
		return nil
	}
	if p.payload != nil && (transform == editing.PlacementRotateRight || transform == editing.PlacementRotateLeft) && (p.target.X+p.payload.Height-1 > e.dmm.MaxX || p.target.Y+p.payload.Width-1 > e.dmm.MaxY) {
		p.err = fmt.Errorf("transformed paste would leave the map; move it inward first")
		return p.err
	}
	p.orientation = next
	p.request++
	p.err = nil
	if !p.workerBusy {
		e.startPasteWorker(p)
	}
	return nil
}

func (e *Editor) PastePolicy() (editing.PastePolicy, bool) {
	if e.paste == nil {
		return editing.PastePolicy{}, false
	}
	return e.paste.policy, true
}
func (e *Editor) SetPastePolicy(policy editing.PastePolicy) {
	if p := e.paste; p != nil && p.intent == nil && p.phase != pasteResolving {
		p.policy = policy
		p.err = nil
	}
}
func (e *Editor) PastePlacementPending() bool {
	return e.paste != nil && (e.paste.intent != nil || e.paste.phase == pasteResolving)
}

// Retain a confirmation's exact target while preparing; hover is replaceable,
// but a click is not. Preparing new source never silently consumes that click.
func (e *Editor) ConfirmPastePlacement() bool {
	p := e.paste
	if p == nil || p.generation != e.attachmentGeneration || p.phase == pasteResolving || p.intent != nil || p.target.Z != p.level {
		return false
	}
	if p.err != nil {
		return false
	}
	if p.payload != nil {
		if err := p.payload.ValidateTarget(p.target, e.dmm.MaxX, e.dmm.MaxY, p.level); err != nil {
			p.err = err
			return false
		}
	}
	target := p.target
	p.intent = &target
	p.selectionOutcome = e.selectionOutcome
	if p.payload != nil && p.prepared == p.request {
		e.submitPasteIntent(p)
	}
	return true
}

func (e *Editor) CancelPastePlacement() {
	if p := e.paste; p != nil && p.phase != pasteResolving {
		e.discardPasteWithoutRestore()
	}
}
func (e *Editor) PastePlacementClosed() bool { return e.paste == nil }

// PasteSelection is read during acceptance, before the source session is freed.
func (e *Editor) PasteSelection() editing.Selection {
	p := e.paste
	if p == nil || p.payload == nil {
		return editing.Selection{}
	}
	points := make([]util.Point, 0, len(p.payload.Tiles))
	for _, tile := range p.payload.Tiles {
		points = append(points, util.Point{X: p.target.X + tile.Coord.X - 1, Y: p.target.Y + tile.Coord.Y - 1, Z: p.level})
	}
	s, _ := editing.MaskSelection(points)
	return s
}

func (e *Editor) CanCancelPastePlacement() bool {
	return e.paste != nil && e.paste.phase != pasteResolving
}

func (e *Editor) pasteBlocksCommittedView() bool {
	return e.paste != nil && e.paste.phase == pasteResolving
}
func (e *Editor) PastePlacementProgress() string {
	p := e.paste
	if p == nil {
		return ""
	}
	if p.phase == pasteResolving {
		return "Applying paste; waiting for the accepted result"
	}
	if p.err != nil {
		return ""
	}
	if p.payload == nil || p.prepared != p.request {
		return "Preparing paste source"
	}
	if p.preparingPresentation != nil {
		prepared := 0
		if p.presentationBuild != nil {
			prepared = p.presentationBuild.tile
		}
		return fmt.Sprintf("Preparing paste sprites: %d / %d tiles", prepared, len(p.payload.Tiles))
	}
	return ""
}

func (e *Editor) ProcessPasteWork() {
	e.processSelectionMoveWork()
	p := e.paste
	if p == nil {
		return
	}
	if p.generation != e.attachmentGeneration {
		e.discardPasteWithoutRestore()
		return
	}
	if p.phase != pasteResolving && e.pMap.ActiveLevel() != p.level {
		e.CancelPastePlacement()
		return
	}
	if p.workerBusy {
		select {
		case result := <-p.results:
			p.workerBusy = false
			p.workerCancel = nil
			if result.source != nil {
				p.source = result.source
				p.sourcePrepared = true
				p.visible = result.visible
				p.sourceFactory = nil
				if p.releaseSource != nil {
					p.releaseSource()
					p.releaseSource = nil
				}
			}
			if result.reservation != nil {
				p.reservation = result.reservation
			}
			if result.request != p.request {
				e.startPasteWorker(p)
				return
			}
			if result.err != nil {
				p.err = result.err
				p.phase = pasteReady
				p.intent = nil
				if p.payload != nil {
					p.orientation = p.preparedOrientation
					p.prepared = p.request
				}
				return
			}
			p.payload = result.payload
			p.preparedOrientation = p.orientation
			p.prepared = result.request
			p.bounds = p.payload.Bounds(p.target)
			p.err = nil
			p.phase = pasteReady
			e.preparePastePresentation(p)
		default:
		}
	}
	if p.payload != nil && p.prepared == p.request && p.phase != pasteResolving {
		e.preparePasteSprites(p)
		if p.intent != nil {
			e.submitPasteIntent(p)
		}
	}
}

// Work depends on effective orientation, never the target or keypress count.
func (e *Editor) startPasteWorker(p *pasteSession) {
	if p.workerBusy {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.workerCancel = cancel
	p.workerBusy = true
	p.phase = pastePreparing
	source, visible, factory, reservation := p.source, p.visible, p.sourceFactory, p.reservation
	orientation, request, first := p.orientation, p.request, !p.sourcePrepared
	releaseSource := p.releaseSource
	p.releaseSource = nil
	budget := e.editWorkBudget()
	go func() {
		if releaseSource != nil {
			defer releaseSource()
		}
		r := pasteWorkResult{request: request, reservation: reservation}
		defer func() {
			p.resultMu.Lock()
			defer p.resultMu.Unlock()
			if p.discarded {
				r.reservation.Release()
				return
			}
			p.results <- r
		}()
		var err error
		if factory != nil {
			source, visible, reservation, err = factory(ctx)
			if err != nil {
				r.err = err
				reservation.Release()
				return
			}
		}
		if reservation == nil {
			reservation, err = budget.Reserve(editing.EstimatePlacementPresentationMemory(source))
			if err != nil {
				r.err = err
				return
			}
		} else if first && factory != nil {
			estimate := editing.EstimatePlacementPresentationMemory(source)
			needed := reservation.Bytes() + estimate
			if needed < estimate {
				needed = ^uint64(0)
			}
			err = reservation.Resize(needed)
			if err != nil {
				r.err = err
				reservation.Release()
				return
			}
		}
		r.reservation = reservation
		if first {
			source, err = preparePlacementSource(ctx, source)
			if err != nil {
				r.err = err
				return
			}
			r.source, r.visible = source, visible
		}
		transformed, err := orientation.Prepare(ctx, source)
		if err != nil {
			r.err = err
			return
		}
		r.payload, r.err = editing.CompilePlacementPayload(ctx, transformed, visible)
	}()
}

func (e *Editor) preparePastePresentation(p *pasteSession) {
	payload := p.payload
	presentation := &render.Presentation{Anchor: p.target, IconSize: dmmap.WorldIconSize}
	p.preparingPresentation = presentation
	validTarget := func() bool {
		return p.target.Z == p.level && p.target.X > 0 && p.target.Y > 0 && p.target.X+payload.Width-1 <= e.dmm.MaxX && p.target.Y+payload.Height-1 <= e.dmm.MaxY
	}
	presentation.Suppress = func(u unit.Unit) bool {
		if !validTarget() {
			return false
		}
		coord := u.Instance().Coord()
		local := util.Point{X: coord.X - p.target.X, Y: coord.Y - p.target.Y}
		intent, exists := payload.Intents[local]
		if !exists {
			return false
		}
		before := e.authoritativeTiles[model.Coord{X: coord.X, Y: coord.Y, Z: coord.Z}]
		return editing.CheckComposition(before, intent, p.policy, p.visible) == nil && p.policy.Suppresses(u.Instance().Prefab().Path(), intent, p.visible)
	}
	presentation.Visible = func(a render.Appearance) bool {
		if !validTarget() || !e.app.PathsFilter().IsVisiblePath(a.Path) {
			return false
		}
		intent := payload.Intents[a.Coord]
		if !p.policy.Writes(editing.ChannelForPath(a.Path), intent[editing.ChannelForPath(a.Path)]) {
			return false
		}
		before := e.authoritativeTiles[model.Coord{X: p.target.X + a.Coord.X, Y: p.target.Y + a.Coord.Y, Z: p.target.Z}]
		return editing.CheckComposition(before, intent, p.policy, p.visible) == nil
	}
	p.presentationBuild = &presentationBuild{
		presentation: presentation,
		tileCount:    len(payload.Tiles),
		instanceCount: func(tile int) int {
			return len(payload.Tiles[tile].Instances())
		},
		appearance: func(tile, instance int) render.Appearance {
			source := payload.Tiles[tile]
			return render.PrepareAppearance(source.Coord, source.Instances()[instance], dmmap.WorldIconSize)
		},
	}
}

func (e *Editor) preparePasteSprites(p *pasteSession) {
	if p.preparingPresentation != nil && e.preparePresentationBuild(p.presentationBuild) {
		e.publishPastePresentation(p)
	}
}

func (e *Editor) publishPastePresentation(p *pasteSession) {
	p.presentation = p.presentationBuild.presentation
	p.preparingPresentation = nil
	if r := e.pMap.Canvas().Render(); r != nil {
		r.SetPresentation(p.presentation)
	}
}

// preparePresentationBuild incrementally prepares one immutable sprite stream
// for either paste or ordinary move, keeping large selection work inside the
// same per-frame budget.
func (e *Editor) preparePresentationBuild(build *presentationBuild) bool {
	if build == nil {
		return false
	}
	if build.finished {
		return true
	}
	if e.pMap.Canvas().Render() == nil {
		build.presentation.Finish()
		build.finished = true
		return true
	}
	started, count := time.Now(), 0
	for build.tile < build.tileCount {
		instances := build.instanceCount(build.tile)
		if build.instance >= instances {
			build.tile++
			build.instance = 0
			continue
		}
		build.presentation.Add(build.appearance(build.tile, build.instance))
		build.instance++
		count++
		if count >= pasteUIChunkTiles || time.Since(started) >= pasteUIChunkTime {
			return false
		}
	}
	build.presentation.Finish()
	build.finished = true
	return true
}

func (e *Editor) submitPasteIntent(p *pasteSession) {
	target := *p.intent
	if err := p.payload.ValidateTarget(target, e.dmm.MaxX, e.dmm.MaxY, p.level); err != nil {
		p.err = err
		p.intent = nil
		return
	}
	if local, ok := e.executor.(localEditExecutor); ok && !e.sessionOwned {
		// All mutation entry points are fenced by localWork, so this model map
		// remains read-only until preparation has relinquished it to publication.
		base, payload, policy, visible := e.authoritativeTiles, p.payload, p.policy, p.visible
		generation := e.attachmentGeneration
		p.phase = pasteResolving
		e.retainPasteCommit(p)
		err := e.startLocalWork(local, true, 1024, func(ctx context.Context, reservation *resources.Reservation) ([]model.TileChange, error) {
			bytes := uint64(1024)
			for _, tile := range payload.Tiles {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				coord := model.Coord{X: target.X + tile.Coord.X - 1, Y: target.Y + tile.Coord.Y - 1, Z: target.Z}
				intent := payload.Intents[util.Point{X: tile.Coord.X - 1, Y: tile.Coord.Y - 1}]
				writes := false
				for c, channel := range intent {
					if policy.Writes(editing.Channel(c), channel) {
						writes = true
						bytes = addWorkBytes(bytes, localTileBytes(model.TileState{Prefabs: channel.Data}))
					}
				}
				if writes {
					bytes = addWorkBytes(bytes, localTileBytes(base[coord]))
				}
			}
			if bytes > ^uint64(0)/8 {
				bytes = ^uint64(0)
			} else {
				bytes *= 8
			}
			if err := reservation.Resize(bytes); err != nil {
				return nil, err
			}
			return payload.BuildPlacementChanges(ctx, target, policy, visible, func(coord model.Coord) (model.TileState, bool) { state, ok := base[coord]; return state, ok })
		}, func(accepted engine.LocalAcceptance, backward []model.TileChange, err error) {
			defer e.finishPasteCommit(p)
			if generation != e.attachmentGeneration || e.paste != p {
				return
			}
			if err != nil {
				p.err = err
				p.intent = nil
				p.phase = pasteReady
				if len(accepted.Changes) > 0 {
					e.collaborationErr = err
					e.reportCollaborationError("Unable to display accepted paste", err)
				}
				return
			}
			if len(accepted.Changes) > 0 {
				e.pushLocalCommandOwned(local, "Paste Tiles", accepted.Changes, backward, p.selectionOutcome)
			}
			e.finishPaste(p, len(accepted.Changes) > 0)
		})
		if err != nil {
			e.finishPasteCommit(p)
			p.err = err
			p.intent = nil
			p.phase = pasteReady
		}
		return
	}
	changes, err := p.payload.BuildPlacementChanges(context.Background(), target, p.policy, p.visible, func(coord model.Coord) (model.TileState, bool) {
		state, ok := e.authoritativeTiles[coord]
		return state, ok
	})
	if err != nil {
		p.err = err
		p.intent = nil
		return
	}
	if len(changes) == 0 {
		e.finishPaste(p, false)
		return
	}
	execution, generation := e.executor, e.attachmentGeneration
	// Session hashing/snapshot work occurs only at the real commit boundary.
	p.phase = pasteResolving
	e.retainPasteCommit(p)
	actor := e.actorID
	go func() {
		snapshot, err := execution.Snapshot(context.Background())
		var operation model.Operation
		if err == nil {
			operation, err = placementWireOperation(snapshot, actor, changes)
		}
		if err != nil {
			e.app.RunLater(func() {
				defer e.finishPasteCommit(p)
				if generation == e.attachmentGeneration && e.paste == p {
					p.err = err
					p.intent = nil
					p.phase = pasteReady
				}
			})
			return
		}
		complete := func(accepted model.AcceptedOperation, executeErr error) {
			// A coherent snapshot includes every intervening accepted revision.
			go func() {
				var current model.Snapshot
				var snapshotErr error
				if executeErr == nil {
					current, snapshotErr = execution.Snapshot(context.Background())
					if snapshotErr == nil && current.Revision < accepted.Revision {
						snapshotErr = fmt.Errorf("accepted snapshot is behind acknowledgement")
					}
				}
				e.app.RunLater(func() {
					defer e.finishPasteCommit(p)
					if generation != e.attachmentGeneration || e.paste != p {
						return
					}
					if executeErr != nil {
						p.err = executeErr
						p.intent = nil
						p.phase = pasteReady
						return
					}
					if snapshotErr != nil {
						e.collaborationErr = snapshotErr
						e.reportCollaborationError("Unable to capture accepted paste", snapshotErr)
						return
					}
					if err := mapadapter.ApplyWithEnvironment(e.dmm, current, e.app.LoadedEnvironment()); err != nil {
						e.collaborationErr = err
						return
					}
					e.setAuthoritative(current)
					e.refreshCollaborationView(p.level, nil, current)
					coords := make([]model.Coord, len(accepted.Changes))
					for i, c := range accepted.Changes {
						coords[i] = c.Coord
					}
					e.pushAcceptedCommand(execution, "Paste Tiles", accepted, accepted.Changes, p.level, coords, p.selectionOutcome)
					e.finishPaste(p, true)
				})
			}()
		}
		if async, ok := execution.(executor.AsyncExecutor); ok {
			if err := async.ExecuteAsync(context.Background(), operation, complete); err != nil {
				complete(model.AcceptedOperation{}, err)
			}
		} else {
			accepted, err := execution.Execute(context.Background(), operation)
			complete(accepted, err)
		}
	}()
}

func placementWireOperation(snapshot model.Snapshot, actor model.ActorID, changes []model.TileChange) (model.Operation, error) {
	hash, err := snapshot.Hash()
	if err != nil {
		return model.Operation{}, err
	}
	id, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, err
	}
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID, ActorID: actor, OperationID: id, BaseRevision: snapshot.Revision, BaseMapHash: hash, EnvironmentHash: snapshot.EnvironmentHash, Kind: model.OperationKindTileChange, Changes: changes}, nil
}

func (e *Editor) finishPaste(p *pasteSession, applied bool) {
	selectionApplied(p.selectionOutcome, applied)
	e.discardPasteWithoutRestore()
}

func (e *Editor) discardPasteWithoutRestore() {
	p := e.paste
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
		result.reservation.Release()
		p.workerBusy = false
	default:
	}
	if !p.workerBusy && p.commitWork == 0 {
		p.reservation.Release()
	}
	if p.releaseSource != nil {
		p.releaseSource()
		p.releaseSource = nil
	}
	p.resultMu.Unlock()
	if r := e.pMap.Canvas().Render(); r != nil {
		r.SetPresentation(nil)
	}
	e.paste = nil
}

func (e *Editor) retainPasteCommit(p *pasteSession) {
	p.resultMu.Lock()
	p.commitWork++
	p.resultMu.Unlock()
}
func (e *Editor) finishPasteCommit(p *pasteSession) {
	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	p.commitWork--
	if p.discarded && p.commitWork == 0 && !p.workerBusy {
		p.reservation.Release()
	}
}

// APHELION EDIT ADDITION END
