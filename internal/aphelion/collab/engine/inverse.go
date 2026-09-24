package engine

import (
	"context"
	"fmt"
	"math"

	"sdmm/internal/aphelion/collab/model"
)

// InverseWorkingBytes estimates transient inverse copies, validation, indexes
// and publication before any operation-sized allocation. It does not account
// for retained history, which is already resident. Call under document ownership.
func (document *Document) InverseWorkingBytes(ctx context.Context, actor model.ActorID, targetID model.OperationID) (int64, error) {
	target, exists := document.accepted[targetID]
	if !exists {
		return 0, document.reject(CodeOperationNotFound, fmt.Errorf("target operation %q was not accepted", targetID))
	}
	if target.ActorID != actor {
		return 0, document.reject(CodeActorMismatch, fmt.Errorf("target operation belongs to actor %q", target.ActorID))
	}
	// Eight copies is deliberately conservative for detached mutable results
	// and the owner/store/publication bridges. This is admission, not a heap meter.
	const copies int64 = 8
	bytes := int64(1 << 20)
	add := func(count, size int64) bool {
		if count < 0 || size < 0 || count > (math.MaxInt64/copies-bytes)/size {
			return false
		}
		bytes += count * size
		return true
	}
	if !add(int64(len(document.snapshot.Tiles)), 384) || !add(int64(len(document.accepted)), 256) {
		return 0, fmt.Errorf("inverse working-memory estimate exceeds addressable capacity")
	}
	for _, change := range target.Changes {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !add(1, 512) {
			return 0, fmt.Errorf("inverse working-memory estimate exceeds addressable capacity")
		}
		for _, state := range []model.TileState{change.Before, change.After} {
			for _, prefab := range state.Prefabs {
				if !add(1, 512) || !add(int64(len(prefab.Path)), 1) {
					return 0, fmt.Errorf("inverse working-memory estimate exceeds addressable capacity")
				}
				for name, raw := range prefab.Vars {
					if !add(1, 128) || !add(int64(len(name)), 1) || !add(int64(len(raw)), 1) {
						return 0, fmt.Errorf("inverse working-memory estimate exceeds addressable capacity")
					}
				}
			}
		}
	}
	return bytes * copies, nil
}

func (document *Document) BuildInverse(actor model.ActorID, targetID model.OperationID, inverseID model.OperationID) (model.Operation, error) {
	return document.BuildInverseContext(context.Background(), actor, targetID, inverseID)
}

func (document *Document) BuildInverseContext(ctx context.Context, actor model.ActorID, targetID model.OperationID, inverseID model.OperationID) (model.Operation, error) {
	if err := ctx.Err(); err != nil {
		return model.Operation{}, err
	}
	if err := actor.Validate(); err != nil {
		return model.Operation{}, document.reject(CodeInvalidOperation, err)
	}
	if err := inverseID.Validate(); err != nil {
		return model.Operation{}, document.reject(CodeInvalidOperation, err)
	}
	target, exists := document.accepted[targetID]
	if !exists {
		return model.Operation{}, document.reject(CodeOperationNotFound, fmt.Errorf("target operation %q was not accepted", targetID))
	}
	if target.ActorID != actor {
		return model.Operation{}, document.reject(CodeActorMismatch, fmt.Errorf("target operation belongs to actor %q", target.ActorID))
	}
	if inverse, exists := document.inverted[targetID]; exists {
		return model.Operation{}, document.reject(CodeAlreadyInverted, fmt.Errorf("target operation was inverted by %q", inverse))
	}
	if target.Kind == model.OperationKindInverse {
		return model.Operation{}, document.reject(CodeInvalidOperation, fmt.Errorf("inverse operations are redone as new forward operations"))
	}

	changes := make([]model.TileChange, len(target.Changes))
	tileIndexes := make(map[model.Coord]int, len(document.snapshot.Tiles))
	for index, tile := range document.snapshot.Tiles {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return model.Operation{}, err
			}
		}
		tileIndexes[tile.Coord] = index
	}
	for index, targetChange := range target.Changes {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, err
		}
		currentIndex, exists := tileIndexes[targetChange.Coord]
		current := model.TileState{}
		if exists {
			current = document.snapshot.Tiles[currentIndex].State
		}
		if !current.Equal(targetChange.After) {
			return model.Operation{}, document.reject(CodePreconditionFailed, fmt.Errorf("target after-value changed at (%d,%d,%d)", targetChange.Coord.X, targetChange.Coord.Y, targetChange.Coord.Z))
		}
		changes[index] = model.TileChange{
			Coord:  targetChange.Coord,
			Before: model.CloneTileState(targetChange.After),
			After:  model.CloneTileState(targetChange.Before),
		}
	}

	inverseOf := targetID
	return model.Operation{
		ProtocolVersion: model.ProtocolVersion,
		DocumentID:      document.snapshot.DocumentID,
		ActorID:         actor,
		OperationID:     inverseID,
		BaseRevision:    document.snapshot.Revision,
		EnvironmentHash: document.snapshot.EnvironmentHash,
		BaseMapHash:     document.mapHash,
		Kind:            model.OperationKindInverse,
		Changes:         changes,
		InverseOf:       &inverseOf,
	}, nil
}

func (document *Document) validateInverseContext(ctx context.Context, operation model.Operation) error {
	if operation.Kind != model.OperationKindInverse {
		return nil
	}
	if operation.InverseOf == nil {
		return document.reject(CodeInvalidOperation, fmt.Errorf("inverse operation must name its target"))
	}
	target, exists := document.accepted[*operation.InverseOf]
	if !exists {
		return document.reject(CodeOperationNotFound, fmt.Errorf("target operation %q was not accepted", *operation.InverseOf))
	}
	if target.ActorID != operation.ActorID {
		return document.reject(CodeActorMismatch, fmt.Errorf("target operation belongs to actor %q", target.ActorID))
	}
	if target.Kind == model.OperationKindInverse {
		return document.reject(CodeInvalidOperation, fmt.Errorf("inverse operations are redone as new forward operations"))
	}
	if inverse, exists := document.inverted[*operation.InverseOf]; exists {
		return document.reject(CodeAlreadyInverted, fmt.Errorf("target operation was inverted by %q", inverse))
	}
	if len(operation.Changes) != len(target.Changes) {
		return document.reject(CodeInvalidOperation, fmt.Errorf("inverse change count is %d, want %d", len(operation.Changes), len(target.Changes)))
	}
	for index, change := range operation.Changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		targetChange := target.Changes[index]
		if change.Coord != targetChange.Coord || !change.Before.Equal(targetChange.After) || !change.After.Equal(targetChange.Before) {
			return document.reject(CodeInvalidOperation, fmt.Errorf("inverse change %d does not exactly reverse target", index))
		}
	}
	return nil
}
