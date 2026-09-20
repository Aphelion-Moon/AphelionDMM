package dmminstance

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

var id uint64

type Instance struct {
	id     uint64
	coord  util.Point
	prefab *dmmprefab.Prefab
	// APHELION EDIT ADDITION START - COLLABORATION
	stableID string
	// APHELION EDIT ADDITION END
}

func (i *Instance) SetPrefab(prefab *dmmprefab.Prefab) {
	i.prefab = prefab
}

func (i Instance) Copy() Instance {
	return Instance{
		id:     i.id,
		coord:  i.coord,
		prefab: i.prefab,
		// APHELION EDIT ADDITION START - COLLABORATION
		stableID: i.stableID,
		// APHELION EDIT ADDITION END
	}
}

// APHELION EDIT ADDITION START - COLLABORATION
func (i Instance) StableID() string {
	return i.stableID
}

func (i *Instance) SetStableID(stableID string) {
	i.stableID = stableID
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - INSTANCE MOVE IDENTITY
// SetCoord preserves both identities and references held by the editor. The
// caller must remove the instance from its old tile and attach it to the new one.
func (i *Instance) SetCoord(coord util.Point) {
	i.coord = coord
}

// APHELION EDIT ADDITION END

func (i Instance) Id() uint64 {
	return i.id
}

func (i Instance) Coord() util.Point {
	return i.coord
}

func (i Instance) Prefab() *dmmprefab.Prefab {
	return i.prefab
}

func New(coord util.Point, prefab *dmmprefab.Prefab) *Instance {
	id++
	return &Instance{
		id,
		coord,
		prefab,
		// APHELION EDIT ADDITION START - COLLABORATION
		"",
		// APHELION EDIT ADDITION END
	}
}
