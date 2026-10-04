package engine

import "sdmm/internal/aphelion/collab/model"

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

// Tile returns detached state, including an empty state for a missing tile.
func (document *Document) Tile(coord model.Coord) model.TileState {
	if index, found := document.tileIndexes[coord]; found {
		return model.CloneTileState(document.snapshot.Tiles[index].State)
	}
	return model.TileState{}
}
