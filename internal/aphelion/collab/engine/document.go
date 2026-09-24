package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

// MaxTileChanges is the legacy v1 transport count, retained for legacy-session
// adapters. The operation engine is bounded by the document's valid coordinates.
const MaxTileChanges = 4096

type Document struct {
	// Snapshot payloads and accepted records are immutable after validation.
	// Public inputs/results are deep copies; branches own their metadata maps
	// and validation copies the tile table before replacing or appending states.
	snapshot model.Snapshot
	mapHash  string
	hashes   map[model.Revision]string
	accepted map[model.OperationID]model.AcceptedOperation
	inverted map[model.OperationID]model.OperationID
}

func NewDocument(snapshot model.Snapshot) (*Document, error) {
	mapHash, err := snapshot.Hash()
	if err != nil {
		return nil, fmt.Errorf("validate initial snapshot: %w", err)
	}
	return &Document{
		snapshot: model.CloneSnapshot(snapshot),
		mapHash:  mapHash,
		hashes: map[model.Revision]string{
			snapshot.Revision: mapHash,
		},
		accepted: make(map[model.OperationID]model.AcceptedOperation),
		inverted: make(map[model.OperationID]model.OperationID),
	}, nil
}

func (document *Document) Apply(operation model.Operation, acceptedAt time.Time) (model.AcceptedOperation, error) {
	return document.ApplyContext(context.Background(), operation, acceptedAt)
}

func (document *Document) ApplyContext(ctx context.Context, operation model.Operation, acceptedAt time.Time) (model.AcceptedOperation, error) {
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}
	if operation.DocumentID != document.snapshot.DocumentID {
		return model.AcceptedOperation{}, document.reject(CodeWrongDocument, fmt.Errorf("document id is %q, want %q", operation.DocumentID, document.snapshot.DocumentID))
	}
	if accepted, exists := document.accepted[operation.OperationID]; exists {
		if !model.SameOperation(accepted.Operation, operation) {
			return model.AcceptedOperation{}, document.reject(CodeInvalidOperation, fmt.Errorf("operation identity already names different content"))
		}
		return model.CloneAcceptedOperation(accepted), nil
	}

	normalized, candidate, candidateHash, err := document.validateContext(ctx, operation)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.AcceptedOperation{}, err
	}

	candidate.Revision = document.snapshot.Revision + 1
	accepted := model.AcceptedOperation{
		Operation:  normalized,
		Revision:   candidate.Revision,
		AcceptedAt: acceptedAt,
	}
	document.snapshot = candidate
	document.mapHash = candidateHash
	document.hashes[candidate.Revision] = candidateHash
	// normalized was detached during validation and never escapes without a
	// clone. History and candidate tiles may therefore share immutable payloads.
	document.accepted[accepted.OperationID] = accepted
	if accepted.InverseOf != nil {
		document.inverted[*accepted.InverseOf] = accepted.OperationID
	}
	return model.CloneAcceptedOperation(accepted), nil
}

func (document *Document) Snapshot() model.Snapshot {
	return model.CloneSnapshot(document.snapshot)
}

func (document *Document) Clone() *Document {
	clone := &Document{
		snapshot: document.snapshot,
		mapHash:  document.mapHash,
		hashes:   make(map[model.Revision]string, len(document.hashes)),
		accepted: make(map[model.OperationID]model.AcceptedOperation, len(document.accepted)),
		inverted: make(map[model.OperationID]model.OperationID, len(document.inverted)),
	}
	for revision, hash := range document.hashes {
		clone.hashes[revision] = hash
	}
	for operationID, accepted := range document.accepted {
		clone.accepted[operationID] = accepted
	}
	for targetID, inverseID := range document.inverted {
		clone.inverted[targetID] = inverseID
	}
	return clone
}

func (document *Document) validate(operation model.Operation) (model.Operation, model.Snapshot, string, error) {
	return document.validateContext(context.Background(), operation)
}

func (document *Document) validateContext(ctx context.Context, operation model.Operation) (model.Operation, model.Snapshot, string, error) {
	if operation.ProtocolVersion != model.ProtocolVersion {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("protocol version is %d, want %d", operation.ProtocolVersion, model.ProtocolVersion))
	}
	if err := operation.ActorID.Validate(); err != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, err)
	}
	if err := operation.OperationID.Validate(); err != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, err)
	}
	if operation.InverseOf != nil {
		if err := operation.InverseOf.Validate(); err != nil {
			return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, err)
		}
	}
	if err := model.ValidateSHA256("environment hash", operation.EnvironmentHash); err != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, err)
	}
	if operation.EnvironmentHash != document.snapshot.EnvironmentHash {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeWrongEnvironment, fmt.Errorf("environment hash does not match document"))
	}
	if err := model.ValidateSHA256("base map hash", operation.BaseMapHash); err != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, err)
	}
	baseHash, exists := document.hashes[operation.BaseRevision]
	if !exists {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeUnknownBaseRevision, fmt.Errorf("base revision %d is not retained", operation.BaseRevision))
	}
	if operation.BaseMapHash != baseHash {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeBaseHashMismatch, fmt.Errorf("base map hash does not match revision %d", operation.BaseRevision))
	}
	if operation.Kind != model.OperationKindTileChange && operation.Kind != model.OperationKindInverse {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("unsupported operation kind %q", operation.Kind))
	}
	if operation.Kind == model.OperationKindTileChange && operation.InverseOf != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("forward tile change cannot name inverse target"))
	}
	cellCount, err := document.snapshot.CellCount()
	if err != nil || len(operation.Changes) == 0 || len(operation.Changes) > cellCount {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("tile change count is %d, want 1 through document cell count %d", len(operation.Changes), cellCount))
	}

	normalized, err := cloneOperationContext(ctx, operation)
	if err != nil {
		return model.Operation{}, model.Snapshot{}, "", err
	}
	sort.Slice(normalized.Changes, func(left int, right int) bool {
		if normalized.Changes[left].Coord.Z != normalized.Changes[right].Coord.Z {
			return normalized.Changes[left].Coord.Z < normalized.Changes[right].Coord.Z
		}
		if normalized.Changes[left].Coord.Y != normalized.Changes[right].Coord.Y {
			return normalized.Changes[left].Coord.Y < normalized.Changes[right].Coord.Y
		}
		return normalized.Changes[left].Coord.X < normalized.Changes[right].Coord.X
	})
	if err := document.validateInverseContext(ctx, normalized); err != nil {
		return model.Operation{}, model.Snapshot{}, "", err
	}

	candidate := document.snapshot
	candidate.Tiles = slices.Clone(document.snapshot.Tiles)
	tileIndexes := make(map[model.Coord]int, len(candidate.Tiles))
	for index, tile := range candidate.Tiles {
		tileIndexes[tile.Coord] = index
	}
	seen := make(map[model.Coord]struct{}, len(normalized.Changes))
	for _, change := range normalized.Changes {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, model.Snapshot{}, "", err
		}
		if _, exists := seen[change.Coord]; exists {
			return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("duplicate change coordinate (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z))
		}
		seen[change.Coord] = struct{}{}
		if !candidate.Contains(change.Coord) {
			return model.Operation{}, model.Snapshot{}, "", document.reject(CodeOutOfBounds, fmt.Errorf("coordinate (%d,%d,%d) is outside document", change.Coord.X, change.Coord.Y, change.Coord.Z))
		}

		index, exists := tileIndexes[change.Coord]
		current := model.TileState{}
		if exists {
			current = candidate.Tiles[index].State
		}
		if !current.Equal(change.Before) {
			return model.Operation{}, model.Snapshot{}, "", document.reject(CodePreconditionFailed, fmt.Errorf("tile precondition failed at (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z))
		}
		if exists {
			candidate.Tiles[index].State = change.After
		} else {
			tileIndexes[change.Coord] = len(candidate.Tiles)
			candidate.Tiles = append(candidate.Tiles, model.Tile{Coord: change.Coord, State: change.After})
		}
	}

	candidateHash, err := candidate.Hash()
	if err != nil {
		return model.Operation{}, model.Snapshot{}, "", document.reject(CodeInvalidOperation, fmt.Errorf("validate resulting map: %w", err))
	}
	return normalized, candidate, candidateHash, nil
}

func cloneOperationContext(ctx context.Context, source model.Operation) (model.Operation, error) {
	owned := source
	if source.InverseOf != nil {
		id := *source.InverseOf
		owned.InverseOf = &id
	}
	owned.Changes = make([]model.TileChange, len(source.Changes))
	for i, change := range source.Changes {
		if err := ctx.Err(); err != nil {
			return model.Operation{}, err
		}
		owned.Changes[i] = model.TileChange{Coord: change.Coord, Before: model.CloneTileState(change.Before), After: model.CloneTileState(change.After)}
	}
	return owned, nil
}

func (document *Document) reject(code Code, cause error) *Rejection {
	return &Rejection{
		Code:            code,
		CurrentRevision: document.snapshot.Revision,
		CurrentMapHash:  document.mapHash,
		Cause:           cause,
	}
}
