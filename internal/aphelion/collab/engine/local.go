package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"sdmm/internal/aphelion/collab/model"
)

// LocalRequest is an internal owner request, never a wire operation. DocumentID
// and its version token fence lifetime and state; before-values still validate
// the complete edit. Only an unshared document may use this entry point.
type LocalRequest struct {
	Version LocalVersion
	Changes []model.TileChange
}

// LocalVersion includes an opaque owner identity so a stale request cannot
// target a replacement/branch with coincident document ID and revision.
type LocalVersion struct {
	DocumentID model.DocumentID
	Revision   model.Revision
	owner      *Document
}

type LocalAcceptance struct {
	DocumentID model.DocumentID
	Revision   model.Revision
	Changes    []model.TileChange
}

// NewUnsharedDocument explicitly selects local ownership. NewDocument remains
// the session constructor and cannot issue a local token. Promotion constructs
// a new session document from a canonical snapshot, never toggles this flag.
func NewUnsharedDocument(snapshot model.Snapshot) (*Document, error) {
	document, err := NewDocument(snapshot)
	if err == nil {
		document.unshared = true
	}
	return document, err
}

// CachedHash reports availability explicitly. Local edits invalidate it; a
// previous revision's digest is never returned as current authority.
func (document *Document) CachedHash() (string, bool) {
	return document.mapHash, document.mapHash != ""
}

func (document *Document) ensureHash() error {
	if document.mapHash != "" {
		return nil
	}
	digest, err := document.snapshot.Hash()
	if err != nil {
		return err
	}
	document.mapHash = digest
	document.hashes[document.snapshot.Revision] = digest
	return nil
}

// LocalVersion returns metadata without capturing document tiles or hashing.
func (document *Document) LocalVersion() LocalVersion {
	if !document.unshared {
		return LocalVersion{}
	}
	return LocalVersion{DocumentID: document.snapshot.DocumentID, Revision: document.snapshot.Revision, owner: document}
}

func (document *Document) ApplyLocal(ctx context.Context, request LocalRequest) (LocalAcceptance, error) {
	if err := ctx.Err(); err != nil {
		return LocalAcceptance{}, err
	}
	if !document.unshared || request.Version.owner != document || request.Version.DocumentID != document.snapshot.DocumentID {
		return LocalAcceptance{}, document.reject(CodeWrongDocument, fmt.Errorf("local document lifetime changed"))
	}
	if request.Version.Revision != document.snapshot.Revision {
		return LocalAcceptance{}, document.reject(CodeUnknownBaseRevision, fmt.Errorf("local revision changed"))
	}
	owned, err := cloneOperationContext(ctx, model.Operation{Changes: request.Changes})
	if err != nil {
		return LocalAcceptance{}, err
	}
	sortChanges(owned.Changes)
	if err := document.validateChanges(ctx, owned.Changes); err != nil {
		return LocalAcceptance{}, err
	}
	changes := slices.DeleteFunc(owned.Changes, func(change model.TileChange) bool { return change.Before.Equal(change.After) })
	if len(changes) == 0 {
		return LocalAcceptance{DocumentID: request.Version.DocumentID, Revision: request.Version.Revision}, nil
	}
	if document.snapshot.Revision == ^model.Revision(0) {
		return LocalAcceptance{}, document.reject(CodeInvalidOperation, fmt.Errorf("document revision exhausted"))
	}
	if err := ctx.Err(); err != nil {
		return LocalAcceptance{}, err
	}
	// Cloned engine branches share immutable payloads, but never mutable tables.
	// Ordinary local edits keep their uniquely owned table between operations.
	if document.sharedTiles {
		document.snapshot.Tiles = slices.Clone(document.snapshot.Tiles)
		document.sharedTiles = false
	}
	document.installChanges(changes)
	document.snapshot.Revision++
	document.mapHash = ""
	return LocalAcceptance{DocumentID: request.Version.DocumentID, Revision: document.snapshot.Revision, Changes: model.CloneOperation(model.Operation{Changes: changes}).Changes}, nil
}

func (document *Document) indexState() {
	document.tileIndexes = make(map[model.Coord]int, len(document.snapshot.Tiles))
	document.identityOwners = make(map[model.StableID]model.Coord)
	for index, tile := range document.snapshot.Tiles {
		document.tileIndexes[tile.Coord] = index
		for _, prefab := range tile.State.Prefabs {
			document.identityOwners[prefab.StableID] = tile.Coord
		}
	}
}

// validateChanges is shared by internal local edits and validated wire edits.
// It stages identity ownership for the entire changed union without publishing
// any index changes, so moving an ID between tiles is valid in either order.
func (document *Document) validateChanges(ctx context.Context, changes []model.TileChange) error {
	count, err := document.snapshot.CellCount()
	if err != nil || len(changes) == 0 || len(changes) > count {
		return document.reject(CodeInvalidOperation, fmt.Errorf("invalid tile change count %d", len(changes)))
	}
	touched := make(map[model.Coord]struct{}, len(changes))
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, exists := touched[change.Coord]; exists {
			return document.reject(CodeInvalidOperation, fmt.Errorf("duplicate change coordinate %v", change.Coord))
		}
		touched[change.Coord] = struct{}{}
		if !document.snapshot.Contains(change.Coord) {
			return document.reject(CodeOutOfBounds, fmt.Errorf("coordinate %v is outside document", change.Coord))
		}
		current := model.TileState{}
		if index, exists := document.tileIndexes[change.Coord]; exists {
			current = document.snapshot.Tiles[index].State
		}
		if !current.Equal(change.Before) {
			return document.reject(CodePreconditionFailed, fmt.Errorf("tile precondition failed at %v", change.Coord))
		}
	}
	assigned := make(map[model.StableID]struct{})
	for _, change := range changes {
		for _, prefab := range change.After.Prefabs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := prefab.StableID.Validate(); err != nil {
				return document.reject(CodeInvalidOperation, err)
			}
			if _, exists := assigned[prefab.StableID]; exists {
				return document.reject(CodeInvalidOperation, fmt.Errorf("duplicate stable id %q", prefab.StableID))
			}
			if owner, exists := document.identityOwners[prefab.StableID]; exists {
				if _, changed := touched[owner]; !changed {
					return document.reject(CodeInvalidOperation, fmt.Errorf("stable id %q belongs to unchanged tile", prefab.StableID))
				}
			}
			assigned[prefab.StableID] = struct{}{}
		}
	}
	return nil
}

// installChanges cannot fail: validation and cancellation precede publication.
func (document *Document) installChanges(changes []model.TileChange) {
	document.ownIndexes()
	document.updateIdentityOwners(changes)
	for _, change := range changes {
		if index, exists := document.tileIndexes[change.Coord]; exists {
			document.snapshot.Tiles[index].State = change.After
		} else {
			document.tileIndexes[change.Coord] = len(document.snapshot.Tiles)
			document.snapshot.Tiles = append(document.snapshot.Tiles, model.Tile{Coord: change.Coord, State: change.After})
		}
	}
}

func (document *Document) ownIndexes() {
	if document.sharedIndexes {
		document.tileIndexes = maps.Clone(document.tileIndexes)
		document.identityOwners = maps.Clone(document.identityOwners)
		document.sharedIndexes = false
	}
}

func (document *Document) updateIdentityOwners(changes []model.TileChange) {
	for _, change := range changes {
		for _, prefab := range change.Before.Prefabs {
			delete(document.identityOwners, prefab.StableID)
		}
	}
	for _, change := range changes {
		for _, prefab := range change.After.Prefabs {
			document.identityOwners[prefab.StableID] = change.Coord
		}
	}
}

func sortChanges(changes []model.TileChange) {
	slices.SortFunc(changes, func(a, b model.TileChange) int {
		for _, pair := range [][2]int{{a.Coord.Z, b.Coord.Z}, {a.Coord.Y, b.Coord.Y}, {a.Coord.X, b.Coord.X}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
}
