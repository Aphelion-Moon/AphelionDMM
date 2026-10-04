package store

import (
	"context"
	"sort"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

func (store *MemoryStore) LoadRecovery(ctx context.Context, documentID model.DocumentID) (engine.RecoveryState, error) {
	if err := ctx.Err(); err != nil {
		return engine.RecoveryState{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return engine.RecoveryState{}, ErrStoreClosed
	}
	snapshot, exists := store.sessions[documentID]
	if !exists {
		return engine.RecoveryState{}, ErrSessionMissing
	}
	head := store.documents[documentID].Revision()
	state := engine.RecoveryState{Snapshot: model.CloneSnapshot(snapshot), SnapshotHash: store.hashes[documentID][snapshot.Revision], HeadRevision: head, HeadHash: store.hashes[documentID][head], Hashes: make(map[model.Revision]string)}
	for revision, hash := range store.hashes[documentID] {
		state.Hashes[revision] = hash
	}
	for _, accepted := range store.accepted[documentID] {
		state.Operations = append(state.Operations, model.CloneAcceptedOperation(accepted))
	}
	sort.Slice(state.Operations, func(i, j int) bool { return state.Operations[i].Revision < state.Operations[j].Revision })
	return state, nil
}
