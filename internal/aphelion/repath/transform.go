package repath

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repath/updatepaths"
)

// Transformer applies a compiled Plan to map content.
type Transformer struct {
	plan      Plan
	known     func(string) bool
	unknown   map[string]struct{}
	resolvers Resolvers
}

// Touches reports whether a path is rewritten by the plan.
func (t *Transformer) Touches(path string) bool {
	if _, ok := t.unknown[path]; !ok {
		return false
	}
	if decision, ok := t.plan.Paths[path]; ok && decision.Kind != Keep {
		return true
	}
	for _, decision := range t.plan.Variants[path] {
		if decision.Kind != Keep {
			return true
		}
	}
	return false
}

// Instance returns the outputs for one instance. resolved is false when the
// decision leaves it unchanged, including when a resolver cannot reach paths
// the environment defines; such instances stay as they are.
func (t *Transformer) Instance(in updatepaths.Instance) (out []updatepaths.Instance, resolved bool, err error) {
	if _, ok := t.unknown[in.Path]; !ok {
		return nil, false, nil
	}
	decision, ok := t.plan.decision(in.Path, VariantKey(in.Vars))
	if !ok || decision.Kind == Keep {
		return nil, false, nil
	}
	if decision.Kind == Delete {
		return nil, true, nil
	}
	out, _, err = resolve(decision, in, t.resolvers, t.known)
	if err != nil || !accepts(decision, in, out, t.known) {
		return nil, false, err
	}
	return out, true, nil
}

// resolve computes an Apply decision's raw outputs and the rules it used.
func resolve(decision Decision, in updatepaths.Instance, resolvers Resolvers, known func(string) bool) ([]updatepaths.Instance, []updatepaths.Pos, error) {
	switch decision.Via {
	case ViaScripts:
		out, trace := resolvers.Scripts.Apply(in, known)
		return out, trace, nil
	case ViaScriptsAll:
		out, trace := resolvers.Scripts.Apply(in, nil)
		return out, trace, nil
	case ViaRemembered:
		return updatepaths.ResolveChain(resolvers.Remembered, in, known)
	}
	if out, matched := decision.Rule.Apply(in); matched {
		return out, []updatepaths.Pos{decision.Rule.Pos}, nil
	}
	return nil, nil, nil
}

// accepts reports whether resolved outputs may replace the instance: every
// output is a known type of the same root, unless crossing roots is allowed.
// An empty result is never accepted; deletion is only a person's decision.
func accepts(decision Decision, in updatepaths.Instance, out []updatepaths.Instance, known func(string) bool) bool {
	if len(out) == 0 {
		return false
	}
	for _, output := range out {
		if !known(output.Path) || !decision.AllowCrossBase && !SameBase(in.Path, output.Path) {
			return false
		}
	}
	return true
}

// IDSource issues stable IDs for additional outputs.
type IDSource func() (model.StableID, error)

// Defaults are the map's default area and turf, restored when a person deletes
// a tile's only area or turf.
type Defaults struct {
	Area, Turf *model.PrefabState
}

type TileConflict struct {
	Coord model.Coord
	Msg   string
}

type Report struct {
	Changed    map[string]int // instances rewritten or deleted, by original path
	Unresolved map[string]int // instances a decision could not resolve
	Tiles      int
	Conflicts  []TileConflict
}

// NewReport returns an empty report for Tile callers.
func NewReport() Report {
	return Report{Changed: map[string]int{}, Unresolved: map[string]int{}}
}

func (r Report) Instances() (total int) {
	for _, count := range r.Changed {
		total += count
	}
	return total
}

// Tile rewrites one tile. The first output of an instance keeps its stable ID;
// further outputs are inserted after it with new IDs.
func (t *Transformer) Tile(coord model.Coord, before model.TileState, ids IDSource, defaults Defaults, report *Report) (model.TileState, bool, error) {
	after := model.TileState{Prefabs: make([]model.PrefabState, 0, len(before.Prefabs))}
	changed, reordered := false, false
	for _, prefab := range before.Prefabs {
		out, resolved, err := t.Instance(updatepaths.Instance{Path: prefab.Path, Vars: prefab.Vars})
		if err != nil {
			return model.TileState{}, false, err
		}
		if !resolved {
			if t.Touches(prefab.Path) {
				report.Unresolved[prefab.Path]++
			}
			after.Prefabs = append(after.Prefabs, prefab)
			continue
		}
		changed = true
		report.Changed[prefab.Path]++
		for n, output := range out {
			state := model.PrefabState{StableID: prefab.StableID, Path: output.Path, Vars: output.Vars}
			if len(state.Vars) == 0 {
				state.Vars = map[string]string{}
			}
			if n != 0 {
				if state.StableID, err = ids(); err != nil {
					return model.TileState{}, false, err
				}
			}
			if Base(output.Path) != Base(prefab.Path) {
				reordered = true
			}
			after.Prefabs = append(after.Prefabs, state)
		}
	}
	if !changed {
		return before, false, nil
	}
	if reordered {
		// Map files list movables, then the turf, then the area.
		slices.SortStableFunc(after.Prefabs, func(a, b model.PrefabState) int { return layerOrder(a.Path) - layerOrder(b.Path) })
	}
	if err := t.restoreDefaults(coord, before, &after, ids, defaults, report); err != nil {
		return model.TileState{}, false, err
	}
	if equalTiles(before, after) {
		return before, false, nil
	}
	return after, true, nil
}

func layerOrder(path string) int {
	switch Base(path) {
	case "/turf":
		return 1
	case "/area":
		return 2
	}
	return 0
}

func count(tile model.TileState) (areas, turfs int) {
	for _, prefab := range tile.Prefabs {
		area, turf := singular(prefab.Path)
		if area {
			areas++
		}
		if turf {
			turfs++
		}
	}
	return areas, turfs
}

// restoreDefaults keeps every tile's area and turf counts. Only a person's
// deletion of the last area or turf is repaired, with the map default.
func (t *Transformer) restoreDefaults(coord model.Coord, before model.TileState, after *model.TileState, ids IDSource, defaults Defaults, report *Report) error {
	beforeAreas, beforeTurfs := count(before)
	afterAreas, afterTurfs := count(*after)
	deleted := func(base string) bool {
		for _, prefab := range before.Prefabs {
			if Base(prefab.Path) != base {
				continue
			}
			if decision, ok := t.plan.decision(prefab.Path, VariantKey(prefab.Vars)); ok && decision.Kind == Delete {
				if _, unknown := t.unknown[prefab.Path]; unknown {
					return true
				}
			}
		}
		return false
	}
	restore := func(base string, beforeCount, afterCount int, fallback *model.PrefabState) error {
		if afterCount == beforeCount {
			return nil
		}
		if afterCount == 0 && beforeCount > 0 && fallback != nil && deleted(base) {
			state := model.PrefabState{Path: fallback.Path, Vars: maps.Clone(fallback.Vars)}
			if state.Vars == nil {
				state.Vars = map[string]string{}
			}
			var err error
			if state.StableID, err = ids(); err != nil {
				return err
			}
			after.Prefabs = append(after.Prefabs, state)
			slices.SortStableFunc(after.Prefabs, func(a, b model.PrefabState) int { return layerOrder(a.Path) - layerOrder(b.Path) })
			return nil
		}
		report.Conflicts = append(report.Conflicts, TileConflict{Coord: coord, Msg: fmt.Sprintf("%s count would change from %d to %d", base, beforeCount, afterCount)})
		return nil
	}
	if err := restore("/area", beforeAreas, afterAreas, defaults.Area); err != nil {
		return err
	}
	return restore("/turf", beforeTurfs, afterTurfs, defaults.Turf)
}

func equalTiles(a, b model.TileState) bool {
	return slices.EqualFunc(a.Prefabs, b.Prefabs, func(x, y model.PrefabState) bool {
		return x.StableID == y.StableID && x.Path == y.Path && maps.Equal(x.Vars, y.Vars)
	})
}

// BuildChanges rewrites every visited tile. It fails closed: any conflict or
// error returns no changes. visit stops early when its callback returns false.
func BuildChanges(ctx context.Context, visit func(func(model.Coord) bool), read func(model.Coord) (model.TileState, bool), t *Transformer, ids IDSource, defaults Defaults) ([]model.TileChange, Report, error) {
	report := NewReport()
	var changes []model.TileChange
	var failure error
	visit(func(coord model.Coord) bool {
		if failure = ctx.Err(); failure != nil {
			return false
		}
		before, ok := read(coord)
		if !ok {
			failure = fmt.Errorf("tile (%d,%d,%d) is unavailable", coord.X, coord.Y, coord.Z)
			return false
		}
		after, changed, err := t.Tile(coord, before, ids, defaults, &report)
		if err != nil {
			failure = err
			return false
		}
		if changed {
			changes = append(changes, model.TileChange{Coord: coord, Before: before, After: after})
		}
		return true
	})
	if failure != nil {
		return nil, report, failure
	}
	report.Tiles = len(changes)
	if len(report.Conflicts) != 0 {
		first := report.Conflicts[0]
		return nil, report, fmt.Errorf("%d tiles would lose or gain an area or turf, first at (%d,%d,%d): %s", len(report.Conflicts), first.Coord.X, first.Coord.Y, first.Coord.Z, first.Msg)
	}
	return changes, report, nil
}
