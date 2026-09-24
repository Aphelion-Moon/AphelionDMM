package server

import (
	"context"
	"errors"
	"fmt"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

type RecoveryError struct {
	DocumentID model.DocumentID
	Err        error
}

func (failure *RecoveryError) Error() string {
	return fmt.Sprintf("recover document %q: %v", failure.DocumentID, failure.Err)
}

func (failure *RecoveryError) Unwrap() error {
	return failure.Err
}

func startOrRecoverDocument(ctx context.Context, snapshot model.Snapshot, store SessionStore, config DocumentConfig) (*DocumentOwner, error) {
	owner, err := StartDocumentWithConfig(ctx, snapshot, store, config)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, ErrSessionExists) {
		return nil, err
	}
	suppliedHash, hashErr := snapshot.Hash()
	if hashErr != nil {
		return nil, hashErr
	}
	retainedHash, found, hashErr := store.RevisionHash(ctx, snapshot.DocumentID, snapshot.Revision)
	if hashErr != nil {
		return nil, &RecoveryError{DocumentID: snapshot.DocumentID, Err: fmt.Errorf("verify recovery baseline: %w", hashErr)}
	}
	if !found || retainedHash != suppliedHash {
		return nil, &RecoveryError{DocumentID: snapshot.DocumentID, Err: fmt.Errorf("recovery baseline does not match retained revision %d", snapshot.Revision)}
	}
	owner, err = RecoverDocumentWithConfig(ctx, snapshot.DocumentID, store, config)
	if err != nil {
		return nil, &RecoveryError{DocumentID: snapshot.DocumentID, Err: err}
	}
	return owner, nil
}

func RecoverDocument(ctx context.Context, documentID model.DocumentID, store SessionStore) (*DocumentOwner, error) {
	return RecoverDocumentWithConfig(ctx, documentID, store, DocumentConfig{})
}

func RecoverDocumentWithConfig(ctx context.Context, documentID model.DocumentID, store SessionStore, config DocumentConfig) (*DocumentOwner, error) {
	if store == nil {
		return nil, fmt.Errorf("recover document: store is nil")
	}
	document, err := loadStoredDocument(ctx, documentID, store, config.Telemetry)
	if err != nil {
		return nil, err
	}
	config, err = configureDocumentTransactions(ctx, store, documentID, config)
	if err != nil {
		return nil, err
	}
	return startDocument(ctx, document, store, config), nil
}

func loadStoredDocument(ctx context.Context, documentID model.DocumentID, store SessionStore, observability *collabtelemetry.Telemetry) (*engine.Document, error) {
	loadContext := ctx
	finishLoad := func(error) {}
	if observability != nil {
		loadContext, finishLoad = observability.Store(ctx, collabtelemetry.StoreLoad)
	}
	state, err := store.LoadRecovery(loadContext, documentID)
	if err != nil {
		finishLoad(err)
		return nil, fmt.Errorf("load stored document: %w", err)
	}
	finishLoad(nil)
	finishReplay := func(error) {}
	if observability != nil {
		_, finishReplay = observability.Replay(ctx, len(state.Operations))
	}
	document, err := state.Restore()
	if err != nil {
		finishReplay(err)
		return nil, err
	}
	finishReplay(nil)
	return document, nil
}
