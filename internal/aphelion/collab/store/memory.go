package store

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

type MemoryStore struct {
	mutex               sync.RWMutex
	closed              bool
	sessions            map[model.DocumentID]model.Snapshot
	operations          map[model.DocumentID][]model.AcceptedOperation
	accepted            map[model.DocumentID]map[model.OperationID]model.AcceptedOperation
	documents           map[model.DocumentID]*engine.Document
	hashes              map[model.DocumentID]map[model.Revision]string
	checkpoints         map[model.DocumentID]map[model.CheckpointID]model.ExportCheckpoint
	checkpointKeys      map[model.DocumentID]map[string]model.CheckpointID
	transactionVersions map[model.DocumentID]int
	revisionHeads       map[model.DocumentID]model.Revision
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:            make(map[model.DocumentID]model.Snapshot),
		operations:          make(map[model.DocumentID][]model.AcceptedOperation),
		accepted:            make(map[model.DocumentID]map[model.OperationID]model.AcceptedOperation),
		documents:           make(map[model.DocumentID]*engine.Document),
		hashes:              make(map[model.DocumentID]map[model.Revision]string),
		checkpoints:         make(map[model.DocumentID]map[model.CheckpointID]model.ExportCheckpoint),
		checkpointKeys:      make(map[model.DocumentID]map[string]model.CheckpointID),
		transactionVersions: make(map[model.DocumentID]int),
		revisionHeads:       make(map[model.DocumentID]model.Revision),
	}
}

func (store *MemoryStore) Create(ctx context.Context, snapshot model.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return ErrStoreClosed
	}
	if _, exists := store.sessions[snapshot.DocumentID]; exists {
		return ErrSessionExists
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		return err
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	store.sessions[snapshot.DocumentID] = model.CloneSnapshot(snapshot)
	store.operations[snapshot.DocumentID] = nil
	store.accepted[snapshot.DocumentID] = make(map[model.OperationID]model.AcceptedOperation)
	store.documents[snapshot.DocumentID] = document
	store.hashes[snapshot.DocumentID] = map[model.Revision]string{snapshot.Revision: mapHash}
	store.checkpoints[snapshot.DocumentID] = make(map[model.CheckpointID]model.ExportCheckpoint)
	store.checkpointKeys[snapshot.DocumentID] = make(map[string]model.CheckpointID)
	store.transactionVersions[snapshot.DocumentID] = LegacyTransactionVersion
	store.revisionHeads[snapshot.DocumentID] = snapshot.Revision
	return nil
}

func (store *MemoryStore) ConfigureTransactions(ctx context.Context, documentID model.DocumentID, version int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version != LegacyTransactionVersion && version != BulkTransactionVersion {
		return ErrUnsupportedTransactionVersion
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return ErrStoreClosed
	}
	if _, exists := store.sessions[documentID]; !exists {
		return ErrSessionMissing
	}
	current := store.transactionVersions[documentID]
	if current == 0 {
		current = LegacyTransactionVersion
	}
	if version < current {
		return ErrTransactionDowngrade
	}
	store.transactionVersions[documentID] = version
	return nil
}

func (store *MemoryStore) TransactionVersion(ctx context.Context, documentID model.DocumentID) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return 0, ErrStoreClosed
	}
	version, exists := store.transactionVersions[documentID]
	if !exists {
		return 0, ErrSessionMissing
	}
	if version == 0 {
		return LegacyTransactionVersion, nil
	}
	return version, nil
}

func (store *MemoryStore) Append(ctx context.Context, accepted model.AcceptedOperation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return ErrStoreClosed
	}
	if _, exists := store.sessions[accepted.DocumentID]; !exists {
		return ErrSessionMissing
	}
	if prior, exists := store.accepted[accepted.DocumentID][accepted.OperationID]; exists {
		if !reflect.DeepEqual(prior, accepted) {
			return fmt.Errorf("operation %q conflicts with stored revision %d", accepted.OperationID, prior.Revision)
		}
		return nil
	}
	wantRevision := store.sessions[accepted.DocumentID].Revision + model.Revision(len(store.operations[accepted.DocumentID])) + 1
	if accepted.Revision != wantRevision {
		return fmt.Errorf("operation revision is %d, want %d", accepted.Revision, wantRevision)
	}
	candidate := store.documents[accepted.DocumentID].Clone()
	verified, err := candidate.Apply(accepted.Operation, accepted.AcceptedAt)
	if err != nil {
		return fmt.Errorf("verify accepted operation: %w", err)
	}
	if !reflect.DeepEqual(verified, accepted) {
		return fmt.Errorf("accepted operation does not match verified operation")
	}
	mapHash, err := candidate.Snapshot().Hash()
	if err != nil {
		return fmt.Errorf("hash accepted revision: %w", err)
	}
	cloned := model.CloneAcceptedOperation(accepted)
	store.operations[accepted.DocumentID] = append(store.operations[accepted.DocumentID], cloned)
	store.accepted[accepted.DocumentID][accepted.OperationID] = cloned
	store.documents[accepted.DocumentID] = candidate
	store.hashes[accepted.DocumentID][accepted.Revision] = mapHash
	store.revisionHeads[accepted.DocumentID] = accepted.Revision
	return nil
}

func (store *MemoryStore) Load(ctx context.Context, documentID model.DocumentID) (model.Snapshot, []model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.Snapshot{}, nil, ErrStoreClosed
	}
	snapshot, exists := store.sessions[documentID]
	if !exists {
		return model.Snapshot{}, nil, ErrSessionMissing
	}
	operations := make([]model.AcceptedOperation, len(store.operations[documentID]))
	for index, accepted := range store.operations[documentID] {
		operations[index] = model.CloneAcceptedOperation(accepted)
	}
	return model.CloneSnapshot(snapshot), operations, nil
}

func (store *MemoryStore) SaveSnapshot(ctx context.Context, snapshot model.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return ErrStoreClosed
	}
	if _, exists := store.sessions[snapshot.DocumentID]; !exists {
		return ErrSessionMissing
	}
	if _, err := engine.NewDocument(snapshot); err != nil {
		return err
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		return err
	}
	retainedHash, exists := store.hashes[snapshot.DocumentID][snapshot.Revision]
	if !exists || retainedHash != mapHash {
		return fmt.Errorf("snapshot revision %d is not retained with the supplied hash", snapshot.Revision)
	}
	base := store.sessions[snapshot.DocumentID]
	current := store.documents[snapshot.DocumentID].Snapshot()
	if snapshot.Revision < base.Revision || snapshot.Revision > current.Revision {
		return fmt.Errorf("snapshot revision %d is outside retained range %d through %d", snapshot.Revision, base.Revision, current.Revision)
	}
	store.sessions[snapshot.DocumentID] = model.CloneSnapshot(snapshot)
	retained := store.operations[snapshot.DocumentID][:0]
	for _, accepted := range store.operations[snapshot.DocumentID] {
		if accepted.Revision > snapshot.Revision {
			retained = append(retained, accepted)
		}
	}
	store.operations[snapshot.DocumentID] = retained
	return nil
}

func (store *MemoryStore) RevisionHash(ctx context.Context, documentID model.DocumentID, revision model.Revision) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return "", false, ErrStoreClosed
	}
	hashes, exists := store.hashes[documentID]
	if !exists {
		return "", false, ErrSessionMissing
	}
	hash, exists := hashes[revision]
	return hash, exists, nil
}

func (store *MemoryStore) LookupOperation(ctx context.Context, documentID model.DocumentID, operationID model.OperationID) (model.AcceptedOperation, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, false, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.AcceptedOperation{}, false, ErrStoreClosed
	}
	operations, exists := store.accepted[documentID]
	if !exists {
		return model.AcceptedOperation{}, false, ErrSessionMissing
	}
	accepted, exists := operations[operationID]
	return model.CloneAcceptedOperation(accepted), exists, nil
}

func (store *MemoryStore) CreateExportCheckpoint(ctx context.Context, checkpoint model.ExportCheckpoint) (model.ExportCheckpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if err := checkpoint.Validate(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	if checkpoint.Status != model.ExportCheckpointPending {
		return model.ExportCheckpoint{}, false, fmt.Errorf("new export checkpoint must be pending")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return model.ExportCheckpoint{}, false, ErrStoreClosed
	}
	if _, exists := store.sessions[checkpoint.DocumentID]; !exists {
		return model.ExportCheckpoint{}, false, ErrSessionMissing
	}
	if checkpointID, exists := store.checkpointKeys[checkpoint.DocumentID][checkpoint.IdempotencyKey]; exists {
		prior := store.checkpoints[checkpoint.DocumentID][checkpointID]
		if SameCheckpointRequest(prior, checkpoint) {
			return model.CloneExportCheckpoint(prior), false, nil
		}
		return model.ExportCheckpoint{}, false, ErrCheckpointConflict
	}
	hash, exists := store.hashes[checkpoint.DocumentID][checkpoint.Revision]
	if !exists || hash != checkpoint.MapHash {
		return model.ExportCheckpoint{}, false, fmt.Errorf("checkpoint revision/hash is not retained")
	}
	if prior, exists := store.checkpoints[checkpoint.DocumentID][checkpoint.CheckpointID]; exists {
		if SameCheckpointRequest(prior, checkpoint) {
			return model.CloneExportCheckpoint(prior), false, nil
		}
		return model.ExportCheckpoint{}, false, ErrCheckpointConflict
	}
	cloned := model.CloneExportCheckpoint(checkpoint)
	store.checkpoints[checkpoint.DocumentID][checkpoint.CheckpointID] = cloned
	store.checkpointKeys[checkpoint.DocumentID][checkpoint.IdempotencyKey] = checkpoint.CheckpointID
	return model.CloneExportCheckpoint(cloned), true, nil
}

func (store *MemoryStore) LookupExportCheckpoint(ctx context.Context, documentID model.DocumentID, checkpointID model.CheckpointID) (model.ExportCheckpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.ExportCheckpoint{}, false, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return model.ExportCheckpoint{}, false, ErrStoreClosed
	}
	checkpoints, exists := store.checkpoints[documentID]
	if !exists {
		return model.ExportCheckpoint{}, false, ErrSessionMissing
	}
	checkpoint, exists := checkpoints[checkpointID]
	return model.CloneExportCheckpoint(checkpoint), exists, nil
}

func (store *MemoryStore) CompleteExportCheckpoint(ctx context.Context, documentID model.DocumentID, checkpointID model.CheckpointID, completion model.ExportCheckpointCompletion) (model.ExportCheckpoint, error) {
	if err := ctx.Err(); err != nil {
		return model.ExportCheckpoint{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return model.ExportCheckpoint{}, ErrStoreClosed
	}
	checkpoints, exists := store.checkpoints[documentID]
	if !exists {
		return model.ExportCheckpoint{}, ErrSessionMissing
	}
	checkpoint, exists := checkpoints[checkpointID]
	if !exists {
		return model.ExportCheckpoint{}, ErrCheckpointMissing
	}
	completed, err := checkpoint.Complete(completion)
	if err != nil {
		if checkpoint.Status != model.ExportCheckpointPending {
			expected := model.CloneExportCheckpoint(checkpoint)
			if CheckpointMatchesCompletion(expected, completion) {
				return expected, nil
			}
			return model.ExportCheckpoint{}, ErrCheckpointTerminal
		}
		return model.ExportCheckpoint{}, err
	}
	checkpoints[checkpointID] = model.CloneExportCheckpoint(completed)
	return model.CloneExportCheckpoint(completed), nil
}

func (store *MemoryStore) Close() error {
	store.mutex.Lock()
	store.closed = true
	store.mutex.Unlock()
	return nil
}
