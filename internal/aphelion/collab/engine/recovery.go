package engine

import (
	"fmt"
	"reflect"

	"sdmm/internal/aphelion/collab/model"
)

// RecoveryState is private persistence data, never a client-supplied snapshot.
// Operations and hashes retain the complete validation history, including the
// prefix compacted out of the public reconnect replay.
type RecoveryState struct {
	Snapshot     model.Snapshot
	SnapshotHash string
	Operations   []model.AcceptedOperation
	Hashes       map[model.Revision]string
	HeadRevision model.Revision
	HeadHash     string
}

func (state RecoveryState) Restore() (*Document, error) {
	return state.RestoreAt(state.HeadRevision)
}

// RestoreAt also verifies the suffix beyond revision before returning an older
// captured state, so snapshot publication cannot hide corrupt acknowledged data.
func (state RecoveryState) RestoreAt(revision model.Revision) (*Document, error) {
	document, err := NewDocument(state.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("open stored snapshot: %w", err)
	}
	if document.mapHash != state.SnapshotHash || state.Hashes[state.Snapshot.Revision] != document.mapHash {
		return nil, fmt.Errorf("stored snapshot hash differs from retained revision %d", state.Snapshot.Revision)
	}
	if revision < state.Snapshot.Revision || revision > state.HeadRevision {
		return nil, fmt.Errorf("snapshot revision %d is outside retained range %d through %d", revision, state.Snapshot.Revision, state.HeadRevision)
	}
	firstRevision := state.Snapshot.Revision
	for retainedRevision, hash := range state.Hashes {
		if retainedRevision < firstRevision {
			firstRevision = retainedRevision
		}
		if retainedRevision > state.HeadRevision {
			return nil, fmt.Errorf("hash beyond durable head")
		}
		if err := model.ValidateSHA256("retained revision hash", hash); err != nil {
			return nil, err
		}
	}
	if state.HeadRevision < firstRevision || uint64(state.HeadRevision-firstRevision) != uint64(len(state.Operations)) || uint64(len(state.Hashes)) != uint64(len(state.Operations))+1 {
		return nil, fmt.Errorf("replay revision history does not reach durable head %d", state.HeadRevision)
	}
	// Restore only hashes already authoritative at the snapshot. Future bases
	// must not become valid merely because their rows were read in this batch.
	for retainedRevision, hash := range state.Hashes {
		if retainedRevision <= state.Snapshot.Revision {
			document.hashes[retainedRevision] = hash
		}
	}
	latest := make(map[model.Coord]model.TileState)
	var result *Document
	for index, accepted := range state.Operations {
		want := firstRevision + model.Revision(index) + 1
		if accepted.Revision != want || accepted.DocumentID != state.Snapshot.DocumentID {
			return nil, fmt.Errorf("replay revision %d has inconsistent identity/order; want %d", accepted.Revision, want)
		}
		if _, exists := document.accepted[accepted.OperationID]; exists {
			return nil, fmt.Errorf("duplicate stored operation identity")
		}
		if accepted.Revision <= state.Snapshot.Revision {
			if accepted.BaseRevision >= accepted.Revision {
				return nil, fmt.Errorf("stored operation names a future base")
			}
			// Validate the retained operation's form, base identity and inverse
			// relationship without applying it a second time to snapshot map data.
			before := state.Snapshot
			before.Revision = accepted.Revision - 1
			before.Tiles = make([]model.Tile, len(accepted.Changes))
			for i, change := range accepted.Changes {
				if prior, exists := latest[change.Coord]; exists && !prior.Equal(change.Before) {
					return nil, fmt.Errorf("stored history precondition differs at revision %d", accepted.Revision)
				}
				before.Tiles[i] = model.Tile{Coord: change.Coord, State: change.Before}
			}
			validator, err := NewDocument(before)
			if err != nil {
				return nil, fmt.Errorf("stored history before-values: %w", err)
			}
			validator.hashes, validator.accepted, validator.inverted = document.hashes, document.accepted, document.inverted
			normalized, _, _, _, err := validator.validate(accepted.Operation)
			if err != nil {
				return nil, fmt.Errorf("stored history revision %d: %w", accepted.Revision, err)
			}
			if !reflect.DeepEqual(normalized, accepted.Operation) {
				return nil, fmt.Errorf("stored history is not normalized")
			}
			document.accepted[accepted.OperationID] = model.CloneAcceptedOperation(accepted)
			if accepted.InverseOf != nil {
				document.inverted[*accepted.InverseOf] = accepted.OperationID
			}
			for _, change := range accepted.Changes {
				latest[change.Coord] = change.After
			}
		} else {
			if result == nil && document.snapshot.Revision == revision {
				result = document.Clone()
			}
			verified, err := document.Apply(accepted.Operation, accepted.AcceptedAt)
			if err != nil {
				return nil, fmt.Errorf("replay revision %d: %w", accepted.Revision, err)
			}
			if !reflect.DeepEqual(verified, accepted) || document.mapHash != state.Hashes[accepted.Revision] {
				return nil, fmt.Errorf("replay revision %d differs from stored operation/hash", accepted.Revision)
			}
		}
	}
	// Every last change in the compacted prefix must agree with the snapshot.
	tiles := make(map[model.Coord]model.TileState, len(state.Snapshot.Tiles))
	for _, tile := range state.Snapshot.Tiles {
		tiles[tile.Coord] = tile.State
	}
	for coord, after := range latest {
		if current, exists := tiles[coord]; !exists || !current.Equal(after) {
			return nil, fmt.Errorf("stored snapshot differs from compacted history")
		}
	}
	if document.snapshot.Revision != state.HeadRevision || document.mapHash != state.HeadHash || state.Hashes[state.HeadRevision] != state.HeadHash {
		return nil, fmt.Errorf("recovered document differs from durable head")
	}
	if result != nil {
		return result, nil
	}
	return document, nil
}
