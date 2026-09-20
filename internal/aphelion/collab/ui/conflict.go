package ui

import (
	"sort"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
)

type ConflictAction string

const (
	ConflictActionRefresh ConflictAction = "refresh_authoritative"
	ConflictActionDiscard ConflictAction = "discard_local"
	ConflictActionRebuild ConflictAction = "rebuild_operation"
	ConflictActionExport  ConflictAction = "export_draft"
)

type VariableView struct {
	Name  string
	Value string
}

type PrefabValueView struct {
	StableID  model.StableID
	Path      string
	Variables []VariableView
}

type AuthoritativeTileView struct {
	Coord   model.Coord
	Prefabs []PrefabValueView
}

type ConflictView struct {
	OperationID model.OperationID
	Code        string
	Message     string
	Revision    model.Revision
	Values      []AuthoritativeTileView
	DraftBefore []AuthoritativeTileView
	DraftAfter  []AuthoritativeTileView
	Actions     []ConflictAction
}

func BuildConflictView(conflict client.Conflict) ConflictView {
	before := make([]model.Tile, len(conflict.Draft.Changes))
	after := make([]model.Tile, len(conflict.Draft.Changes))
	for index, change := range conflict.Draft.Changes {
		before[index] = model.Tile{Coord: change.Coord, State: change.Before}
		after[index] = model.Tile{Coord: change.Coord, State: change.After}
	}
	return ConflictView{
		OperationID: conflict.OperationID,
		Code:        conflict.Code,
		Message:     conflict.Message,
		Revision:    conflict.Revision,
		Values:      buildTileValues(conflict.AuthoritativeValues),
		DraftBefore: buildTileValues(before),
		DraftAfter:  buildTileValues(after),
		Actions:     []ConflictAction{ConflictActionRefresh, ConflictActionDiscard, ConflictActionRebuild, ConflictActionExport},
	}
}

func buildTileValues(tiles []model.Tile) []AuthoritativeTileView {
	values := make([]AuthoritativeTileView, len(tiles))
	for tileIndex, tile := range tiles {
		prefabs := make([]PrefabValueView, len(tile.State.Prefabs))
		for prefabIndex, prefab := range tile.State.Prefabs {
			variables := make([]VariableView, 0, len(prefab.Vars))
			for name, value := range prefab.Vars {
				variables = append(variables, VariableView{Name: name, Value: value})
			}
			sort.Slice(variables, func(left, right int) bool { return variables[left].Name < variables[right].Name })
			prefabs[prefabIndex] = PrefabValueView{StableID: prefab.StableID, Path: prefab.Path, Variables: variables}
		}
		// Prefab order is map data; show the original stack order.
		values[tileIndex] = AuthoritativeTileView{Coord: tile.Coord, Prefabs: prefabs}
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].Coord.Z != values[right].Coord.Z {
			return values[left].Coord.Z < values[right].Coord.Z
		}
		if values[left].Coord.Y != values[right].Coord.Y {
			return values[left].Coord.Y < values[right].Coord.Y
		}
		return values[left].Coord.X < values[right].Coord.X
	})
	return values
}
