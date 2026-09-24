package editing

import (
	"context"
	"fmt"
	"sort"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// TransformModelSelection works on immutable authority values. Environment
// parents are read-only; no live instances or global prefab storage are used.
func TransformModelSelection(ctx context.Context, selection Selection, transform PlacementTransform, visible func(string) bool, lookup func(model.Coord) (model.TileState, bool), parent func(string) *dmvars.Variables, defaults []model.PrefabState) ([]model.TileChange, error) {
	if transform < PlacementRotateRight || transform > PlacementMirrorVertical || selection.Len() == 0 {
		return nil, fmt.Errorf("invalid selection transform")
	}
	area := selection.Bounds()
	w, h := int(area.X2-area.X1+1), int(area.Y2-area.Y1+1)
	before := make(map[model.Coord]model.TileState)
	incoming := make(map[model.Coord][]model.PrefabState)
	cache := make(map[string]map[string]string)
	var failure error
	load := func(c model.Coord) bool {
		if _, ok := before[c]; ok {
			return true
		}
		state, ok := lookup(c)
		if !ok {
			failure = fmt.Errorf("transformed selection leaves the map")
			return false
		}
		before[c] = state
		return true
	}
	selection.Visit(func(p util.Point) {
		if failure != nil {
			return
		}
		if failure = ctx.Err(); failure != nil {
			return
		}
		x, y := p.X-int(area.X1), p.Y-int(area.Y1)
		switch transform {
		case PlacementRotateRight:
			x, y = y, w-1-x
		case PlacementRotateLeft:
			x, y = h-1-y, x
		case PlacementMirrorHorizontal:
			x = w - 1 - x
		case PlacementMirrorVertical:
			y = h - 1 - y
		}
		source := model.Coord{X: p.X, Y: p.Y, Z: p.Z}
		destination := model.Coord{X: int(area.X1) + x, Y: int(area.Y1) + y, Z: p.Z}
		if !load(source) || !load(destination) {
			return
		}
		for _, state := range before[source].Prefabs {
			if !visible(state.Path) {
				continue
			}
			variables := &dmvars.MutableVariables{}
			for name, value := range state.Vars {
				variables.Put(name, value)
			}
			vars := variables.ToImmutable()
			if parent != nil {
				vars.LinkParent(parent(state.Path))
			}
			prefab := dmmprefab.New(dmmprefab.IdNone, state.Path, vars)
			key := prefab.ContentKey()
			converted, found := cache[key]
			if !found {
				var result *dmmprefab.Prefab
				switch transform {
				case PlacementRotateRight, PlacementRotateLeft:
					result, failure = rotatePrefab(prefab, transform == PlacementRotateRight)
				case PlacementMirrorHorizontal:
					result, failure = mirrorPrefab(prefab, MirrorHorizontal)
				case PlacementMirrorVertical:
					result, failure = mirrorPrefab(prefab, MirrorVertical)
				}
				if failure != nil {
					return
				}
				converted = make(map[string]string, result.Vars().Len())
				for _, name := range result.Vars().Iterate() {
					converted[name], _ = result.Vars().ExplicitValue(name)
				}
				cache[key] = converted
			}
			state.Vars = converted
			incoming[destination] = append(incoming[destination], state)
		}
	})
	if failure != nil {
		return nil, failure
	}
	coords := make([]model.Coord, 0, len(before))
	for c := range before {
		coords = append(coords, c)
	}
	sort.Slice(coords, func(i, j int) bool {
		a, b := coords[i], coords[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	changes := make([]model.TileChange, 0, len(coords))
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state := before[c]
		after := model.TileState{}
		for _, prefab := range state.Prefabs {
			if !visible(prefab.Path) {
				after.Prefabs = append(after.Prefabs, prefab)
			}
		}
		after.Prefabs = append(after.Prefabs, incoming[c]...)
		for _, fallback := range defaults {
			found := false
			for _, prefab := range after.Prefabs {
				if ChannelForPath(prefab.Path) == ChannelForPath(fallback.Path) {
					found = true
					break
				}
			}
			if !found {
				var err error
				fallback.StableID, err = model.NewStableID()
				if err != nil {
					return nil, err
				}
				after.Prefabs = append(after.Prefabs, fallback)
			}
		}
		if !state.Equal(after) {
			changes = append(changes, model.TileChange{Coord: c, Before: state, After: after})
		}
	}
	return changes, nil
}
