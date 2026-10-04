package executor

import (
	"context"

	"sdmm/internal/aphelion/collab/model"
)

type Executor interface {
	Execute(context.Context, model.Operation) (model.AcceptedOperation, error)
	BuildInverse(context.Context, model.OperationID) (model.Operation, error)
	// Snapshot returns detached committed state owned by the caller.
	Snapshot(context.Context) (model.Snapshot, error)
}

type AsyncExecutor interface {
	Executor
	ExecuteAsync(context.Context, model.Operation, func(model.AcceptedOperation, error)) error
}
