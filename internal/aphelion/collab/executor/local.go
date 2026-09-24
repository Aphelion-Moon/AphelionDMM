package executor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

type Local struct {
	mu       sync.Mutex
	document *engine.Document
	actor    model.ActorID
	now      func() time.Time
}

func NewLocal(document *engine.Document, actor model.ActorID) (*Local, error) {
	if document == nil {
		return nil, fmt.Errorf("create local executor: document is nil")
	}
	if err := actor.Validate(); err != nil {
		return nil, fmt.Errorf("create local executor: %w", err)
	}
	return &Local{
		document: document,
		actor:    actor,
		now:      time.Now,
	}, nil
}

func (local *Local) Execute(ctx context.Context, operation model.Operation) (model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}
	operation.ActorID = local.actor
	return local.document.ApplyContext(ctx, operation, local.now().UTC())
}

func (local *Local) BuildInverse(ctx context.Context, target model.OperationID) (model.Operation, error) {
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	inverseID, err := model.NewOperationID()
	if err != nil {
		return model.Operation{}, fmt.Errorf("build local inverse: %w", err)
	}
	return local.document.BuildInverseContext(ctx, local.actor, target, inverseID)
}

func (local *Local) Snapshot(ctx context.Context) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	return local.document.Snapshot(), nil
}
