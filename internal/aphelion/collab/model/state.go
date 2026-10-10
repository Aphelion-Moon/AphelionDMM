package model

const (
	ProtocolVersion uint16 = 2
	SchemaVersion   uint16 = 1
)

type Revision uint64

type Coord struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}

type PrefabState struct {
	StableID StableID          `json:"stable_id"`
	Path     string            `json:"path"`
	Vars     map[string]string `json:"vars"`
}

type TileState struct {
	Prefabs []PrefabState `json:"prefabs"`
}

type Tile struct {
	Coord Coord     `json:"coord"`
	State TileState `json:"state"`
}

type Snapshot struct {
	ProtocolVersion uint16     `json:"protocol_version"`
	SchemaVersion   uint16     `json:"schema_version"`
	DocumentID      DocumentID `json:"document_id"`
	Revision        Revision   `json:"revision"`
	EnvironmentHash string     `json:"environment_hash"`
	MaxX            int        `json:"max_x"`
	MaxY            int        `json:"max_y"`
	MaxZ            int        `json:"max_z"`
	Tiles           []Tile     `json:"tiles"`
}
