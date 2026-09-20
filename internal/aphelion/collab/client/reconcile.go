package client

import (
	"fmt"
	"sdmm/internal/aphelion/diagnostics/uistage"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

type Projection struct {
	Acknowledged model.Snapshot
	Pending      []model.Operation
}

type Conflict struct {
	OperationID         model.OperationID
	Draft               model.Operation
	Code                string
	Message             string
	Revision            model.Revision
	MapHash             string
	AuthoritativeValues []model.Tile
}

func NewProjection(snapshot model.Snapshot) Projection {
	return Projection{Acknowledged: model.CloneSnapshot(snapshot)}
}

func (projection Projection) Submit(operation model.Operation) (Projection, error) {
	acknowledgedHash, err := projection.Acknowledged.Hash()
	if err != nil {
		return Projection{}, err
	}
	if operation.ProtocolVersion != model.ProtocolVersion || operation.DocumentID != projection.Acknowledged.DocumentID || operation.EnvironmentHash != projection.Acknowledged.EnvironmentHash {
		return Projection{}, fmt.Errorf("operation is incompatible with acknowledged document")
	}
	if operation.BaseRevision != projection.Acknowledged.Revision || operation.BaseMapHash != acknowledgedHash {
		return Projection{}, fmt.Errorf("operation base does not match acknowledged revision")
	}
	visible, err := projection.Visible()
	if err != nil {
		return Projection{}, err
	}
	if _, err := applyOperation(visible, operation); err != nil {
		return Projection{}, fmt.Errorf("apply speculative operation: %w", err)
	}
	result := cloneProjection(projection)
	result.Pending = append(result.Pending, model.CloneOperation(operation))
	return result, nil
}

func (projection Projection) Accept(accepted model.AcceptedOperation, authoritativeHash string) (Projection, error) {
	if accepted.Revision != projection.Acknowledged.Revision+1 {
		return Projection{}, fmt.Errorf("accepted revision is %d, want %d", accepted.Revision, projection.Acknowledged.Revision+1)
	}
	if err := model.ValidateSHA256("authoritative map hash", authoritativeHash); err != nil {
		return Projection{}, err
	}
	next, err := applyAcceptedSnapshot(projection.Acknowledged, accepted)
	if err != nil {
		return Projection{}, err
	}
	actualHash, err := next.Hash()
	if err != nil {
		return Projection{}, err
	}
	if actualHash != authoritativeHash {
		return Projection{}, fmt.Errorf("authoritative map hash mismatch at revision %d", accepted.Revision)
	}
	remaining := make([]model.Operation, 0, len(projection.Pending))
	for _, pending := range projection.Pending {
		if pending.OperationID != accepted.OperationID {
			remaining = append(remaining, model.CloneOperation(pending))
		}
	}
	rebased, err := rebasePending(next, remaining)
	if err != nil {
		return Projection{}, fmt.Errorf("reapply pending operations: %w", err)
	}
	return Projection{Acknowledged: next, Pending: rebased}, nil
}

func (projection Projection) Reject(rejected protocol.OperationRejectedPayload) (Projection, Conflict, error) {
	acknowledgedHash, err := projection.Acknowledged.Hash()
	if err != nil {
		return Projection{}, Conflict{}, err
	}
	if rejected.Revision != projection.Acknowledged.Revision || rejected.MapHash != acknowledgedHash {
		return Projection{}, Conflict{}, fmt.Errorf("rejection authority does not match acknowledged revision")
	}
	remaining := make([]model.Operation, 0, len(projection.Pending))
	found := false
	var draft model.Operation
	for _, pending := range projection.Pending {
		if pending.OperationID == rejected.OperationID {
			found = true
			draft = model.CloneOperation(pending)
			continue
		}
		remaining = append(remaining, model.CloneOperation(pending))
	}
	if !found {
		return Projection{}, Conflict{}, fmt.Errorf("rejected operation %q is not pending", rejected.OperationID)
	}
	rebased, err := rebasePending(projection.Acknowledged, remaining)
	if err != nil {
		return Projection{}, Conflict{}, fmt.Errorf("reapply pending operations after rejection: %w", err)
	}
	return Projection{Acknowledged: model.CloneSnapshot(projection.Acknowledged), Pending: rebased}, Conflict{
		OperationID:         rejected.OperationID,
		Draft:               draft,
		Code:                rejected.Code,
		Message:             rejected.Message,
		Revision:            rejected.Revision,
		MapHash:             rejected.MapHash,
		AuthoritativeValues: cloneTiles(rejected.AuthoritativeValues),
	}, nil
}

func cloneTiles(tiles []model.Tile) []model.Tile {
	result := make([]model.Tile, len(tiles))
	for index, tile := range tiles {
		result[index] = model.Tile{Coord: tile.Coord, State: model.CloneTileState(tile.State)}
	}
	return result
}

func (projection Projection) Visible() (model.Snapshot, error) {
	defer uistage.Begin(uistage.ProjectionVisible).End()
	// A competing authoritative edit may hide speculation without resolving its
	// submitted intent. Build visible state independently; retain pending drafts.
	visible := visibleProjection(projection.Acknowledged, projection.Pending)
	visible.Revision = projection.Acknowledged.Revision
	return visible, nil
}

func applyAcceptedSnapshot(snapshot model.Snapshot, accepted model.AcceptedOperation) (model.Snapshot, error) {
	if accepted.DocumentID != snapshot.DocumentID || accepted.EnvironmentHash != snapshot.EnvironmentHash {
		return model.Snapshot{}, fmt.Errorf("accepted operation is incompatible with acknowledged document")
	}
	next, err := applyOperation(snapshot, accepted.Operation)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("apply accepted operation: %w", err)
	}
	next.Revision = accepted.Revision
	return next, nil
}

func applyOperation(snapshot model.Snapshot, operation model.Operation) (model.Snapshot, error) {
	result := model.CloneSnapshot(snapshot)
	indexes := make(map[model.Coord]int, len(result.Tiles))
	for index, tile := range result.Tiles {
		indexes[tile.Coord] = index
	}
	seen := make(map[model.Coord]struct{}, len(operation.Changes))
	for _, change := range operation.Changes {
		if _, exists := seen[change.Coord]; exists {
			return model.Snapshot{}, fmt.Errorf("duplicate coordinate (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
		seen[change.Coord] = struct{}{}
		if !result.Contains(change.Coord) {
			return model.Snapshot{}, fmt.Errorf("coordinate (%d,%d,%d) is out of bounds", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
		index, exists := indexes[change.Coord]
		current := model.TileState{}
		if exists {
			current = result.Tiles[index].State
		}
		if !current.Equal(change.Before) {
			return model.Snapshot{}, fmt.Errorf("precondition failed at (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
		if exists {
			result.Tiles[index].State = model.CloneTileState(change.After)
		} else {
			indexes[change.Coord] = len(result.Tiles)
			result.Tiles = append(result.Tiles, model.Tile{Coord: change.Coord, State: model.CloneTileState(change.After)})
		}
	}
	if _, err := result.Hash(); err != nil {
		return model.Snapshot{}, err
	}
	return result, nil
}

func rebasePending(acknowledged model.Snapshot, pending []model.Operation) ([]model.Operation, error) {
	_, err := acknowledged.Hash()
	if err != nil {
		return nil, err
	}
	rebased := make([]model.Operation, 0, len(pending))
	for _, operation := range pending {
		// These operations have already crossed the transport boundary. Their
		// base and preconditions must stay identical to the original submission.
		rebased = append(rebased, model.CloneOperation(operation))
	}
	return rebased, nil
}

func cloneProjection(projection Projection) Projection {
	clone := Projection{Acknowledged: model.CloneSnapshot(projection.Acknowledged), Pending: make([]model.Operation, len(projection.Pending))}
	for index, operation := range projection.Pending {
		clone.Pending[index] = model.CloneOperation(operation)
	}
	return clone
}

func tileAt(snapshot model.Snapshot, x int) model.TileState {
	coord := model.Coord{X: x, Y: 1, Z: 1}
	for _, tile := range snapshot.Tiles {
		if tile.Coord == coord {
			return model.CloneTileState(tile.State)
		}
	}
	return model.TileState{}
}
