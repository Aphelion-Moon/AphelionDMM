package engine

import (
	"reflect"
	"sdmm/internal/aphelion/collab/model"
)

// Metadata is a coherent authority header without tile payloads. Like other
// Document methods, reads belong to its serialized owner. The canonical hash
// is reused until a local edit invalidates it.
type Metadata struct {
	ProtocolVersion uint16
	SchemaVersion   uint16
	DocumentID      model.DocumentID
	Revision        model.Revision
	MapHash         string
}

func (document *Document) Metadata() (Metadata, error) {
	if err := document.ensureHash(); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		ProtocolVersion: document.snapshot.ProtocolVersion,
		SchemaVersion:   document.snapshot.SchemaVersion,
		DocumentID:      document.snapshot.DocumentID,
		Revision:        document.snapshot.Revision,
		MapHash:         document.mapHash,
	}, nil
}

func (document *Document) Revision() model.Revision { return document.snapshot.Revision }

// EqualSnapshot compares borrowed input without disposable payload copies.
// Preserve CloneSnapshot's normalization of a nil tile table; prefab ordering
// and nil versus empty variable maps still compare exactly.
func (document *Document) EqualSnapshot(snapshot model.Snapshot) bool {
	if snapshot.Tiles == nil {
		snapshot.Tiles = []model.Tile{}
	}
	return reflect.DeepEqual(document.snapshot, snapshot)
}

// Tile returns detached state, including an empty state for a missing tile.
func (document *Document) Tile(coord model.Coord) model.TileState {
	if index, found := document.tileIndexes[coord]; found {
		return model.CloneTileState(document.snapshot.Tiles[index].State)
	}
	return model.TileState{}
}
