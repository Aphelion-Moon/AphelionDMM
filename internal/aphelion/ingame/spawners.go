package ingame

import (
	"regexp"
	"sync/atomic"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// Structure spawners (code/game/objects/effects/spawners/structure.dm) create
// every type in spawn_list on their tile at Initialize, so the mapper icon of a
// window spawner stands in for a grille and a smoothed window.

const structureSpawnerRoot = "/obj/effect/spawner/structure"

// dynamicSpawner matches spawners whose Initialize picks spawn_list from dir
// (hollow window ends, middles and directionals) or otherwise departs from the
// static list; they keep their mapper icon.
var dynamicSpawner = regexp.MustCompile(`^/obj/effect/spawner/structure/(flipped_table|window/hollow/(.+/)?(end|middle|directional))(/|$)`)

var typePathRe = regexp.MustCompile(`/[\w/]+`)

var types atomic.Pointer[func(path string) *dmvars.Variables]

// SetTypes installs the loaded environment's type variables (nil clears).
func SetTypes(lookup func(path string) *dmvars.Variables) {
	if lookup == nil {
		types.Store(nil)
	} else {
		types.Store(&lookup)
	}
	resetCache()
}

func typeVars(path string) *dmvars.Variables {
	if lookup := types.Load(); lookup != nil {
		return (*lookup)(path)
	}
	return nil
}

// spawnedPrefabs returns the prefabs a static structure spawner creates, built
// from the environment's type defaults, or nil.
func spawnedPrefabs(path string, vars *dmvars.Variables) []*dmmprefab.Prefab {
	if !isType(path, structureSpawnerRoot) || dynamicSpawner.MatchString(path) || vars == nil {
		return nil
	}
	raw, ok := vars.Value("spawn_list")
	if !ok || raw == "null" {
		return nil
	}
	var out []*dmmprefab.Prefab
	for _, spawned := range typePathRe.FindAllString(raw, -1) {
		parent := typeVars(spawned)
		if parent == nil {
			return nil // an unknown type: do not guess
		}
		out = append(out, dmmprefab.New(dmmprefab.IdNone, spawned, dmvars.FromParent(parent)))
	}
	return out
}

// Part is one atom a spawner creates, with its in-game appearance.
type Part struct {
	Prefab   *dmmprefab.Prefab
	Override Override
}

// Expand returns what a structure spawner at (x, y, z) looks like in game, in
// spawn order, or nil when prefab is not an expandable spawner.
func Expand(m Map, x, y, z int, prefab *dmmprefab.Prefab) []Part {
	if prefab == nil {
		return nil
	}
	a := factsOf(prefab)
	if len(a.spawned) == 0 {
		return nil
	}
	parts := make([]Part, 0, len(a.spawned))
	for _, p := range a.spawned {
		o, ok := Resolve(m, x, y, z, p)
		if !ok {
			vars := p.Vars()
			o = Override{IconState: vars.TextV("icon_state", ""), Dir: vars.IntV("dir", south)}
		}
		if o.Icon == "" {
			o.Icon = p.Vars().TextV("icon", "")
		}
		parts = append(parts, Part{Prefab: p, Override: o})
	}
	return parts
}
