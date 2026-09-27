package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

var (
	ErrOperationRejected  = errors.New("collaboration operation was rejected")
	ErrExecutorTerminated = errors.New("collaboration network executor was terminated")
)

const conflictDeliveryUnconfirmed = "delivery_unconfirmed"

type operationResult struct {
	accepted model.AcceptedOperation
	err      error
}

// ProjectionCapture pins one immutable executor projection. NetworkExecutor
// replaces projections instead of mutating them, so the capture can be
// materialized by a worker after the executor advances.
type ProjectionCapture struct {
	projection Projection
	mapHash    string
	hasPending bool
}

func (capture ProjectionCapture) HasPending() bool {
	return capture.hasPending || len(capture.projection.Pending) != 0
}

func (capture ProjectionCapture) BaseRevision() model.Revision {
	return capture.projection.Acknowledged.Revision
}

func (capture ProjectionCapture) DocumentID() model.DocumentID {
	return capture.projection.Acknowledged.DocumentID
}

// AcceptedSnapshot materializes only the pinned acknowledged revision. Pending
// view data is never included in source context or persistence.
func (capture ProjectionCapture) AcceptedSnapshot() model.Snapshot {
	return model.CloneSnapshot(capture.projection.Acknowledged)
}

// EstimatedBytes conservatively covers materializing one visible snapshot,
// the projection indexes, and pending change states. It allocates nothing.
func (capture ProjectionCapture) EstimatedBytes() uint64 {
	bytes := uint64(1 << 20)
	for _, tile := range capture.projection.Acknowledged.Tiles {
		bytes = estimateCaptureAdd(bytes, estimateCaptureState(tile.State)+64)
	}
	for _, operation := range capture.projection.Pending {
		bytes = estimateCaptureAdd(bytes, uint64(len(operation.Changes))*128)
		for _, change := range operation.Changes {
			bytes = estimateCaptureAdd(bytes, estimateCaptureState(change.After))
		}
	}
	return estimateCaptureMul(bytes, 2)
}

func estimateCaptureState(state model.TileState) uint64 {
	bytes := uint64(96)
	for _, prefab := range state.Prefabs {
		bytes = estimateCaptureAdd(bytes, 128+uint64(len(prefab.Path))+uint64(len(prefab.StableID)))
		for name, value := range prefab.Vars {
			bytes = estimateCaptureAdd(bytes, 64+uint64(len(name))+uint64(len(value)))
		}
	}
	return bytes
}

func estimateCaptureAdd(a, b uint64) uint64 {
	if b > ^uint64(0)-a {
		return ^uint64(0)
	}
	return a + b
}

func estimateCaptureMul(value, factor uint64) uint64 {
	if factor != 0 && value > ^uint64(0)/factor {
		return ^uint64(0)
	}
	return value * factor
}

// VisibleSnapshot deep-copies the captured optimistic projection. Call it from
// a worker when the snapshot may contain a large document.
func (capture ProjectionCapture) VisibleSnapshot() (model.Snapshot, error) {
	return capture.projection.Visible()
}

// VisibleTiles detaches only the requested footprint from this immutable
// capture. Pending operations must still be reconciled as complete batches:
// filtering them first could expose half of a conflicting move.
func (capture ProjectionCapture) VisibleTiles(contains func(model.Coord) bool) (map[model.Coord]model.TileState, error) {
	snapshot := capture.projection.Acknowledged
	if len(capture.projection.Pending) != 0 {
		var err error
		snapshot, err = capture.VisibleSnapshot()
		if err != nil {
			return nil, err
		}
	}
	tiles := make(map[model.Coord]model.TileState)
	for _, tile := range snapshot.Tiles {
		if contains(tile.Coord) {
			tiles[tile.Coord] = model.CloneTileState(tile.State)
		}
	}
	return tiles, nil
}

// EstimatedVisibleTilesBytes covers the detached footprint and lookup index.
// Pending batches require the full projection scratch in addition to that copy.
func (capture ProjectionCapture) EstimatedVisibleTilesBytes(contains func(model.Coord) bool) uint64 {
	if len(capture.projection.Pending) != 0 {
		return estimateCaptureMul(capture.EstimatedBytes(), 2)
	}
	bytes := uint64(1 << 20)
	for _, tile := range capture.projection.Acknowledged.Tiles {
		if contains(tile.Coord) {
			bytes = estimateCaptureAdd(bytes, estimateCaptureState(tile.State)+128)
		}
	}
	return estimateCaptureMul(bytes, 2)
}

// OperationBase returns the metadata needed to build an operation against the
// captured acknowledged revision without exposing executor-owned tile data.
func (capture ProjectionCapture) OperationBase() (model.DocumentID, model.Revision, string, string, error) {
	snapshot := capture.projection.Acknowledged
	if capture.mapHash == "" {
		return "", 0, "", "", errors.New("locally verified acknowledged map hash is unavailable")
	}
	return snapshot.DocumentID, snapshot.Revision, snapshot.EnvironmentHash, capture.mapHash, nil
}

type NetworkExecutor struct {
	transport Transport
	actor     model.ActorID
	sessionID string

	mutex            sync.Mutex
	projection       Projection
	pending          map[model.OperationID]chan operationResult
	accepted         map[model.OperationID]model.AcceptedOperation
	acceptedHashes   map[model.OperationID]string
	acceptedReceipts map[model.OperationID][sha256.Size]byte
	verifiedHistory  map[model.Revision]string
	conflicts        []Conflict
	conflictsDirty   bool
	updates          chan Projection
	legacyUpdates    bool
	terminal         error
	suspended        error

	// mutex protects state transitions and legacy compatibility reads. The
	// publication/capture paths use the immutable pointer below instead of
	// waiting behind reconciliation or canonical hashing.
	publication     publicationState
	published       atomic.Pointer[publishedProjection]
	authorityTiles  map[model.Coord]model.TileState
	authorityOwners map[model.StableID]model.Coord
}

func NewNetworkExecutor(transport Transport, snapshot model.Snapshot, actor model.ActorID, sessionID string) (*NetworkExecutor, error) {
	if transport == nil {
		return nil, fmt.Errorf("network executor transport is nil")
	}
	snapshotHash, err := snapshot.Hash()
	if err != nil {
		return nil, err
	}
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("network executor session id is empty")
	}
	projection := newProjection(snapshot, snapshotHash)
	network := &NetworkExecutor{
		transport:        transport,
		actor:            actor,
		sessionID:        sessionID,
		projection:       projection,
		pending:          make(map[model.OperationID]chan operationResult),
		accepted:         make(map[model.OperationID]model.AcceptedOperation),
		acceptedHashes:   make(map[model.OperationID]string),
		acceptedReceipts: make(map[model.OperationID][sha256.Size]byte),
		verifiedHistory:  map[model.Revision]string{snapshot.Revision: snapshotHash},
		updates:          make(chan Projection, 1),
		publication:      newPublicationState(),
	}
	network.authorityTiles, network.authorityOwners = buildAuthorityIndex(snapshot)
	network.publication.mu.Lock()
	network.publishCaptureLocked()
	network.publication.mu.Unlock()
	return network, nil
}

func (network *NetworkExecutor) Execute(ctx context.Context, operation model.Operation) (model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}
	operation, err := network.prepareOperation(operation)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	ticket, bytes, err := network.reserveAdmission(operation)
	if err != nil {
		network.refuseAdmission(operation, err)
		return model.AcceptedOperation{}, err
	}
	network.waitDispatchTurn(ticket)
	accepted, err := network.executeAdmitted(ctx, operation, ticket, bytes, false)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	return accepted, nil
}

func (network *NetworkExecutor) prepareOperation(operation model.Operation) (model.Operation, error) {
	// ExecuteAsync may wait behind an earlier dispatch turn. Detach every
	// nested tile/prefab value before returning to the caller so that mutating a
	// draft after admission cannot alter the queued wire operation or conflict
	// recovery state.
	operation = model.CloneOperation(operation)
	operation.ActorID = network.actor
	if operation.OperationID == "" {
		operationID, err := model.NewOperationID()
		if err != nil {
			return model.Operation{}, err
		}
		operation.OperationID = operationID
	}
	return operation, nil
}

func (network *NetworkExecutor) executeAdmitted(ctx context.Context, operation model.Operation, ticket, bytes uint64, async bool) (model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		network.refuseAdmitted(operation, err, ticket, bytes)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}

	network.mutex.Lock()
	if network.terminal != nil {
		err := network.terminal
		network.mutex.Unlock()
		network.refuseAdmitted(operation, err, ticket, bytes)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}
	if network.suspended != nil {
		err := network.suspended
		network.mutex.Unlock()
		network.refuseAdmitted(operation, err, ticket, bytes)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}
	network.publication.mu.Lock()
	interrupted := ticket < network.publication.interruptedBefore
	network.publication.mu.Unlock()
	if interrupted {
		err := fmt.Errorf("connection interrupted before queued edit was submitted; the edit is retained for recovery")
		network.mutex.Unlock()
		network.refuseAdmitted(operation, err, ticket, bytes)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}
	previous := network.projection
	verifiedHash, hashErr := network.projection.verifiedMapHash()
	var projection Projection
	var err error
	if operation.BaseRevision > network.projection.Acknowledged.Revision || network.verifiedHistory[operation.BaseRevision] != operation.BaseMapHash {
		err = fmt.Errorf("operation base does not match a locally verified acknowledged revision")
	} else {
		projection, err = network.projection.submitWithVerifiedOverlay(operation, verifiedHash, hashErr, network.authorityTiles, network.authorityOwners)
	}
	if err != nil {
		network.retainUnsentLocked(operation, err)
		network.mutex.Unlock()
		network.refuseAdmitted(operation, err, ticket, bytes)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}
	result := make(chan operationResult, 1)
	network.projection = projection
	network.pending[operation.OperationID] = result
	transport := network.transport
	network.beginPublicationLocked(previous, projection, nil, nil, false)
	network.mutex.Unlock()

	if sender, ok := transport.(interface {
		SendOperation(context.Context, string, model.Operation) error
	}); ok {
		err = sender.SendOperation(ctx, network.sessionID, operation)
	} else {
		var payload []byte
		payload, err = json.Marshal(protocol.OperationSubmitPayload{Operation: operation})
		if err == nil {
			err = transport.Send(ctx, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(operation.OperationID), SessionID: network.sessionID, Type: protocol.ClientOperationSubmit, Payload: payload})
		}
	}
	if err != nil {
		network.failPending(operation.OperationID, err)
		network.finishAdmission(operation.OperationID, ticket, bytes, false)
		if !async {
			network.recordCompletion(ticket, model.AcceptedOperation{}, err, nil)
		}
		return model.AcceptedOperation{}, err
	}
	// The pending projection now contains every admitted edit through this
	// dispatch turn. Release admission before waiting for acknowledgement so a
	// later gesture can be submitted without serializing on the server RTT.
	network.finishAdmission(operation.OperationID, ticket, bytes, true)
	if !async {
		network.recordCompletion(ticket, model.AcceptedOperation{}, nil, nil)
	}
	select {
	case resolved := <-result:
		return resolved.accepted, resolved.err
	case <-ctx.Done():
		return model.AcceptedOperation{}, ctx.Err()
	}
}

func (network *NetworkExecutor) ExecuteAsync(ctx context.Context, operation model.Operation, complete func(model.AcceptedOperation, error)) error {
	if complete == nil {
		return fmt.Errorf("network executor completion callback is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	operation, err := network.prepareOperation(operation)
	if err != nil {
		return err
	}
	ticket, bytes, err := network.reserveAdmission(operation)
	if err != nil {
		network.refuseAdmission(operation, err)
		return err
	}
	go func() {
		network.waitDispatchTurn(ticket)
		accepted, executeErr := network.executeAdmitted(ctx, operation, ticket, bytes, true)
		network.recordCompletion(ticket, accepted, executeErr, complete)
	}()
	return nil
}

func (network *NetworkExecutor) BuildInverse(ctx context.Context, targetID model.OperationID) (model.Operation, error) {
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	target, exists := network.accepted[targetID]
	if !exists {
		return model.Operation{}, fmt.Errorf("accepted operation %q is not retained", targetID)
	}
	if target.ActorID != network.actor {
		return model.Operation{}, fmt.Errorf("accepted operation belongs to actor %q", target.ActorID)
	}
	if target.Kind == model.OperationKindInverse {
		return model.Operation{}, fmt.Errorf("inverse operations are redone as new forward operations")
	}
	operationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, err
	}
	baseHash, err := network.projection.verifiedMapHash()
	if err != nil {
		return model.Operation{}, err
	}
	changes := make([]model.TileChange, len(target.Changes))
	for index, targetChange := range target.Changes {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, err
		}
		current := network.authorityTiles[targetChange.Coord]
		if !current.Equal(targetChange.After) {
			return model.Operation{}, fmt.Errorf("tile (%d,%d,%d) was changed by a later edit; undo would overwrite newer work. Undo the conflicting edit first, then retry. Your undo history is preserved", targetChange.Coord.X, targetChange.Coord.Y, targetChange.Coord.Z)
		}
		changes[index] = model.TileChange{Coord: targetChange.Coord, Before: model.CloneTileState(targetChange.After), After: model.CloneTileState(targetChange.Before)}
	}
	inverseOf := targetID
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: network.projection.Acknowledged.DocumentID, ActorID: network.actor, OperationID: operationID, BaseRevision: network.projection.Acknowledged.Revision, EnvironmentHash: network.projection.Acknowledged.EnvironmentHash, BaseMapHash: baseHash, Kind: model.OperationKindInverse, Changes: changes, InverseOf: &inverseOf}, nil
}

func (network *NetworkExecutor) Snapshot(ctx context.Context) (model.Snapshot, error) {
	capture, err := network.CaptureProjection(ctx)
	if err != nil {
		return model.Snapshot{}, err
	}
	// Pin a coherent verified revision, then copy outside reconciliation. Public
	// callers still own detached data even while the executor advances.
	return capture.AcceptedSnapshot(), nil
}

// CaptureProjection pins the current acknowledged and speculative views with
// an O(1) copy. Use the returned capture to read a stable view off the UI
// thread; the captured slices are immutable because executor updates replace
// the projection value and its backing storage.
func (network *NetworkExecutor) CaptureProjection(ctx context.Context) (ProjectionCapture, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionCapture{}, err
	}
	published := network.published.Load()
	if published == nil {
		return ProjectionCapture{}, ErrExecutorTerminated
	}
	return ProjectionCapture{projection: published.projection, mapHash: published.mapHash, hasPending: published.hasPending}, nil
}

func (network *NetworkExecutor) ProjectionUpdates() <-chan Projection {
	network.mutex.Lock()
	network.legacyUpdates = true
	network.mutex.Unlock()
	return network.updates
}

func (network *NetworkExecutor) HasUnacknowledgedOperations() bool {
	published := network.published.Load()
	return published != nil && published.hasPending
}

func (network *NetworkExecutor) Conflicts() []Conflict {
	published := network.published.Load()
	if published == nil || len(published.conflicts) == 0 {
		return nil
	}
	conflicts := make([]Conflict, len(published.conflicts))
	for index, conflict := range published.conflicts {
		conflicts[index] = cloneConflict(conflict)
	}
	return conflicts
}

// Suspend rolls back speculation and retains unacknowledged intent for explicit
// recovery. A queued operation may already be durable; never resend it here.
func (network *NetworkExecutor) Suspend(cause error) {
	if cause == nil {
		cause = ErrTransportNotConnected
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	network.suspendLocked(cause)
}

func (network *NetworkExecutor) suspendLocked(cause error) {
	if network.terminal != nil || network.suspended != nil {
		return
	}
	network.suspended = cause
	network.clearPendingLocked(cause)
}

// Preserve uncertain delivery on both recoverable and terminal failures. The
// acknowledged snapshot remains available for inspection and local export.
func (network *NetworkExecutor) clearPendingLocked(cause error) {
	// Admission runs separately from reconciliation. Close it and pin every
	// pre-interruption ticket before collecting drafts; Resume must not send
	// queued gestures automatically through a new connection.
	network.publication.mu.Lock()
	network.publication.admissionErr = cause
	network.publication.interruptedBefore = network.publication.nextDispatch
	tickets := make([]uint64, 0, len(network.publication.queuedAdmission))
	for ticket := range network.publication.queuedAdmission {
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(i, j int) bool { return tickets[i] < tickets[j] })
	queued := make([]model.Operation, 0, len(tickets))
	for _, ticket := range tickets {
		queued = append(queued, network.publication.queuedAdmission[ticket])
	}
	// Recovery consumes this ownership once. A second interruption (including
	// terminal cleanup after suspension) must not recreate discarded drafts.
	clear(network.publication.queuedAdmission)
	network.publication.mu.Unlock()
	snapshot := network.projection.Acknowledged
	if hash, err := network.projection.verifiedMapHash(); err == nil {
		for _, operation := range network.projection.Pending {
			network.retainConflictLocked(Conflict{
				OperationID: operation.OperationID, Draft: operation,
				Code:     conflictDeliveryUnconfirmed,
				Message:  "Delivery ended before acknowledgement. This edit may already have been applied; inspect current authority before deciding whether to rebuild it.",
				Revision: snapshot.Revision, MapHash: hash,
			})
		}
	}
	for _, operation := range queued {
		// Pending operations were retained above with uncertain-delivery status.
		// Already acknowledged operations must not become recovery drafts if
		// acceptance beat the sending worker's admission cleanup.
		network.retainUnsentLocked(operation, cause)
	}
	waiters := make([]chan operationResult, 0, len(network.pending))
	for operationID, waiter := range network.pending {
		delete(network.pending, operationID)
		network.releasePendingAdmission(operationID)
		waiters = append(waiters, waiter)
	}
	previous := network.projection
	// Dropping speculation does not change immutable executor-owned authority.
	network.projection = Projection{Acknowledged: previous.Acknowledged, acknowledgedHash: previous.acknowledgedHash}
	network.beginPublicationLocked(previous, network.projection, nil, nil, false)
	for _, waiter := range waiters {
		waiter <- operationResult{err: cause}
	}
}

// Resume binds a fresh transport while preserving acknowledged state and accepted history.
func (network *NetworkExecutor) Resume(transport Transport) error {
	if transport == nil {
		return fmt.Errorf("network executor transport is nil")
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if network.terminal != nil {
		return network.terminal
	}
	if network.suspended == nil {
		return fmt.Errorf("network executor is not suspended")
	}
	network.transport = transport
	network.suspended = nil
	network.publication.mu.Lock()
	network.publication.admissionErr = nil
	network.publication.mu.Unlock()
	return nil
}

// ReplaceAcknowledgedSnapshot installs a newer authoritative baseline during reconnect fallback.
func (network *NetworkExecutor) ReplaceAcknowledgedSnapshot(ctx context.Context, snapshot model.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	replacementHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if network.terminal != nil {
		return network.terminal
	}
	if len(network.pending) != 0 || len(network.projection.Pending) != 0 {
		return fmt.Errorf("cannot replace acknowledged snapshot while operations are pending")
	}
	current := network.projection.Acknowledged
	if snapshot.DocumentID != current.DocumentID || snapshot.EnvironmentHash != current.EnvironmentHash {
		return fmt.Errorf("replacement snapshot is incompatible with acknowledged document")
	}
	if snapshot.Revision < current.Revision {
		return fmt.Errorf("replacement snapshot revision %d precedes acknowledged revision %d", snapshot.Revision, current.Revision)
	}
	if snapshot.Revision == current.Revision {
		currentHash, err := network.projection.verifiedMapHash()
		if err != nil {
			return err
		}
		if replacementHash != currentHash {
			return fmt.Errorf("replacement snapshot conflicts with acknowledged revision %d", current.Revision)
		}
		return nil
	}
	network.projection = newProjection(snapshot, replacementHash)
	network.replaceAuthorityIndex(snapshot)
	network.verifiedHistory = map[model.Revision]string{snapshot.Revision: replacementHash}
	// Snapshot fallback carries no operation IDs. Keep unresolved drafts; their
	// rebuild path compares intended values with this fresh authority instead.
	network.beginReplacementPublicationLocked(snapshot)
	return nil
}

// RefreshConflict pins authority for recovery inspection without copying the map.
// Call AcceptedSnapshot on the capture only when detached map data is needed.
func (network *NetworkExecutor) RefreshConflict(ctx context.Context, operationID model.OperationID) (ProjectionCapture, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionCapture{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if _, exists := network.conflictLocked(operationID); !exists {
		return ProjectionCapture{}, fmt.Errorf("conflict for operation %q is not retained", operationID)
	}
	return network.CaptureProjection(ctx)
}

// DiscardConflict removes only the retained draft and returns its pinned authority.
func (network *NetworkExecutor) DiscardConflict(ctx context.Context, operationID model.OperationID) (ProjectionCapture, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionCapture{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	index, exists := network.conflictLocked(operationID)
	if !exists {
		return ProjectionCapture{}, fmt.Errorf("conflict for operation %q is not retained", operationID)
	}
	capture, err := network.CaptureProjection(ctx)
	if err != nil {
		return ProjectionCapture{}, err
	}
	network.conflicts = slices.Delete(network.conflicts, index, index+1)
	network.conflictsDirty = true
	network.beginMetadataPublicationLocked()
	return capture, nil
}

func (network *NetworkExecutor) BuildConflictRebuild(ctx context.Context, operationID model.OperationID) (model.Operation, error) {
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	index, exists := network.conflictLocked(operationID)
	if !exists {
		return model.Operation{}, fmt.Errorf("conflict for operation %q is not retained", operationID)
	}
	if len(network.projection.Pending) != 0 {
		return model.Operation{}, fmt.Errorf("cannot rebuild conflict while operations are pending")
	}
	conflict := network.conflicts[index]
	newOperationID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, err
	}
	baseHash, err := network.projection.verifiedMapHash()
	if err != nil {
		return model.Operation{}, err
	}
	changes := make([]model.TileChange, 0, len(conflict.Draft.Changes))
	for _, draftChange := range conflict.Draft.Changes {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, err
		}
		current := network.authorityTiles[draftChange.Coord]
		if current.Equal(draftChange.After) {
			continue
		}
		changes = append(changes, model.TileChange{
			Coord:  draftChange.Coord,
			Before: model.CloneTileState(current),
			After:  model.CloneTileState(draftChange.After),
		})
	}
	if len(changes) == 0 {
		return model.Operation{}, fmt.Errorf("rejected intent is already present in the authoritative document")
	}
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      network.projection.Acknowledged.DocumentID,
		ActorID:         network.actor,
		OperationID:     newOperationID,
		BaseRevision:    network.projection.Acknowledged.Revision,
		EnvironmentHash: network.projection.Acknowledged.EnvironmentHash,
		BaseMapHash:     baseHash,
		Kind:            model.OperationKindTileChange,
		Changes:         changes,
	}, nil
}

func (network *NetworkExecutor) DismissConflict(operationID model.OperationID) bool {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	index, exists := network.conflictLocked(operationID)
	if !exists {
		return false
	}
	network.conflicts = slices.Delete(network.conflicts, index, index+1)
	network.conflictsDirty = true
	network.beginMetadataPublicationLocked()
	return true
}

// Terminate releases pending operations and prevents further executor use.
func (network *NetworkExecutor) Terminate(cause error) {
	if cause == nil {
		cause = ErrExecutorTerminated
	}
	network.failAll(cause)
}

func (network *NetworkExecutor) Receive(envelope protocol.ServerEnvelope) error {
	decoded, err := protocol.DecodeServerEnvelope(envelope)
	if err != nil {
		decodeErr := fmt.Errorf("decode network executor message: %w", err)
		network.failAll(decodeErr)
		return decodeErr
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if network.terminal != nil {
		return network.terminal
	}
	switch decoded.Envelope.Type {
	case protocol.ServerOperationAccepted:
		payload := decoded.Payload.(*protocol.OperationAcceptedPayload)
		// Only this actor's forward operations can be undone. Keep compact
		// verified receipts for remote edits and inverses, rather than
		// retaining every whole-area before/after payload for the session.
		retain := payload.Operation.ActorID == network.actor && payload.Operation.Kind != model.OperationKindInverse
		var receipt [sha256.Size]byte
		if !retain {
			digest := sha256.New()
			if err := json.NewEncoder(digest).Encode(payload); err != nil {
				return fmt.Errorf("fingerprint accepted operation: %w", err)
			}
			copy(receipt[:], digest.Sum(nil))
			if prior, exists := network.acceptedReceipts[payload.Operation.OperationID]; exists && prior == receipt {
				return nil
			}
		}
		if prior, exists := network.accepted[payload.Operation.OperationID]; exists && reflect.DeepEqual(prior, payload.Operation) {
			if payload.MapHash == network.acceptedHashes[payload.Operation.OperationID] {
				return nil
			}
		}
		previous := network.projection
		projection, applyErr := network.projection.acceptWithVerifiedCurrent(payload.Operation, payload.MapHash, nil)
		if applyErr != nil {
			network.suspendLocked(applyErr)
			return applyErr
		}
		network.applyAuthorityChanges(payload.Operation.Changes)
		network.projection = projection
		network.verifiedHistory[payload.Operation.Revision] = payload.MapHash
		if len(network.verifiedHistory) > 256 {
			oldest := payload.Operation.Revision
			for revision := range network.verifiedHistory {
				if revision < oldest {
					oldest = revision
				}
			}
			delete(network.verifiedHistory, oldest)
		}
		if retain {
			network.accepted[payload.Operation.OperationID] = model.CloneAcceptedOperation(payload.Operation)
			network.acceptedHashes[payload.Operation.OperationID] = payload.MapHash
		} else {
			network.acceptedReceipts[payload.Operation.OperationID] = receipt
		}
		if index, found := network.conflictLocked(payload.Operation.OperationID); found {
			conflict := network.conflicts[index]
			if conflict.Code == conflictDeliveryUnconfirmed && model.SameOperation(conflict.Draft, payload.Operation.Operation) {
				network.conflicts = slices.Delete(network.conflicts, index, index+1)
				network.conflictsDirty = true
			}
		}
		var waiter chan operationResult
		if pendingWaiter, exists := network.pending[payload.Operation.OperationID]; exists {
			delete(network.pending, payload.Operation.OperationID)
			network.releasePendingAdmission(payload.Operation.OperationID)
			waiter = pendingWaiter
		}
		network.beginPublicationLocked(previous, projection, payload.Operation.Changes, nil, false)
		if waiter != nil {
			waiter <- operationResult{accepted: model.CloneAcceptedOperation(payload.Operation)}
		}
	case protocol.ServerOperationRejected:
		payload := decoded.Payload.(*protocol.OperationRejectedPayload)
		previous := network.projection
		verifiedHash, hashErr := network.projection.verifiedMapHash()
		projection, conflict, rejectErr := network.projection.rejectWithVerifiedHash(*payload, verifiedHash, hashErr)
		if rejectErr != nil {
			network.suspendLocked(rejectErr)
			return rejectErr
		}
		network.projection = projection
		network.retainConflictLocked(conflict)
		var waiter chan operationResult
		if pendingWaiter, exists := network.pending[payload.OperationID]; exists {
			delete(network.pending, payload.OperationID)
			network.releasePendingAdmission(payload.OperationID)
			waiter = pendingWaiter
		}
		displayCoords := make([]model.Coord, 0, len(conflict.Draft.Changes))
		for _, change := range conflict.Draft.Changes {
			displayCoords = append(displayCoords, change.Coord)
		}
		network.beginPublicationLocked(previous, projection, nil, displayCoords, false)
		if waiter != nil {
			waiter <- operationResult{err: fmt.Errorf("%w: %s: %s", ErrOperationRejected, conflict.Code, conflict.Message)}
		}
	}
	return nil
}

func cloneConflict(conflict Conflict) Conflict {
	conflict.Draft = model.CloneOperation(conflict.Draft)
	conflict.AuthoritativeValues = cloneTiles(conflict.AuthoritativeValues)
	return conflict
}

func (network *NetworkExecutor) conflictLocked(operationID model.OperationID) (int, bool) {
	for index, conflict := range network.conflicts {
		if conflict.OperationID == operationID {
			return index, true
		}
	}
	return 0, false
}

// Local validation and transport queue failures never receive a server rejection.
// Keep their intent in the same explicit refresh/discard/rebuild flow instead of
// losing it when the editor restores acknowledged state.
func (network *NetworkExecutor) retainUnsentLocked(operation model.Operation, cause error) {
	if _, accepted := network.accepted[operation.OperationID]; accepted {
		return
	}
	if _, accepted := network.acceptedReceipts[operation.OperationID]; accepted {
		return
	}
	snapshot := network.projection.Acknowledged
	if operation.ProtocolVersion != snapshot.ProtocolVersion || operation.DocumentID != snapshot.DocumentID || operation.EnvironmentHash != snapshot.EnvironmentHash || operation.OperationID.Validate() != nil {
		return // A foreign document must never become rebuildable in this one.
	}
	hash, err := network.projection.verifiedMapHash()
	if err != nil {
		return
	}
	network.retainConflictLocked(Conflict{
		OperationID: operation.OperationID,
		Draft:       operation,
		Code:        "submission_failed",
		Message:     cause.Error(),
		Revision:    snapshot.Revision,
		MapHash:     hash,
	})
}

func (network *NetworkExecutor) retainConflictLocked(conflict Conflict) {
	if _, exists := network.conflictLocked(conflict.OperationID); exists {
		return
	}
	network.conflicts = append(network.conflicts, cloneConflict(conflict))
	network.conflictsDirty = true
	// These drafts may be the only remaining copy of user intent. Keep them
	// until explicitly resolved; UI preview limits must never evict recovery
	// data, including a burst of pending operations cleared on disconnect.
}

func (network *NetworkExecutor) failPending(operationID model.OperationID, cause error) {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if waiter, exists := network.pending[operationID]; exists {
		delete(network.pending, operationID)
		network.releasePendingAdmission(operationID)
		previous := network.projection
		remaining := make([]model.Operation, 0, len(network.projection.Pending)-1)
		for _, operation := range network.projection.Pending {
			if operation.OperationID != operationID {
				remaining = append(remaining, model.CloneOperation(operation))
			} else {
				network.retainUnsentLocked(operation, cause)
			}
		}
		network.projection = Projection{Acknowledged: previous.Acknowledged, Pending: remaining, acknowledgedHash: previous.acknowledgedHash}
		network.beginPublicationLocked(previous, network.projection, nil, nil, false)
		waiter <- operationResult{err: cause}
	}
}

func (network *NetworkExecutor) failAll(cause error) {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	network.failAllLocked(cause)
}

func (network *NetworkExecutor) failAllLocked(cause error) {
	if network.terminal != nil {
		return
	}
	network.terminal = cause
	network.clearPendingLocked(cause)
}

func (network *NetworkExecutor) publishLegacyLocked() {
	if !network.legacyUpdates {
		return
	}
	update := cloneProjection(network.projection)
	select {
	case network.updates <- update:
	default:
		select {
		case <-network.updates:
		default:
		}
		select {
		case network.updates <- update:
		default:
		}
	}
}
