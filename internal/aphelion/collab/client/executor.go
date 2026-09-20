package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

var (
	ErrOperationRejected  = errors.New("collaboration operation was rejected")
	ErrExecutorTerminated = errors.New("collaboration network executor was terminated")
)

const (
	maxRetainedConflicts        = 100
	conflictDeliveryUnconfirmed = "delivery_unconfirmed"
)

type operationResult struct {
	accepted model.AcceptedOperation
	err      error
}

type NetworkExecutor struct {
	transport Transport
	actor     model.ActorID
	sessionID string

	mutex          sync.Mutex
	projection     Projection
	pending        map[model.OperationID]chan operationResult
	accepted       map[model.OperationID]model.AcceptedOperation
	acceptedHashes map[model.OperationID]string
	conflicts      []Conflict
	updates        chan Projection
	terminal       error
	suspended      error
}

func NewNetworkExecutor(transport Transport, snapshot model.Snapshot, actor model.ActorID, sessionID string) (*NetworkExecutor, error) {
	if transport == nil {
		return nil, fmt.Errorf("network executor transport is nil")
	}
	if _, err := snapshot.Hash(); err != nil {
		return nil, err
	}
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("network executor session id is empty")
	}
	return &NetworkExecutor{
		transport:      transport,
		actor:          actor,
		sessionID:      sessionID,
		projection:     NewProjection(snapshot),
		pending:        make(map[model.OperationID]chan operationResult),
		accepted:       make(map[model.OperationID]model.AcceptedOperation),
		acceptedHashes: make(map[model.OperationID]string),
		updates:        make(chan Projection, 1),
	}, nil
}

func (network *NetworkExecutor) Execute(ctx context.Context, operation model.Operation) (model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}
	operation.ActorID = network.actor
	if operation.OperationID == "" {
		operationID, err := model.NewOperationID()
		if err != nil {
			return model.AcceptedOperation{}, err
		}
		operation.OperationID = operationID
	}

	network.mutex.Lock()
	if network.terminal != nil {
		err := network.terminal
		network.mutex.Unlock()
		return model.AcceptedOperation{}, err
	}
	if network.suspended != nil {
		err := network.suspended
		network.mutex.Unlock()
		return model.AcceptedOperation{}, err
	}
	projection, err := network.projection.Submit(operation)
	if err != nil {
		network.retainUnsentLocked(operation, err)
		network.mutex.Unlock()
		return model.AcceptedOperation{}, err
	}
	result := make(chan operationResult, 1)
	network.projection = projection
	network.pending[operation.OperationID] = result
	transport := network.transport
	network.publishLocked()
	network.mutex.Unlock()

	payload, err := json.Marshal(protocol.OperationSubmitPayload{Operation: operation})
	if err != nil {
		network.failPending(operation.OperationID, err)
		return model.AcceptedOperation{}, err
	}
	envelope := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(operation.OperationID), SessionID: network.sessionID, Type: protocol.ClientOperationSubmit, Payload: payload}
	if err := transport.Send(ctx, envelope); err != nil {
		network.failPending(operation.OperationID, err)
		return model.AcceptedOperation{}, err
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
	go func() {
		accepted, err := network.Execute(ctx, operation)
		complete(accepted, err)
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
	baseHash, err := network.projection.Acknowledged.Hash()
	if err != nil {
		return model.Operation{}, err
	}
	changes := make([]model.TileChange, len(target.Changes))
	for index, targetChange := range target.Changes {
		current := tileStateAt(network.projection.Acknowledged, targetChange.Coord)
		if !current.Equal(targetChange.After) {
			return model.Operation{}, fmt.Errorf("accepted operation is no longer safely reversible at (%d,%d,%d)", targetChange.Coord.X, targetChange.Coord.Y, targetChange.Coord.Z)
		}
		changes[index] = model.TileChange{Coord: targetChange.Coord, Before: model.CloneTileState(targetChange.After), After: model.CloneTileState(targetChange.Before)}
	}
	inverseOf := targetID
	return model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: network.projection.Acknowledged.DocumentID, ActorID: network.actor, OperationID: operationID, BaseRevision: network.projection.Acknowledged.Revision, EnvironmentHash: network.projection.Acknowledged.EnvironmentHash, BaseMapHash: baseHash, Kind: model.OperationKindInverse, Changes: changes, InverseOf: &inverseOf}, nil
}

func (network *NetworkExecutor) Snapshot(ctx context.Context) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	return model.CloneSnapshot(network.projection.Acknowledged), nil
}

func (network *NetworkExecutor) ProjectionUpdates() <-chan Projection {
	return network.updates
}

func (network *NetworkExecutor) HasUnacknowledgedOperations() bool {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	return len(network.projection.Pending) != 0
}

func (network *NetworkExecutor) Conflicts() []Conflict {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	conflicts := make([]Conflict, len(network.conflicts))
	for index, conflict := range network.conflicts {
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
	snapshot := network.projection.Acknowledged
	if hash, err := snapshot.Hash(); err == nil {
		for _, operation := range network.projection.Pending {
			network.retainConflictLocked(Conflict{
				OperationID: operation.OperationID, Draft: operation,
				Code:     conflictDeliveryUnconfirmed,
				Message:  "Connection ended before acknowledgement. This edit may already have been applied; reconnect before deciding whether to rebuild it.",
				Revision: snapshot.Revision, MapHash: hash,
			})
		}
	}
	for operationID, waiter := range network.pending {
		delete(network.pending, operationID)
		waiter <- operationResult{err: cause}
	}
	network.projection.Pending = nil
	network.publishLocked()
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
	return nil
}

// ReplaceAcknowledgedSnapshot installs a newer authoritative baseline during reconnect fallback.
func (network *NetworkExecutor) ReplaceAcknowledgedSnapshot(ctx context.Context, snapshot model.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := snapshot.Hash(); err != nil {
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
		currentHash, err := current.Hash()
		if err != nil {
			return err
		}
		replacementHash, err := snapshot.Hash()
		if err != nil {
			return err
		}
		if replacementHash != currentHash {
			return fmt.Errorf("replacement snapshot conflicts with acknowledged revision %d", current.Revision)
		}
		return nil
	}
	network.projection = NewProjection(snapshot)
	// Snapshot fallback carries no operation IDs. Keep unresolved drafts; their
	// rebuild path compares intended values with this fresh authority instead.
	network.publishLocked()
	return nil
}

func (network *NetworkExecutor) RefreshConflict(ctx context.Context, operationID model.OperationID) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if _, exists := network.conflictLocked(operationID); !exists {
		return model.Snapshot{}, fmt.Errorf("conflict for operation %q is not retained", operationID)
	}
	return model.CloneSnapshot(network.projection.Acknowledged), nil
}

func (network *NetworkExecutor) DiscardConflict(ctx context.Context, operationID model.OperationID) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	index, exists := network.conflictLocked(operationID)
	if !exists {
		return model.Snapshot{}, fmt.Errorf("conflict for operation %q is not retained", operationID)
	}
	network.conflicts = append(network.conflicts[:index], network.conflicts[index+1:]...)
	return model.CloneSnapshot(network.projection.Acknowledged), nil
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
	baseHash, err := network.projection.Acknowledged.Hash()
	if err != nil {
		return model.Operation{}, err
	}
	changes := make([]model.TileChange, 0, len(conflict.Draft.Changes))
	for _, draftChange := range conflict.Draft.Changes {
		current := tileStateAt(network.projection.Acknowledged, draftChange.Coord)
		if current.Equal(draftChange.After) {
			continue
		}
		changes = append(changes, model.TileChange{
			Coord:  draftChange.Coord,
			Before: current,
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
	network.conflicts = append(network.conflicts[:index], network.conflicts[index+1:]...)
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
	data, err := json.Marshal(envelope)
	if err != nil {
		network.failAll(err)
		return err
	}
	decoded, err := protocol.DecodeServer(data)
	if err != nil {
		decodeErr := fmt.Errorf("decode network executor message: %w", err)
		network.failAll(decodeErr)
		return decodeErr
	}
	network.mutex.Lock()
	defer network.mutex.Unlock()
	switch decoded.Envelope.Type {
	case protocol.ServerOperationAccepted:
		payload := decoded.Payload.(*protocol.OperationAcceptedPayload)
		if prior, exists := network.accepted[payload.Operation.OperationID]; exists && reflect.DeepEqual(prior, payload.Operation) {
			if payload.MapHash == network.acceptedHashes[payload.Operation.OperationID] {
				return nil
			}
		}
		projection, applyErr := network.projection.Accept(payload.Operation, payload.MapHash)
		if applyErr != nil {
			network.suspendLocked(applyErr)
			return applyErr
		}
		network.projection = projection
		network.accepted[payload.Operation.OperationID] = model.CloneAcceptedOperation(payload.Operation)
		network.acceptedHashes[payload.Operation.OperationID] = payload.MapHash
		if index, found := network.conflictLocked(payload.Operation.OperationID); found {
			conflict := network.conflicts[index]
			if conflict.Code == conflictDeliveryUnconfirmed && model.SameOperation(conflict.Draft, payload.Operation.Operation) {
				network.conflicts = append(network.conflicts[:index], network.conflicts[index+1:]...)
			}
		}
		if waiter, exists := network.pending[payload.Operation.OperationID]; exists {
			delete(network.pending, payload.Operation.OperationID)
			waiter <- operationResult{accepted: model.CloneAcceptedOperation(payload.Operation)}
		}
		network.publishLocked()
	case protocol.ServerOperationRejected:
		payload := decoded.Payload.(*protocol.OperationRejectedPayload)
		projection, conflict, rejectErr := network.projection.Reject(*payload)
		if rejectErr != nil {
			network.suspendLocked(rejectErr)
			return rejectErr
		}
		network.projection = projection
		network.retainConflictLocked(conflict)
		if waiter, exists := network.pending[payload.OperationID]; exists {
			delete(network.pending, payload.OperationID)
			waiter <- operationResult{err: fmt.Errorf("%w: %s: %s", ErrOperationRejected, conflict.Code, conflict.Message)}
		}
		network.publishLocked()
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
	snapshot := network.projection.Acknowledged
	if operation.ProtocolVersion != snapshot.ProtocolVersion || operation.DocumentID != snapshot.DocumentID || operation.EnvironmentHash != snapshot.EnvironmentHash || operation.OperationID.Validate() != nil {
		return // A foreign document must never become rebuildable in this one.
	}
	hash, err := snapshot.Hash()
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
	if len(network.conflicts) > maxRetainedConflicts {
		network.conflicts = network.conflicts[len(network.conflicts)-maxRetainedConflicts:]
	}
}

func (network *NetworkExecutor) failPending(operationID model.OperationID, cause error) {
	network.mutex.Lock()
	defer network.mutex.Unlock()
	if waiter, exists := network.pending[operationID]; exists {
		delete(network.pending, operationID)
		remaining := make([]model.Operation, 0, len(network.projection.Pending)-1)
		for _, operation := range network.projection.Pending {
			if operation.OperationID != operationID {
				remaining = append(remaining, model.CloneOperation(operation))
			} else {
				network.retainUnsentLocked(operation, cause)
			}
		}
		rebased, err := rebasePending(network.projection.Acknowledged, remaining)
		if err != nil {
			waiter <- operationResult{err: cause}
			network.failAllLocked(fmt.Errorf("reapply pending operations after send failure: %w", err))
			return
		}
		network.projection.Pending = rebased
		waiter <- operationResult{err: cause}
		network.publishLocked()
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
	for operationID, waiter := range network.pending {
		delete(network.pending, operationID)
		waiter <- operationResult{err: cause}
	}
	network.projection.Pending = nil
	network.publishLocked()
}

func (network *NetworkExecutor) publishLocked() {
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

func tileStateAt(snapshot model.Snapshot, coord model.Coord) model.TileState {
	for _, tile := range snapshot.Tiles {
		if tile.Coord == coord {
			return model.CloneTileState(tile.State)
		}
	}
	return model.TileState{}
}
