package maplint

import (
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// EnvironmentTypes adapts a loaded environment for subtype promotion. The
// environment is immutable once loaded, so the adapter may be used from a
// worker; its subtype cache is not shared between goroutines.
func EnvironmentTypes(env *dmenv.Dme) TypeTree {
	if env == nil {
		return nil
	}
	return &envTypes{env: env, subtypes: map[string][]string{}}
}

type envTypes struct {
	env      *dmenv.Dme
	subtypes map[string][]string
}

func (t *envTypes) Subtypes(path string) []string {
	if cached, ok := t.subtypes[path]; ok {
		return cached
	}
	var out []string
	var walk func(string)
	walk = func(p string) {
		object := t.env.Objects[p]
		if object == nil {
			return
		}
		for _, child := range object.DirectChildren {
			out = append(out, child)
			walk(child)
		}
	}
	walk(path)
	t.subtypes[path] = out
	return out
}

func (t *envTypes) OwnVars(path string) []string {
	if object := t.env.Objects[path]; object != nil && object.Vars != nil {
		return object.Vars.Iterate()
	}
	return nil
}

func (t *envTypes) Value(path, name string) (string, bool) {
	if object := t.env.Objects[path]; object != nil && object.Vars != nil {
		return object.Vars.Value(name)
	}
	return "", false
}

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
