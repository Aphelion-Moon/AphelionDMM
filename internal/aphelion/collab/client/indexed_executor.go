package client

import (
	"fmt"
	"maps"
	"slices"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// The executor owns these indexes under its mutex. Tile indexes are immutable
// once captured; only appending a previously absent coordinate detaches them.
// Identity owners never escape the mutex. Published tile payloads stay immutable.
func (network *NetworkExecutor) indexAcknowledgedLocked(hash string) {
	network.projection.acknowledgedHash = hash
	network.tileIndexes = make(map[model.Coord]int, len(network.projection.Acknowledged.Tiles))
	for index, tile := range network.projection.Acknowledged.Tiles {
		network.tileIndexes[tile.Coord] = index
	}
	network.authorityTiles, network.authorityOwners = buildAuthorityIndex(network.projection.Acknowledged)
}

func (network *NetworkExecutor) acknowledgedTileLocked(coord model.Coord) model.TileState {
	if index, found := network.tileIndexes[coord]; found {
		return network.projection.Acknowledged.Tiles[index].State
	}
	return model.TileState{}
}

func (network *NetworkExecutor) rebuildVisibleLocked() {
	overlay := projectionOverlay{network: network}
	for _, operation := range network.projection.Pending {
		_ = overlay.apply(operation)
	}
	network.visibleTiles = overlay.tiles
	network.visibleCoords = overlay.coords
}

// projectionOverlay holds only speculative coordinates and ownership changes.
// Missing owners in overrides fall back to authority; a zero coordinate is a
// tombstone for an identity removed by an earlier speculative operation.
type projectionOverlay struct {
	network *NetworkExecutor
	tiles   map[model.Coord]model.TileState
	owners  map[model.StableID]model.Coord
	coords  []model.Coord
}

func (overlay *projectionOverlay) tile(coord model.Coord) model.TileState {
	if state, found := overlay.tiles[coord]; found {
		return state
	}
	return overlay.network.acknowledgedTileLocked(coord)
}

func (overlay *projectionOverlay) apply(operation model.Operation) error {
	affected := make(map[model.Coord]struct{}, len(operation.Changes))
	for _, change := range operation.Changes {
		if _, duplicate := affected[change.Coord]; duplicate || !overlay.network.projection.Acknowledged.Contains(change.Coord) {
			return fmt.Errorf("duplicate or out-of-bounds coordinate %v", change.Coord)
		}
		affected[change.Coord] = struct{}{}
		if !overlay.tile(change.Coord).Equal(change.Before) {
			return fmt.Errorf("precondition failed at %v", change.Coord)
		}
	}
	afterIDs := make(map[model.StableID]struct{})
	for _, change := range operation.Changes {
		for _, prefab := range change.After.Prefabs {
			if err := prefab.StableID.Validate(); err != nil {
				return err
			}
			if _, duplicate := afterIDs[prefab.StableID]; duplicate {
				return fmt.Errorf("duplicate stable id %q", prefab.StableID)
			}
			afterIDs[prefab.StableID] = struct{}{}
			owner, overridden := overlay.owners[prefab.StableID]
			if !overridden {
				owner = overlay.network.authorityOwners[prefab.StableID]
			}
			if owner != (model.Coord{}) {
				if _, moving := affected[owner]; !moving {
					return fmt.Errorf("stable id %q belongs to an unchanged tile", prefab.StableID)
				}
			}
		}
	}
	if overlay.tiles == nil {
		overlay.tiles = make(map[model.Coord]model.TileState)
		overlay.owners = make(map[model.StableID]model.Coord)
	}
	for _, change := range operation.Changes {
		for _, prefab := range overlay.tile(change.Coord).Prefabs {
			overlay.owners[prefab.StableID] = model.Coord{}
		}
	}
	for _, change := range operation.Changes {
		if _, found := overlay.tiles[change.Coord]; !found {
			overlay.coords = append(overlay.coords, change.Coord)
		}
		overlay.tiles[change.Coord] = change.After
		for _, prefab := range change.After.Prefabs {
			overlay.owners[prefab.StableID] = change.Coord
		}
	}
	return nil
}

func (network *NetworkExecutor) submitProjectionLocked(operation model.Operation) error {
	verifiedHash, hashErr := network.projection.verifiedMapHash()
	baseHash := network.verifiedHistory[operation.BaseRevision]
	if operation.BaseRevision == network.projection.Acknowledged.Revision {
		baseHash = verifiedHash
	}
	if operation.BaseRevision > network.projection.Acknowledged.Revision || baseHash != operation.BaseMapHash {
		return fmt.Errorf("operation base does not match a locally verified acknowledged revision")
	}
	projection, err := network.projection.submitWithVerifiedOverlay(operation, verifiedHash, hashErr, network.authorityTiles, network.authorityOwners)
	if err != nil {
		return err
	}
	network.projection = projection
	network.rebuildVisibleLocked()
	return nil
}

func (network *NetworkExecutor) acceptProjectionLocked(accepted model.AcceptedOperation, hash string) error {
	base := network.projection.Acknowledged
	if accepted.Revision != base.Revision+1 {
		return fmt.Errorf("accepted revision is %d, want %d", accepted.Revision, base.Revision+1)
	}
	if err := model.ValidateSHA256("authoritative map hash", hash); err != nil {
		return err
	}
	if accepted.DocumentID != base.DocumentID || accepted.EnvironmentHash != base.EnvironmentHash {
		return fmt.Errorf("accepted operation is incompatible with acknowledged document")
	}
	overlay := projectionOverlay{network: network}
	if err := overlay.apply(accepted.Operation); err != nil {
		return fmt.Errorf("apply accepted operation: %w", err)
	}
	next := base
	next.Tiles = slices.Clone(base.Tiles)
	indexes := network.tileIndexes
	appended := false
	for _, change := range accepted.Changes {
		state := model.CloneTileState(change.After)
		if index, found := indexes[change.Coord]; found {
			next.Tiles[index].State = state
		} else {
			if !appended {
				indexes = maps.Clone(indexes)
				appended = true
			}
			indexes[change.Coord] = len(next.Tiles)
			next.Tiles = append(next.Tiles, model.Tile{Coord: change.Coord, State: state})
		}
	}
	next.Revision = accepted.Revision
	actual, err := next.Hash()
	if err != nil {
		return err
	}
	if actual != hash {
		return fmt.Errorf("authoritative map hash mismatch at revision %d", accepted.Revision)
	}
	// Nothing above mutates authority or indexes: even a bad digest leaves all
	// captures, pending drafts, and identity ownership at the previous revision.
	remaining := make([]model.Operation, 0, len(network.projection.Pending))
	for _, pending := range network.projection.Pending {
		if pending.OperationID != accepted.OperationID {
			remaining = append(remaining, pending)
		}
	}
	network.projection = Projection{Acknowledged: next, Pending: remaining, acknowledgedHash: hash}
	network.tileIndexes = indexes
	network.applyAuthorityChanges(accepted.Changes)
	network.rebuildVisibleLocked()
	return nil
}

func (network *NetworkExecutor) rejectProjectionLocked(rejected protocol.OperationRejectedPayload) (Conflict, error) {
	verifiedHash, hashErr := network.projection.verifiedMapHash()
	projection, conflict, err := network.projection.rejectWithVerifiedHash(rejected, verifiedHash, hashErr)
	if err != nil {
		return Conflict{}, err
	}
	network.projection = projection
	network.rebuildVisibleLocked()
	return conflict, nil
}
