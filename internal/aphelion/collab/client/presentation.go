package client

import (
	"fmt"
	"sort"
	"sync"

	"sdmm/internal/aphelion/collab/model"
)

// PresentationUpdate is the client-to-editor publication boundary. Ordinary
// updates contain only coordinates whose authoritative or effective display
// state changed since the editor's last successful drain. Replacement is set
// only for an explicit validated snapshot fallback.
type PresentationUpdate struct {
	Sequence        uint64
	DocumentID      model.DocumentID
	EnvironmentHash string
	Revision        model.Revision
	MapHash         string
	Authoritative   []model.Tile
	Display         []model.Tile
	Replacement     *model.Snapshot
}

// publishedProjection is immutable after publication. The executor stores it
// atomically so UI metadata/capture reads never wait for reconciliation work.
type publishedProjection struct {
	tileIndexes   map[model.Coord]int
	visibleTiles  map[model.Coord]model.TileState
	visibleCoords []model.Coord
	projection    Projection
	mapHash       string
	hasPending    bool
	conflicts     []Conflict
}

const (
	// These bounds cover a burst of ordinary gesture submissions while keeping
	// both the pre-send dispatch and ACK-pending ownership finite. A sent
	// operation keeps its budget until its ACK, rejection, suspension, or
	// terminal cleanup releases it.
	maxLocalAdmissionCount = 64
	maxLocalAdmissionBytes = 256 << 20
)

var errLocalAdmissionFull = fmt.Errorf("local collaboration submission admission is full")

type publicationState struct {
	mu sync.Mutex

	sequence      uint64
	ready         bool
	authoritative map[model.Coord]model.Tile
	display       map[model.Coord]model.Tile
	replacement   *model.Snapshot

	localAdmissions       int
	localAdmissionBytes   uint64
	queuedAdmission       map[uint64]model.Operation
	admissionErr          error
	interruptedBefore     uint64
	pendingAdmission      map[model.OperationID]uint64
	pendingAdmissionBytes uint64
	nextDispatch          uint64
	dispatchTurn          uint64
	dispatchWake          chan struct{}
	completionTurn        uint64
	completion            map[uint64]completionEntry
	completionRunning     bool
}

type completionEntry struct {
	accepted model.AcceptedOperation
	err      error
	callback func(model.AcceptedOperation, error)
}

func newPublicationState() publicationState {
	return publicationState{
		authoritative:    make(map[model.Coord]model.Tile),
		display:          make(map[model.Coord]model.Tile),
		pendingAdmission: make(map[model.OperationID]uint64),
		queuedAdmission:  make(map[uint64]model.Operation),
		dispatchWake:     make(chan struct{}),
		completion:       make(map[uint64]completionEntry),
	}
}

// recordCompletion preserves admission order for asynchronous callbacks while
// invoking them outside every executor/publication lock. Synchronous Execute
// calls record a nil callback skip as soon as their transport handoff ends.
func (network *NetworkExecutor) recordCompletion(ticket uint64, accepted model.AcceptedOperation, err error, callback func(model.AcceptedOperation, error)) {
	network.publication.mu.Lock()
	network.publication.completion[ticket] = completionEntry{accepted: accepted, err: err, callback: callback}
	if network.publication.completionRunning {
		network.publication.mu.Unlock()
		return
	}
	network.publication.completionRunning = true
	network.publication.mu.Unlock()
	network.drainCompletions()
}

// drainCompletions is the sole callback owner while it is running. A callback
// may block or re-enter the executor, so each entry is detached before the
// callback runs and no executor/publication lock is held across user code.
func (network *NetworkExecutor) drainCompletions() {
	for {
		network.publication.mu.Lock()
		entry, exists := network.publication.completion[network.publication.completionTurn]
		if !exists {
			network.publication.completionRunning = false
			network.publication.mu.Unlock()
			return
		}
		delete(network.publication.completion, network.publication.completionTurn)
		network.publication.completionTurn++
		network.publication.mu.Unlock()
		if entry.callback != nil {
			entry.callback(entry.accepted, entry.err)
		}
	}
}

// TakePresentationUpdate drains the single editor-consumer publication
// accumulator. The returned tile states are detached from executor-owned
// state, and coordinates are sorted for deterministic consumers. While a
// locally admitted submission is still waiting to install its pending view,
// nil is returned so an older publication cannot overwrite a newer gesture.
func (network *NetworkExecutor) TakePresentationUpdate() *PresentationUpdate {
	network.publication.mu.Lock()
	if network.publication.localAdmissions != 0 || !network.publication.ready {
		network.publication.mu.Unlock()
		return nil
	}

	published := network.published.Load()
	if published == nil {
		network.publication.mu.Unlock()
		return nil
	}
	authoritative := network.publication.authoritative
	display := network.publication.display
	replacement := network.publication.replacement
	update := &PresentationUpdate{
		Sequence:        network.publication.sequence,
		DocumentID:      published.projection.Acknowledged.DocumentID,
		EnvironmentHash: published.projection.Acknowledged.EnvironmentHash,
		Revision:        published.projection.Acknowledged.Revision,
		MapHash:         published.mapHash,
	}
	network.publication.authoritative = make(map[model.Coord]model.Tile)
	network.publication.display = make(map[model.Coord]model.Tile)
	network.publication.replacement = nil
	network.publication.ready = false
	network.publication.mu.Unlock()
	// Clone only the detached sparse payload after releasing the short
	// publication critical section. Full replacement is already an explicit
	// recovery boundary and is likewise materialized off the UI lock.
	update.Authoritative = takeTiles(authoritative)
	update.Display = takeTiles(display)
	if replacement != nil {
		replacementSnapshot := model.CloneSnapshot(*replacement)
		update.Replacement = &replacementSnapshot
	}
	return update
}

// HasPresentationUpdate reports whether the editor has a coalesced publication
// waiting, or an admitted submission still installing its pending view.
func (network *NetworkExecutor) HasPresentationUpdate() bool {
	network.publication.mu.Lock()
	defer network.publication.mu.Unlock()
	return network.publication.localAdmissions != 0 || network.publication.ready
}

func takeTiles(source map[model.Coord]model.Tile) []model.Tile {
	if len(source) == 0 {
		return nil
	}
	coords := make([]model.Coord, 0, len(source))
	for coord := range source {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(left, right int) bool { return coordLess(coords[left], coords[right]) })
	result := make([]model.Tile, 0, len(coords))
	for _, coord := range coords {
		tile := source[coord]
		result = append(result, model.Tile{Coord: tile.Coord, State: model.CloneTileState(tile.State)})
	}
	return result
}

func coordLess(left, right model.Coord) bool {
	if left.Z != right.Z {
		return left.Z < right.Z
	}
	if left.Y != right.Y {
		return left.Y < right.Y
	}
	return left.X < right.X
}

func (network *NetworkExecutor) publishCaptureLocked() {
	projection := network.projection
	mapHash := projection.acknowledgedHash
	var conflicts []Conflict
	if prior := network.published.Load(); prior != nil {
		conflicts = prior.conflicts
	}
	if network.conflictsDirty {
		// Entries are detached on retention and never mutated. Only membership
		// changes, so publish a new slice without copying all retained drafts.
		conflicts = append([]Conflict(nil), network.conflicts...)
		network.conflictsDirty = false
	}
	network.published.Store(&publishedProjection{
		tileIndexes:   network.tileIndexes,
		visibleTiles:  network.visibleTiles,
		visibleCoords: network.visibleCoords,
		projection:    projection,
		mapHash:       mapHash,
		hasPending:    len(projection.Pending) != 0 || network.publication.localAdmissions != 0 || len(network.publication.pendingAdmission) != 0,
		conflicts:     conflicts,
	})
}

func (network *NetworkExecutor) publishAdmissionMetadataLocked() {
	prior := network.published.Load()
	if prior == nil {
		return
	}
	updated := *prior
	updated.hasPending = true
	network.published.Store(&updated)
}

func buildAuthorityIndex(snapshot model.Snapshot) (map[model.Coord]model.TileState, map[model.StableID]model.Coord) {
	tiles := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	owners := make(map[model.StableID]model.Coord)
	for _, tile := range snapshot.Tiles {
		tiles[tile.Coord] = tile.State
		for _, prefab := range tile.State.Prefabs {
			owners[prefab.StableID] = tile.Coord
		}
	}
	return tiles, owners
}

func (network *NetworkExecutor) applyAuthorityChanges(changes []model.TileChange) {
	// Remove all old owners before assigning any new owner. This is required
	// for a stable-ID move/swap batch where one change temporarily reuses an ID
	// that another change moves away from its original coordinate.
	for _, change := range changes {
		if prior, exists := network.authorityTiles[change.Coord]; exists {
			for _, prefab := range prior.Prefabs {
				delete(network.authorityOwners, prefab.StableID)
			}
		}
	}
	for _, change := range changes {
		state := network.acknowledgedTileLocked(change.Coord)
		network.authorityTiles[change.Coord] = state
		for _, prefab := range state.Prefabs {
			network.authorityOwners[prefab.StableID] = change.Coord
		}
	}
}

func (network *NetworkExecutor) authorityTileState(coord model.Coord) model.TileState {
	if state, exists := network.authorityTiles[coord]; exists {
		return model.CloneTileState(state)
	}
	return model.TileState{}
}

func effectiveProjectionStates(projection Projection, authorityTiles map[model.Coord]model.TileState, authorityOwners map[model.StableID]model.Coord, coords map[model.Coord]struct{}, stateOverrides map[model.Coord]model.TileState, ownerOverrides map[model.StableID]effectiveOwner) map[model.Coord]model.TileState {
	states, _ := effectiveProjectionOverlay(projection, authorityTiles, authorityOwners, coords, stateOverrides, ownerOverrides)
	return states
}

// effectiveProjectionOverlay builds the sparse effective view used by both
// publication and private network submission. The returned owner overrides
// contain only IDs touched by pending operations; callers can fall back to the
// immutable authority owner index for all other IDs.
func effectiveProjectionOverlay(projection Projection, authorityTiles map[model.Coord]model.TileState, authorityOwners map[model.StableID]model.Coord, coords map[model.Coord]struct{}, stateOverrides map[model.Coord]model.TileState, ownerOverrides map[model.StableID]effectiveOwner) (map[model.Coord]model.TileState, map[model.StableID]effectiveOwner) {
	for _, operation := range projection.Pending {
		for _, change := range operation.Changes {
			coords[change.Coord] = struct{}{}
		}
	}
	states := make(map[model.Coord]model.TileState, len(coords))
	for coord := range coords {
		if state, exists := stateOverrides[coord]; exists {
			states[coord] = model.CloneTileState(state)
		} else if state, exists := authorityTiles[coord]; exists {
			states[coord] = model.CloneTileState(state)
		} else {
			states[coord] = model.TileState{}
		}
	}
	overrides := make(map[model.StableID]effectiveOwner, len(ownerOverrides))
	for id, override := range ownerOverrides {
		overrides[id] = override
	}
	owner := func(id model.StableID) (model.Coord, bool) {
		if override, exists := overrides[id]; exists {
			return override.coord, override.present
		}
		coord, exists := authorityOwners[id]
		return coord, exists
	}
	setOwner := func(id model.StableID, coord model.Coord, present bool) {
		overrides[id] = effectiveOwner{coord: coord, present: present}
	}
	for _, operation := range projection.Pending {
		affected := make(map[model.Coord]struct{}, len(operation.Changes))
		valid := true
		for _, change := range operation.Changes {
			if _, duplicate := affected[change.Coord]; duplicate {
				valid = false
				break
			}
			affected[change.Coord] = struct{}{}
			current := states[change.Coord]
			if !current.Equal(change.Before) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		afterIDs := make(map[model.StableID]struct{})
		for _, change := range operation.Changes {
			for _, prefab := range change.After.Prefabs {
				if _, duplicate := afterIDs[prefab.StableID]; duplicate {
					valid = false
					break
				}
				afterIDs[prefab.StableID] = struct{}{}
				if ownerCoord, exists := owner(prefab.StableID); exists {
					if _, changingOwner := affected[ownerCoord]; !changingOwner {
						valid = false
						break
					}
				}
			}
			if !valid {
				break
			}
		}
		if !valid {
			continue
		}
		for _, change := range operation.Changes {
			current := states[change.Coord]
			for _, prefab := range current.Prefabs {
				setOwner(prefab.StableID, change.Coord, false)
			}
		}
		for _, change := range operation.Changes {
			after := model.CloneTileState(change.After)
			states[change.Coord] = after
			for _, prefab := range after.Prefabs {
				setOwner(prefab.StableID, change.Coord, true)
			}
		}
	}
	return states, overrides
}

type effectiveOwner struct {
	coord   model.Coord
	present bool
}

func (network *NetworkExecutor) beginPublicationLocked(previous, next Projection, authorityChanges []model.TileChange, explicitDisplay []model.Coord, forceDisplay bool) {
	authoritative := make(map[model.Coord]model.Tile, len(authorityChanges))
	for _, change := range authorityChanges {
		authoritative[change.Coord] = model.Tile{Coord: change.Coord, State: network.authorityTileState(change.Coord)}
	}

	coords := make(map[model.Coord]struct{}, len(authorityChanges)+len(explicitDisplay))
	for _, change := range authorityChanges {
		coords[change.Coord] = struct{}{}
	}
	for _, coord := range explicitDisplay {
		coords[coord] = struct{}{}
	}
	for _, operation := range previous.Pending {
		for _, change := range operation.Changes {
			coords[change.Coord] = struct{}{}
		}
	}
	for _, operation := range next.Pending {
		for _, change := range operation.Changes {
			coords[change.Coord] = struct{}{}
		}
	}
	beforeStateOverrides := make(map[model.Coord]model.TileState, len(authorityChanges))
	beforeOwnerOverrides := make(map[model.StableID]effectiveOwner)
	beforeIDs := make(map[model.StableID]model.Coord)
	for _, change := range authorityChanges {
		beforeStateOverrides[change.Coord] = model.CloneTileState(change.Before)
		for _, prefab := range change.Before.Prefabs {
			beforeIDs[prefab.StableID] = change.Coord
			beforeOwnerOverrides[prefab.StableID] = effectiveOwner{coord: change.Coord, present: true}
		}
	}
	for _, change := range authorityChanges {
		for _, prefab := range change.After.Prefabs {
			if beforeCoord, existed := beforeIDs[prefab.StableID]; existed {
				beforeOwnerOverrides[prefab.StableID] = effectiveOwner{coord: beforeCoord, present: true}
			} else {
				beforeOwnerOverrides[prefab.StableID] = effectiveOwner{present: false}
			}
		}
	}
	beforeStates := effectiveProjectionStates(previous, network.authorityTiles, network.authorityOwners, coords, beforeStateOverrides, beforeOwnerOverrides)
	afterStates := effectiveProjectionStates(next, network.authorityTiles, network.authorityOwners, coords, nil, nil)
	display := make(map[model.Coord]model.Tile, len(coords))
	for coord := range coords {
		before := beforeStates[coord]
		after := afterStates[coord]
		if forceDisplay || !before.Equal(after) {
			display[coord] = model.Tile{Coord: coord, State: model.CloneTileState(after)}
		}
	}

	// Reading the replacement flag is a short publication critical section;
	// materializing the full fallback snapshot happens before reacquiring it.
	network.publication.mu.Lock()
	hasReplacement := network.publication.replacement != nil
	network.publication.mu.Unlock()
	var replacement *model.Snapshot
	if hasReplacement {
		updated := model.CloneSnapshot(next.Acknowledged)
		replacement = &updated
	}

	network.publication.mu.Lock()
	network.publication.sequence++
	network.publication.ready = true
	for coord, tile := range authoritative {
		network.publication.authoritative[coord] = tile
	}
	for coord, tile := range display {
		network.publication.display[coord] = tile
	}
	if replacement != nil && network.publication.replacement != nil {
		// A fallback is a coherent baseline. If more accepted data arrives before
		// the UI consumes it, keep that baseline at the newest acknowledged state.
		network.publication.replacement = replacement
	}
	network.publishCaptureLocked()
	network.publication.mu.Unlock()
	// Legacy consumers explicitly opt in through ProjectionUpdates. Keep their
	// compatibility clone out of the publication critical section.
	network.publishChangesLocked(authorityChanges)
	network.publishLegacyLocked()
}

func (network *NetworkExecutor) beginMetadataPublicationLocked() {
	network.publication.mu.Lock()
	network.publication.sequence++
	network.publication.ready = true
	network.publishCaptureLocked()
	network.publication.mu.Unlock()
}

func (network *NetworkExecutor) beginReplacementPublicationLocked(snapshot model.Snapshot) {
	replacement := model.CloneSnapshot(snapshot)
	network.publication.mu.Lock()
	network.publication.sequence++
	network.publication.ready = true
	network.publication.authoritative = make(map[model.Coord]model.Tile)
	network.publication.display = make(map[model.Coord]model.Tile)
	network.publication.replacement = &replacement
	network.publishCaptureLocked()
	network.publication.mu.Unlock()
	network.publishChangesLocked(nil)
	network.publishLegacyLocked()
}

func (network *NetworkExecutor) refuseAdmission(operation model.Operation, cause error) {
	coords := make([]model.Coord, 0, len(operation.Changes))
	for _, change := range operation.Changes {
		coords = append(coords, change.Coord)
	}
	network.mutex.Lock()
	network.retainUnsentLocked(operation, cause)
	previous := network.projection
	network.beginPublicationLocked(previous, previous, nil, coords, true)
	network.mutex.Unlock()
}

func (network *NetworkExecutor) refuseAdmitted(operation model.Operation, cause error, ticket, bytes uint64) {
	coords := make([]model.Coord, 0, len(operation.Changes))
	for _, change := range operation.Changes {
		coords = append(coords, change.Coord)
	}
	network.mutex.Lock()
	network.publication.mu.Lock()
	alreadyRecovered := ticket < network.publication.interruptedBefore
	network.publication.mu.Unlock()
	// Interruption already retained this ticket. A late callback must not
	// resurrect a draft the user discarded or accepted replay resolved.
	if !alreadyRecovered {
		network.retainUnsentLocked(operation, cause)
	}
	previous := network.projection
	network.beginPublicationLocked(previous, previous, nil, coords, true)
	network.mutex.Unlock()
	network.finishAdmission(operation.OperationID, ticket, bytes, false)
}

func (network *NetworkExecutor) reserveAdmission(operation model.Operation) (uint64, uint64, error) {
	bytes := operationAdmissionBytes(operation)
	network.publication.mu.Lock()
	defer network.publication.mu.Unlock()
	if network.publication.admissionErr != nil {
		return 0, 0, network.publication.admissionErr
	}
	usedCount := network.publication.localAdmissions + len(network.publication.pendingAdmission)
	usedBytes := network.publication.localAdmissionBytes + network.publication.pendingAdmissionBytes
	if usedCount >= maxLocalAdmissionCount || bytes > maxLocalAdmissionBytes-usedBytes {
		return 0, 0, errLocalAdmissionFull
	}
	ticket := network.publication.nextDispatch
	network.publication.nextDispatch++
	network.publication.localAdmissions++
	network.publication.localAdmissionBytes += bytes
	// prepareOperation detached the payload before admission. Own it until
	// handoff or interruption recovers edits still queued locally.
	network.publication.queuedAdmission[ticket] = operation
	// Publish the pending/admission bit without touching network.projection or
	// waiting for its reconciliation mutex. The projection pointer remains the
	// prior immutable view until the ordered owner installs this operation.
	network.publishAdmissionMetadataLocked()
	return ticket, bytes, nil
}

func (network *NetworkExecutor) waitDispatchTurn(ticket uint64) {
	for {
		network.publication.mu.Lock()
		if ticket == network.publication.dispatchTurn {
			network.publication.mu.Unlock()
			return
		}
		wake := network.publication.dispatchWake
		network.publication.mu.Unlock()
		<-wake
	}
}

func (network *NetworkExecutor) finishAdmission(operationID model.OperationID, ticket, bytes uint64, sent bool) {
	network.mutex.Lock()
	if sent {
		_, sentPending := network.pending[operationID]
		sent = sentPending
	}
	network.publication.mu.Lock()
	delete(network.publication.queuedAdmission, ticket)
	if network.publication.localAdmissions > 0 {
		network.publication.localAdmissions--
	}
	if bytes <= network.publication.localAdmissionBytes {
		network.publication.localAdmissionBytes -= bytes
	} else {
		network.publication.localAdmissionBytes = 0
	}
	if sent {
		network.publication.pendingAdmission[operationID] = bytes
		network.publication.pendingAdmissionBytes += bytes
	}
	if ticket == network.publication.dispatchTurn {
		network.publication.dispatchTurn++
		close(network.publication.dispatchWake)
		network.publication.dispatchWake = make(chan struct{})
	}
	network.publishCaptureLocked()
	network.publication.mu.Unlock()
	network.mutex.Unlock()
}

func (network *NetworkExecutor) releasePendingAdmission(operationID model.OperationID) {
	network.publication.mu.Lock()
	if bytes, exists := network.publication.pendingAdmission[operationID]; exists {
		delete(network.publication.pendingAdmission, operationID)
		if bytes <= network.publication.pendingAdmissionBytes {
			network.publication.pendingAdmissionBytes -= bytes
		} else {
			network.publication.pendingAdmissionBytes = 0
		}
	}
	network.publication.mu.Unlock()
}
