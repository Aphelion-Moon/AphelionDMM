package client

import (
	"sort"

	"sdmm/internal/aphelion/collab/model"
)

// ConflictPreview contains detached, bounded display data, never a rebuildable
// operation. Counts describe the complete retained draft, including hidden data.
type ConflictPreview struct {
	OperationID            model.OperationID
	Code                   string
	Message                string
	Revision               model.Revision
	Values                 []ConflictTilePreview
	DraftBefore            []ConflictTilePreview
	DraftAfter             []ConflictTilePreview
	AuthoritativeTileCount int
	DraftTileCount         int
}

type ConflictTilePreview struct {
	Coord       model.Coord
	Prefabs     []ConflictPrefabPreview
	PrefabCount int
}

type ConflictPrefabPreview struct {
	StableID      model.StableID
	Path          string
	Variables     []ConflictVariablePreview
	VariableCount int
}

type ConflictVariablePreview struct{ Name, Value string }

type ConflictPreviewLimits struct{ Tiles, Prefabs, Variables int }

func (network *NetworkExecutor) ConflictCount() int {
	published := network.published.Load()
	if published == nil {
		return 0
	}
	return len(published.conflicts)
}

// Conflict returns only the requested complete draft for explicit export.
func (network *NetworkExecutor) Conflict(id model.OperationID) (Conflict, bool) {
	published := network.published.Load()
	if published != nil {
		for _, conflict := range published.conflicts {
			if conflict.OperationID == id {
				return cloneConflict(conflict), true
			}
		}
	}
	return Conflict{}, false
}

func (network *NetworkExecutor) ConflictPreviews(limit int, detail ConflictPreviewLimits) ([]ConflictPreview, int) {
	previews, count, _ := network.ConflictPreviewPage(0, limit, detail)
	return previews, count
}

// ConflictPreviewPage clamps navigation against the same immutable list used
// to build the page, including when resolution removes the requested last page.
func (network *NetworkExecutor) ConflictPreviewPage(page, size int, detail ConflictPreviewLimits) ([]ConflictPreview, int, int) {
	published := network.published.Load()
	if published == nil {
		return nil, 0, 0
	}
	count := len(published.conflicts)
	if count == 0 || size <= 0 {
		return nil, count, 0
	}
	page = min(max(page, 0), (count-1)/size)
	start := page * size
	previews := make([]ConflictPreview, min(size, count-start))
	for i := range previews {
		previews[i] = PreviewConflict(published.conflicts[start+i], detail)
	}
	return previews, count, page
}

func PreviewConflict(conflict Conflict, limits ConflictPreviewLimits) ConflictPreview {
	result := ConflictPreview{OperationID: conflict.OperationID, Code: conflict.Code, Message: conflict.Message, Revision: conflict.Revision,
		DraftTileCount: len(conflict.Draft.Changes), AuthoritativeTileCount: len(conflict.AuthoritativeValues)}
	changes := conflict.Draft.Changes
	indexes := previewTileIndexes(len(changes), limits.Tiles, func(i int) model.Coord { return changes[i].Coord })
	result.DraftBefore = make([]ConflictTilePreview, len(indexes))
	result.DraftAfter = make([]ConflictTilePreview, len(indexes))
	for i, index := range indexes {
		change := changes[index]
		result.DraftBefore[i] = previewTile(change.Coord, change.Before, limits)
		result.DraftAfter[i] = previewTile(change.Coord, change.After, limits)
	}
	tiles := conflict.AuthoritativeValues
	indexes = previewTileIndexes(len(tiles), limits.Tiles, func(i int) model.Coord { return tiles[i].Coord })
	result.Values = make([]ConflictTilePreview, len(indexes))
	for i, index := range indexes {
		result.Values[i] = previewTile(tiles[index].Coord, tiles[index].State, limits)
	}
	return result
}

// Retain only the smallest visible coordinates instead of sorting or copying
// an entire area operation for each frame. Equal coordinates keep input order.
func previewTileIndexes(count, limit int, coord func(int) model.Coord) []int {
	limit = min(max(limit, 0), count)
	result := make([]int, 0, limit)
	if limit == 0 {
		return result
	}
	for i := 0; i < count; i++ {
		current := coord(i)
		at := sort.Search(len(result), func(j int) bool { return coordLess(current, coord(result[j])) })
		if at >= limit {
			continue
		}
		if len(result) < limit {
			result = append(result, i)
		}
		copy(result[at+1:], result[at:len(result)-1])
		result[at] = i
	}
	return result
}

func previewTile(coord model.Coord, state model.TileState, limits ConflictPreviewLimits) ConflictTilePreview {
	result := ConflictTilePreview{Coord: coord, PrefabCount: len(state.Prefabs)}
	result.Prefabs = make([]ConflictPrefabPreview, min(max(limits.Prefabs, 0), len(state.Prefabs)))
	for i := range result.Prefabs {
		prefab := state.Prefabs[i]
		limit := min(max(limits.Variables, 0), len(prefab.Vars))
		keys := make([]string, 0, limit)
		if limit != 0 {
			for key := range prefab.Vars {
				at := sort.SearchStrings(keys, key)
				if at >= limit {
					continue
				}
				if len(keys) < limit {
					keys = append(keys, key)
				}
				copy(keys[at+1:], keys[at:len(keys)-1])
				keys[at] = key
			}
		}
		variables := make([]ConflictVariablePreview, len(keys))
		for j, key := range keys {
			variables[j] = ConflictVariablePreview{Name: key, Value: prefab.Vars[key]}
		}
		result.Prefabs[i] = ConflictPrefabPreview{StableID: prefab.StableID, Path: prefab.Path, Variables: variables, VariableCount: len(prefab.Vars)}
	}
	return result
}
