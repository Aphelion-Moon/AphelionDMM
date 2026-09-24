package dmmprefab

import (
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	"sdmm/internal/aphelion/prefabidentity"
	// APHELION EDIT ADDITION END
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const (
	IdNone  = 0
	IdStage = 1 // Prefabs with this ID are temporal by their nature.
)

type Prefab struct {
	id   uint64
	path string
	vars *dmvars.Variables
	// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
	contentKey string
	// APHELION EDIT ADDITION END
}

func New(id uint64, path string, vars *dmvars.Variables) *Prefab {
	// APHELION EDIT CHANGE - SEALED PREFAB CONTENT - ORIGINAL: return &Prefab{id, path, vars}
	return &Prefab{id: id, path: path, vars: vars}
}

// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
func (p Prefab) IsStaged() bool   { return p.id == IdStage }
func (p Prefab) IsInterned() bool { return p.contentKey != "" }

// Interned seals explicit contents before caching their structural key and ID.
// Plain/staged candidates remain editable through the existing copy-on-write API.
func (p *Prefab) Interned() *Prefab {
	if p.IsInterned() {
		return p
	}
	copy := *p
	copy.vars = p.vars.FrozenCopy()
	copy.contentKey = prefabidentity.Key(copy.path, copy.vars)
	if copy.id == IdNone {
		copy.id = util.Djb2(copy.contentKey)
		if copy.id <= IdStage {
			copy.id += 2
		}
	}
	return &copy
}
func (p Prefab) ContentKey() string {
	if p.contentKey != "" {
		return p.contentKey
	}
	return prefabidentity.Key(p.path, p.vars)
}
func (p Prefab) WithLocalID(id uint64) *Prefab { p.id = id; return &p }

// APHELION EDIT ADDITION END

func (p Prefab) Id() uint64 {
	if p.id == IdNone {
		p.id = Id(p.path, p.vars)
	}
	return p.id
}

func (p Prefab) Path() string {
	return p.path
}

func (p Prefab) Vars() *dmvars.Variables {
	return p.vars
}

// Stage returns a copy of the prefab with the ID equals to IdStage. Staged prefabs are temporal.
// They are needed when creating/modifying existing prefab, without persisting of the temporal object.
func (p Prefab) Stage() Prefab {
	return Prefab{
		id:   IdStage,
		path: p.path,
		vars: p.vars,
	}
}

func Id(path string, vars *dmvars.Variables) uint64 {
	/* APHELION EDIT REMOVAL START - CONTENT IDENTITY
	snap := path
	if vars != nil {
		for _, name := range vars.Iterate() {
			if value, ok := vars.Value(name); ok {
				snap += name + value
			} else {
				snap += name
			}
		}
	}
	return util.Djb2(snap)
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	id := util.Djb2(prefabidentity.Key(path, vars))
	if id <= IdStage {
		id += 2
	}
	return id
	// APHELION EDIT ADDITION END
}
