// APHELION EDIT ADDITION START - LOCAL WORK OWNER
package editor

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/util"
)

const directLocalTiles = 128

type localWork struct {
	cancel       context.CancelFunc
	generation   uint64
	execution    localEditExecutor
	reservation  *resources.Reservation
	accepted     engine.LocalAcceptance
	backward     []model.TileChange
	next         int
	applyDisplay bool
	complete     func(engine.LocalAcceptance, []model.TileChange, error)
}

// One admitted job owns this document until its display and derived views have
// caught up. prepare may read captured model values, never live display objects.
// Close cancels preparation and fences publication; already accepted work is not
// relabeled as a cancelled edit in a surviving document.
func (e *Editor) startLocalWork(execution localEditExecutor, applyDisplay bool, bytes uint64, prepare func(context.Context, *resources.Reservation) ([]model.TileChange, error), complete func(engine.LocalAcceptance, []model.TileChange, error)) error {
	if e.localWork != nil || e.mapViewClosed || e.executor != execution || e.sessionOwned {
		return fmt.Errorf("document already has work in progress or changed ownership")
	}
	reservation, err := e.editWorkBudget().Reserve(bytes)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &localWork{cancel: cancel, generation: e.attachmentGeneration, execution: execution, reservation: reservation, applyDisplay: applyDisplay, complete: complete}
	e.localWork = w
	documentID, revision := e.authoritative.DocumentID, e.authoritative.Revision
	compositionLock := e.compositionEditFence()
	go func() {
		version, err := execution.LocalVersion(ctx)
		if err == nil && (version.DocumentID != documentID || version.Revision != revision) {
			err = fmt.Errorf("local authority changed before submission")
		}
		var changes []model.TileChange
		if err == nil {
			stage := uistage.Begin(uistage.LocalPrepare)
			changes, err = prepare(ctx, reservation)
			stage.End()
		}
		if err == nil {
			err = reservation.Resize(localChangesBytes(changes))
		}
		if err == nil {
			err = validateCompositionChanges(compositionLock, changes)
		}
		if err == nil {
			stage := uistage.Begin(uistage.LocalApply)
			w.accepted, err = execution.ApplyLocal(ctx, engine.LocalRequest{Version: version, Changes: changes})
			stage.End()
		}
		if err == nil {
			// Adjacent installation groups refresh each affected render chunk once.
			sort.Slice(w.accepted.Changes, func(i, j int) bool {
				a, b := w.accepted.Changes[i].Coord, w.accepted.Changes[j].Coord
				if a.Z != b.Z {
					return a.Z < b.Z
				}
				if (a.Y-1)/(chunk.Size+1) != (b.Y-1)/(chunk.Size+1) {
					return a.Y < b.Y
				}
				if (a.X-1)/(chunk.Size+1) != (b.X-1)/(chunk.Size+1) {
					return a.X < b.X
				}
				if a.Y != b.Y {
					return a.Y < b.Y
				}
				return a.X < b.X
			})
			w.backward = reverseLocalChanges(w.accepted.Changes)
		}
		e.app.RunLater(func() {
			if err != nil {
				e.finishLocalWork(w, err)
				return
			}
			e.continueLocalWork(w)
		})
	}()
	return nil
}

func (e *Editor) continueLocalWork(w *localWork) {
	defer uistage.Begin(uistage.LocalRefresh).End()
	if e.localWork != w || w.generation != e.attachmentGeneration || e.mapViewClosed {
		e.finishLocalWork(w, fmt.Errorf("editor ownership changed"))
		return
	}
	if len(w.accepted.Changes) == 0 {
		e.finishLocalWork(w, nil)
		return
	}
	if w.accepted.DocumentID != e.authoritative.DocumentID || w.accepted.Revision != e.authoritative.Revision+1 {
		e.finishLocalWork(w, fmt.Errorf("local acceptance is not contiguous with displayed authority"))
		return
	}
	started := time.Now()
	for w.next < len(w.accepted.Changes) {
		first := w.next
		key := localChunk(w.accepted.Changes[first].Coord)
		for w.next < len(w.accepted.Changes) && localChunk(w.accepted.Changes[w.next].Coord) == key {
			w.next++
		}
		batch := w.accepted.Changes[first:w.next]
		coords := make([]model.Coord, 0, len(batch))
		points := make([]util.Point, 0, len(batch))
		for _, change := range batch {
			if err := e.installLocalTile(change, w.applyDisplay); err != nil {
				e.finishLocalWork(w, err)
				return
			}
			coords = append(coords, change.Coord)
			points = append(points, util.Point{X: change.Coord.X, Y: change.Coord.Y, Z: change.Coord.Z})
		}
		e.syncPasteSnapshot(coords)
		// Include geometry work in this frame's budget; never defer an unbounded
		// all-level refresh behind a supposedly completed publication.
		if r := e.pMap.Canvas().Render(); r != nil {
			r.UpdateBucketV(e.dmm, key.Z, points)
		}
		if time.Since(started) >= 2*time.Millisecond {
			break
		}
	}
	if w.next < len(w.accepted.Changes) {
		e.app.RunLater(func() { e.continueLocalWork(w) })
		return
	}
	e.authoritative.Revision = w.accepted.Revision
	e.mapViewGeneration++
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	e.finishLocalWork(w, nil)
}

func (e *Editor) finishLocalWork(w *localWork, err error) {
	w.cancel()
	defer w.reservation.Release()
	if e.localWork == w {
		e.localWork = nil
	}
	if w.generation != e.attachmentGeneration || e.mapViewClosed {
		err = fmt.Errorf("editor ownership changed")
	}
	complete := w.complete
	w.complete = nil
	if complete != nil {
		complete(w.accepted, w.backward, err)
	}
}

func localChunk(c model.Coord) model.Coord {
	return model.Coord{X: (c.X - 1) / (chunk.Size + 1), Y: (c.Y - 1) / (chunk.Size + 1), Z: c.Z}
}

func reverseLocalChanges(forward []model.TileChange) []model.TileChange {
	backward := make([]model.TileChange, len(forward))
	for i, c := range forward {
		backward[i] = model.TileChange{Coord: c.Coord, Before: c.After, After: c.Before}
	}
	return backward
}

func localTileBytes(state model.TileState) uint64 {
	bytes := uint64(256)
	for _, p := range state.Prefabs {
		bytes = addWorkBytes(bytes, uint64(256+len(p.Path)+len(p.StableID)))
		for name, value := range p.Vars {
			bytes = addWorkBytes(bytes, uint64(96+len(name)+len(value)))
		}
	}
	return bytes
}
func localChangesBytes(changes []model.TileChange) uint64 {
	bytes := uint64(1024)
	for _, c := range changes {
		bytes = addWorkBytes(bytes, addWorkBytes(localTileBytes(c.Before), localTileBytes(c.After)))
	}
	if bytes > math.MaxUint64/8 {
		return math.MaxUint64
	}
	return bytes * 8
}
func addWorkBytes(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// APHELION EDIT ADDITION END
