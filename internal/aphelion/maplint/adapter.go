package maplint

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// AtomFromPrefab converts a map prefab to an Atom using only its explicit
// variable edits (inherited environment defaults are not "var edits"). Values
// are the DM source text the editor already stores, which is what the rules'
// constant parsing expects.
func AtomFromPrefab(p *dmmprefab.Prefab) Atom {
	if p == nil {
		return Atom{}
	}
	a := Atom{Path: p.Path()}
	vars := p.Vars()
	if vars == nil {
		return a
	}
	for _, name := range vars.Iterate() {
		if value, ok := vars.ExplicitValue(name); ok {
			if a.Vars == nil {
				a.Vars = make(map[string]string, vars.Len())
			}
			a.Vars[name] = value
		}
	}
	return a
}

// AtomsFromPrefabs converts a tile's prefabs, preserving order.
func AtomsFromPrefabs(prefabs []*dmmprefab.Prefab) []Atom {
	out := make([]Atom, len(prefabs))
	for i, p := range prefabs {
		out[i] = AtomFromPrefab(p)
	}
	return out
}
