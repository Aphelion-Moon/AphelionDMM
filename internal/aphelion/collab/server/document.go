package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

const documentRequestQueueSize = 256

var ErrDocumentClosed = errors.New("document owner is closed")

type requestKind uint8

const (
	requestSubmit requestKind = iota + 1
	requestSnapshot
	requestBuildInverse
)

type request struct {
	context   context.Context
	kind      requestKind
	operation model.Operation
	actorID   model.ActorID
	targetID  model.OperationID
	inverseID model.OperationID
	response  chan response
	admit     func(int64) (func(), error)
}

type response struct {
	accepted  model.AcceptedOperation
	snapshot  model.Snapshot
	duplicate bool
	err       error
	release   func()
}

type DocumentOwner struct {
	bulkEdits          bool
	requests           chan request
	done               chan struct{}
	cancel             context.CancelFunc
	closeOnce          sync.Once
	snapshots          sync.WaitGroup
	durableMutex       sync.Mutex
	durableSubscribers map[uint64]chan model.AcceptedOperation
	sharedSubscribers  map[uint64]chan *durableEvent
	nextSubscriberID   uint64
	durableClosed      bool
}

func StartDocument(ctx context.Context, snapshot model.Snapshot, store SessionStore) (*DocumentOwner, error) {
	return StartDocumentWithConfig(ctx, snapshot, store, DocumentConfig{})
}

func StartDocumentWithConfig(ctx context.Context, snapshot model.Snapshot, store SessionStore, config DocumentConfig) (*DocumentOwner, error) {
	if store == nil {
		return nil, fmt.Errorf("start document: store is nil")
	}
	document, err := engine.NewDocument(snapshot)
	if err != nil {
		return nil, fmt.Errorf("start document: %w", err)
	}
	storeContext := ctx
	finishStore := func(error) {}
	if config.Telemetry != nil {
		storeContext, finishStore = config.Telemetry.Store(ctx, collabtelemetry.StoreCreate)
	}
	if err := store.Create(storeContext, snapshot); err != nil {
		finishStore(err)
		return nil, fmt.Errorf("create stored session: %w", err)
	}
	finishStore(nil)
	config, err = configureDocumentTransactions(ctx, store, snapshot.DocumentID, config)
	if err != nil {
		return nil, err
	}
	return startDocument(ctx, document, store, config), nil
}

func startDocument(ctx context.Context, document *engine.Document, store SessionStore, config DocumentConfig) *DocumentOwner {
	runContext, cancel := context.WithCancel(ctx)
	owner := &DocumentOwner{
		bulkEdits: config.BulkEdits,
		requests:  make(chan request, documentRequestQueueSize),
		done:      make(chan struct{}),
		cancel:    cancel,
	}
	go owner.run(runContext, document, store, config)
	return owner
}

func (owner *DocumentOwner) Submit(ctx context.Context, operation model.Operation) (model.AcceptedOperation, error) {
	accepted, _, err := owner.SubmitWithStatus(ctx, operation)
	return accepted, err
}

func (owner *DocumentOwner) SubmitWithStatus(ctx context.Context, operation model.Operation) (model.AcceptedOperation, bool, error) {
	result, err := owner.request(ctx, request{kind: requestSubmit, operation: model.CloneOperation(operation)})
	return result.accepted, result.duplicate, err
}

func (owner *DocumentOwner) Snapshot(ctx context.Context) (model.Snapshot, error) {
	result, err := owner.request(ctx, request{kind: requestSnapshot})
	return result.snapshot, err
}

func (owner *DocumentOwner) BuildInverse(ctx context.Context, actorID model.ActorID, targetID, inverseID model.OperationID) (model.Operation, error) {
	result, err := owner.request(ctx, request{kind: requestBuildInverse, actorID: actorID, targetID: targetID, inverseID: inverseID})
	return result.accepted.Operation, err
}

func (owner *DocumentOwner) buildInverseAdmitted(ctx context.Context, actorID model.ActorID, targetID, inverseID model.OperationID, admit func(int64) (func(), error)) (model.Operation, func(), error) {
	result, err := owner.request(ctx, request{kind: requestBuildInverse, actorID: actorID, targetID: targetID, inverseID: inverseID, admit: admit})
	return result.accepted.Operation, result.release, err
}

func (owner *DocumentOwner) Close(ctx context.Context) error {
	owner.closeOnce.Do(owner.cancel)
	select {
	case <-owner.done:
		owner.snapshots.Wait()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (owner *DocumentOwner) request(ctx context.Context, value request) (response, error) {
	if err := ctx.Err(); err != nil {
		return response{}, err
	}
	value.context = ctx
	value.response = make(chan response, 1)
	select {
	case owner.requests <- value:
	case <-owner.done:
		return response{}, ErrDocumentClosed
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
	if value.kind == requestSubmit || value.kind == requestBuildInverse {
		// An enqueued submission owns its caller's admission lease until the
		// document worker finishes. Cancellation still reaches validation and
		// append, but an ambiguous commit must finish reconciliation before
		// the caller releases memory that the worker is still using.
		select {
		case result := <-value.response:
			return result, result.err
		case <-owner.done:
			// A completed inverse may own a lease. Transfer its response even
			// when shutdown races with completion, so its caller can release it.
			select {
			case result := <-value.response:
				return result, result.err
			default:
			}
			return response{}, ErrDocumentClosed
		}
	}
	select {
	case result := <-value.response:
		return result, result.err
	case <-owner.done:
		return response{}, ErrDocumentClosed
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
}

func (owner *DocumentOwner) run(ctx context.Context, document *engine.Document, store SessionStore, config DocumentConfig) {
	defer close(owner.done)
	defer owner.closeDurable()
	acceptedSinceSnapshot := 0
	snapshotInFlight := false
	snapshotResults := make(chan snapshotResult, 1)
	var interval <-chan time.Time
	var ticker *time.Ticker
	if config.SnapshotInterval > 0 {
		ticker = time.NewTicker(config.SnapshotInterval)
		defer ticker.Stop()
		interval = ticker.C
	}
	scheduleSnapshot := func() {
		snapshotInFlight = true
		owner.saveSnapshot(ctx, store, document.Snapshot(), snapshotResults, config.Telemetry)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-interval:
			if acceptedSinceSnapshot > 0 && !snapshotInFlight {
				scheduleSnapshot()
			}
		case result := <-snapshotResults:
			snapshotInFlight = false
			snapshotFailed := result.err != nil
			if result.err != nil {
				if config.OnSnapshotError != nil {
					config.OnSnapshotError(result.err)
				}
			} else {
				acceptedSinceSnapshot = int(document.Snapshot().Revision - result.revision)
			}
			if !snapshotFailed && config.SnapshotOperationThreshold > 0 && acceptedSinceSnapshot >= config.SnapshotOperationThreshold {
				scheduleSnapshot()
			}
		case request := <-owner.requests:
			switch request.kind {
			case requestSubmit:
				submitCtx, cancel := context.WithCancel(request.context)
				stop := context.AfterFunc(ctx, cancel)
				if ctx.Err() != nil {
					cancel()
				}
				accepted, err := submit(submitCtx, document, store, request.operation, config)
				stop()
				cancel()
				if err == nil {
					document = accepted.document
					if !accepted.duplicate {
						acceptedSinceSnapshot++
						owner.publishDurable(accepted.operation)
					}
				}
				request.response <- response{accepted: accepted.operation, duplicate: accepted.duplicate, err: err}
				if err == nil && !snapshotInFlight && config.SnapshotOperationThreshold > 0 && acceptedSinceSnapshot >= config.SnapshotOperationThreshold {
					scheduleSnapshot()
				}
			case requestSnapshot:
				request.response <- response{snapshot: document.Snapshot()}
			case requestBuildInverse:
				var release func()
				if request.admit != nil {
					bytes, err := document.InverseWorkingBytes(request.context, request.actorID, request.targetID)
					if err == nil {
						release, err = request.admit(bytes)
					}
					if err != nil {
						request.response <- response{err: err}
						continue
					}
				}
				operation, err := document.BuildInverseContext(request.context, request.actorID, request.targetID, request.inverseID)
				if err != nil && release != nil {
					release()
					release = nil
				}
				request.response <- response{accepted: model.AcceptedOperation{Operation: operation}, release: release, err: err}
			default:
				request.response <- response{err: fmt.Errorf("unsupported document request %d", request.kind)}
			}
		}
	}
}

type snapshotResult struct {
	revision model.Revision
	err      error
}

func (owner *DocumentOwner) saveSnapshot(ctx context.Context, store SessionStore, snapshot model.Snapshot, results chan<- snapshotResult, observability *collabtelemetry.Telemetry) {
	owner.snapshots.Add(1)
	go func() {
		defer owner.snapshots.Done()
		storeContext := ctx
		finishStore := func(error) {}
		if observability != nil {
			storeContext, finishStore = observability.Store(ctx, collabtelemetry.StoreSnapshot)
		}
		err := store.SaveSnapshot(storeContext, snapshot)
		finishStore(err)
		result := snapshotResult{revision: snapshot.Revision, err: err}
		select {
		case results <- result:
		case <-ctx.Done():
		}
	}()
}

type submitResult struct {
	operation model.AcceptedOperation
	document  *engine.Document
	duplicate bool
}

func submit(ctx context.Context, document *engine.Document, store SessionStore, operation model.Operation, config DocumentConfig) (submitResult, error) {
	observability := config.Telemetry
	prior, exists, err := store.LookupOperation(ctx, operation.DocumentID, operation.OperationID)
	if err != nil {
		return submitResult{}, err
	}
	if exists {
		reconciled, err := reconcileStoredOperation(ctx, document, store, operation, prior, observability)
		if err != nil {
			return submitResult{}, err
		}
		return submitResult{operation: prior, document: reconciled, duplicate: true}, nil
	}
	candidate := document.Clone()
	accepted, err := candidate.ApplyContext(ctx, operation, time.Now().UTC())
	if err != nil {
		return submitResult{}, err
	}
	if err := validateDocumentDelivery(accepted, config.BulkEdits); err != nil {
		return submitResult{}, err
	}
	storeContext := ctx
	finishStore := func(error) {}
	if observability != nil {
		storeContext, finishStore = observability.Store(ctx, collabtelemetry.StoreAppend)
	}
	if appendErr := store.Append(storeContext, accepted); appendErr != nil {
		finishStore(appendErr)
		reconciliationContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.reconciliationTimeout())
		defer cancel()
		prior, exists, lookupErr := store.LookupOperation(reconciliationContext, operation.DocumentID, operation.OperationID)
		if lookupErr != nil {
			return submitResult{}, errors.Join(appendErr, fmt.Errorf("reconcile append lookup: %w", lookupErr))
		}
		if !exists {
			return submitResult{}, appendErr
		}
		reconciled, reconcileErr := reconcileStoredOperation(reconciliationContext, document, store, operation, prior, observability)
		if reconcileErr != nil {
			return submitResult{}, errors.Join(appendErr, fmt.Errorf("reconcile append: %w", reconcileErr))
		}
		return submitResult{operation: prior, document: reconciled}, nil
	}
	finishStore(nil)
	return submitResult{operation: accepted, document: candidate}, nil
}

func reconcileStoredOperation(ctx context.Context, document *engine.Document, store SessionStore, operation model.Operation, prior model.AcceptedOperation, observability *collabtelemetry.Telemetry) (*engine.Document, error) {
	if !model.SameOperation(prior.Operation, operation) {
		return nil, fmt.Errorf("operation %q conflicts with stored revision %d", operation.OperationID, prior.Revision)
	}
	current := document.Snapshot()
	currentHash, err := current.Hash()
	if err != nil {
		return nil, fmt.Errorf("hash in-memory document: %w", err)
	}
	storedCurrentHash, found, err := store.RevisionHash(ctx, current.DocumentID, current.Revision)
	if err != nil {
		return nil, fmt.Errorf("look up in-memory revision hash: %w", err)
	}
	if prior.Revision <= current.Revision && found && storedCurrentHash == currentHash {
		return document, nil
	}
	reconciled, err := loadStoredDocument(ctx, operation.DocumentID, store, observability)
	if err != nil {
		return nil, fmt.Errorf("reload stored document: %w", err)
	}
	reconciledSnapshot := reconciled.Snapshot()
	if reconciledSnapshot.Revision < prior.Revision {
		return nil, fmt.Errorf("reloaded revision %d is behind stored operation revision %d", reconciledSnapshot.Revision, prior.Revision)
	}
	reconciledHash, err := reconciledSnapshot.Hash()
	if err != nil {
		return nil, fmt.Errorf("hash reloaded document: %w", err)
	}
	storedHash, found, err := store.RevisionHash(ctx, operation.DocumentID, reconciledSnapshot.Revision)
	if err != nil {
		return nil, fmt.Errorf("look up reloaded revision hash: %w", err)
	}
	if !found || storedHash != reconciledHash {
		return nil, fmt.Errorf("reloaded revision %d hash does not match durable state", reconciledSnapshot.Revision)
	}
	return reconciled, nil
}
