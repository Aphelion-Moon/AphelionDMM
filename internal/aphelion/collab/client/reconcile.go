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

	// acknowledgedHash is the canonical digest verified for Acknowledged. It
	// is intentionally private so public projection values cannot advertise an
	// unverified digest; values constructed by legacy callers fall back to one
	// validation at the operation boundary.
	acknowledgedHash string
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
	clone := model.CloneSnapshot(snapshot)
	hash, _ := clone.Hash()
	return Projection{Acknowledged: clone, acknowledgedHash: hash}
}

func newProjection(snapshot model.Snapshot, verifiedHash string) Projection {
	clone := model.CloneSnapshot(snapshot)
	if verifiedHash == "" {
		verifiedHash, _ = clone.Hash()
	}
	return Projection{Acknowledged: clone, acknowledgedHash: verifiedHash}
}

func (projection Projection) acknowledgedMapHash() (string, error) {
	// Projection is a public value with an exported mutable snapshot. Always
	// validate it at this compatibility boundary; NetworkExecutor uses the
	// private verified helpers below for its immutable owned state.
	return projection.Acknowledged.Hash()
}

func (projection Projection) verifiedMapHash() (string, error) {
	if projection.acknowledgedHash == "" {
		return projection.Acknowledged.Hash()
	}
	return projection.acknowledgedHash, nil
}

func (projection Projection) Submit(operation model.Operation) (Projection, error) {
	acknowledgedHash, err := projection.acknowledgedMapHash()
	return projection.submitWithHash(operation, acknowledgedHash, err)
}

// submitWithVerifiedOverlay validates a network-owned operation against the
// sparse effective view and appends it without cloning or hashing the full
// acknowledged snapshot. The executor owns the acknowledged snapshot and its
// authority indexes immutably between publications; only the pending slice and
// submitted operation need new storage here.
func (projection Projection) submitWithVerifiedOverlay(operation model.Operation, acknowledgedHash string, hashErr error, authorityTiles map[model.Coord]model.TileState, authorityOwners map[model.StableID]model.Coord) (Projection, error) {
	if hashErr != nil {
		return Projection{}, hashErr
	}
	if operation.ProtocolVersion != model.ProtocolVersion || operation.DocumentID != projection.Acknowledged.DocumentID || operation.EnvironmentHash != projection.Acknowledged.EnvironmentHash {
		return Projection{}, fmt.Errorf("operation is incompatible with acknowledged document")
	}
	if operation.BaseRevision > projection.Acknowledged.Revision {
		return Projection{}, fmt.Errorf("operation base does not match acknowledged revision")
	}
	if err := model.ValidateSHA256("acknowledged map hash", acknowledgedHash); err != nil {
		return Projection{}, err
	}

	coords := make(map[model.Coord]struct{}, len(operation.Changes))
	for _, change := range operation.Changes {
		coords[change.Coord] = struct{}{}
	}
	states, owners := effectiveProjectionOverlay(projection, authorityTiles, authorityOwners, coords, nil, nil)
	affected := make(map[model.Coord]struct{}, len(operation.Changes))
	for _, change := range operation.Changes {
		if _, duplicate := affected[change.Coord]; duplicate {
			return Projection{}, fmt.Errorf("apply speculative operation: duplicate coordinate (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
		affected[change.Coord] = struct{}{}
		if !projection.Acknowledged.Contains(change.Coord) {
			return Projection{}, fmt.Errorf("apply speculative operation: coordinate (%d,%d,%d) is out of bounds", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
		if !states[change.Coord].Equal(change.Before) {
			return Projection{}, fmt.Errorf("apply speculative operation: precondition failed at (%d,%d,%d)", change.Coord.X, change.Coord.Y, change.Coord.Z)
		}
	}

	afterIDs := make(map[model.StableID]struct{})
	for _, change := range operation.Changes {
		for _, prefab := range change.After.Prefabs {
			if err := prefab.StableID.Validate(); err != nil {
				return Projection{}, fmt.Errorf("apply speculative operation: %w", err)
			}
			if _, duplicate := afterIDs[prefab.StableID]; duplicate {
				return Projection{}, fmt.Errorf("apply speculative operation: duplicate stable id %q", prefab.StableID)
			}
			afterIDs[prefab.StableID] = struct{}{}
			owner, overridden := owners[prefab.StableID]
			exists := overridden && owner.present
			if !overridden {
				coord, found := authorityOwners[prefab.StableID]
				owner = effectiveOwner{coord: coord, present: found}
				exists = found
			}
			if exists {
				if _, changingOwner := affected[owner.coord]; !changingOwner {
					return Projection{}, fmt.Errorf("apply speculative operation: stable id %q belongs to unchanged tile", prefab.StableID)
				}
			}
		}
	}

	for _, change := range operation.Changes {
		for _, prefab := range states[change.Coord].Prefabs {
			owners[prefab.StableID] = effectiveOwner{coord: change.Coord, present: false}
		}
	}
	for _, change := range operation.Changes {
		after := model.CloneTileState(change.After)
		states[change.Coord] = after
		for _, prefab := range after.Prefabs {
			owners[prefab.StableID] = effectiveOwner{coord: change.Coord, present: true}
		}
	}

	pending := make([]model.Operation, len(projection.Pending)+1)
	copy(pending, projection.Pending)
	pending[len(projection.Pending)] = model.CloneOperation(operation)
	return Projection{Acknowledged: projection.Acknowledged, Pending: pending, acknowledgedHash: projection.acknowledgedHash}, nil
}

func (projection Projection) submitWithHash(operation model.Operation, acknowledgedHash string, hashErr error) (Projection, error) {
	err := hashErr
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
	// Projection is a public value with an exported mutable acknowledged
	// snapshot. Validate that snapshot before applying an accepted operation so
	// callers cannot use a stale private digest after mutating it.
	_, currentHashErr := projection.acknowledgedMapHash()
	return projection.acceptWithVerifiedCurrent(accepted, authoritativeHash, currentHashErr)
}

// acceptWithVerifiedCurrent is used by NetworkExecutor for its immutable
// executor-owned projection. The caller has already retained the locally
// verified acknowledged hash, so rehashing the current snapshot here would
// add a full-map validation to every receive path.
func (projection Projection) acceptWithVerifiedCurrent(accepted model.AcceptedOperation, authoritativeHash string, currentHashErr error) (Projection, error) {
	if currentHashErr != nil {
		return Projection{}, currentHashErr
	}
	if accepted.Revision != projection.Acknowledged.Revision+1 {
		return Projection{}, fmt.Errorf("accepted revision is %d, want %d", accepted.Revision, projection.Acknowledged.Revision+1)
	}
	if err := model.ValidateSHA256("authoritative map hash", authoritativeHash); err != nil {
		return Projection{}, err
	}
	next, actualHash, err := applyAcceptedSnapshotVerified(projection.Acknowledged, accepted)
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
	return Projection{Acknowledged: next, Pending: rebased, acknowledgedHash: actualHash}, nil
}

func (projection Projection) Reject(rejected protocol.OperationRejectedPayload) (Projection, Conflict, error) {
	acknowledgedHash, err := projection.acknowledgedMapHash()
	return projection.rejectWithHashMode(rejected, acknowledgedHash, err, true)
}

func (projection Projection) rejectWithVerifiedHash(rejected protocol.OperationRejectedPayload, acknowledgedHash string, hashErr error) (Projection, Conflict, error) {
	return projection.rejectWithHashMode(rejected, acknowledgedHash, hashErr, false)
}

func (projection Projection) rejectWithHashMode(rejected protocol.OperationRejectedPayload, acknowledgedHash string, hashErr error, cloneAcknowledged bool) (Projection, Conflict, error) {
	err := hashErr
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
	acknowledged := projection.Acknowledged
	if cloneAcknowledged {
		acknowledged = model.CloneSnapshot(acknowledged)
	}
	return Projection{Acknowledged: acknowledged, Pending: rebased, acknowledgedHash: acknowledgedHash}, Conflict{
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
	next, _, err := applyAcceptedSnapshotVerified(snapshot, accepted)
	return next, err
}

func applyAcceptedSnapshotVerified(snapshot model.Snapshot, accepted model.AcceptedOperation) (model.Snapshot, string, error) {
	if accepted.DocumentID != snapshot.DocumentID || accepted.EnvironmentHash != snapshot.EnvironmentHash {
		return model.Snapshot{}, "", fmt.Errorf("accepted operation is incompatible with acknowledged document")
	}
	next, err := applyOperationUnchecked(snapshot, accepted.Operation)
	if err != nil {
		return model.Snapshot{}, "", fmt.Errorf("apply accepted operation: %w", err)
	}
	next.Revision = accepted.Revision
	hash, err := next.Hash()
	if err != nil {
		return model.Snapshot{}, "", err
	}
	return next, hash, nil
}

func applyOperation(snapshot model.Snapshot, operation model.Operation) (model.Snapshot, error) {
	result, err := applyOperationUnchecked(snapshot, operation)
	if err != nil {
		return model.Snapshot{}, err
	}
	if _, err := result.Hash(); err != nil {
		return model.Snapshot{}, err
	}
	return result, nil
}

func applyOperationUnchecked(snapshot model.Snapshot, operation model.Operation) (model.Snapshot, error) {
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
	return result, nil
}

func rebasePending(acknowledged model.Snapshot, pending []model.Operation) ([]model.Operation, error) {
	rebased := make([]model.Operation, 0, len(pending))
	for _, operation := range pending {
		// These operations have already crossed the transport boundary. Their
		// base and preconditions must stay identical to the original submission.
		rebased = append(rebased, model.CloneOperation(operation))
	}
	return rebased, nil
}

func cloneProjection(projection Projection) Projection {
	clone := Projection{Acknowledged: model.CloneSnapshot(projection.Acknowledged), Pending: make([]model.Operation, len(projection.Pending)), acknowledgedHash: projection.acknowledgedHash}
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
