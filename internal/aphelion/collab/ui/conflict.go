package ui

import (
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

type VariableView = client.ConflictVariablePreview
type PrefabValueView = client.ConflictPrefabPreview
type AuthoritativeTileView = client.ConflictTilePreview

type ConflictView struct {
	OperationID            model.OperationID
	Code                   string
	Message                string
	Revision               model.Revision
	Values                 []AuthoritativeTileView
	DraftBefore            []AuthoritativeTileView
	DraftAfter             []AuthoritativeTileView
	AuthoritativeTileCount int
	DraftTileCount         int
	Actions                []ConflictAction
}

func conflictPreviewLimits() client.ConflictPreviewLimits {
	return client.ConflictPreviewLimits{Tiles: maxPanelConflictTiles, Prefabs: maxPanelConflictPrefabs, Variables: maxPanelConflictVariables}
}

func BuildConflictView(conflict client.Conflict) ConflictView {
	return buildConflictPreviewView(client.PreviewConflict(conflict, conflictPreviewLimits()))
}

func buildConflictPreviewView(conflict client.ConflictPreview) ConflictView {
	return ConflictView{
		OperationID: conflict.OperationID, Code: conflict.Code, Message: conflict.Message, Revision: conflict.Revision,
		Values: clonePreviewTiles(conflict.Values), DraftBefore: clonePreviewTiles(conflict.DraftBefore), DraftAfter: clonePreviewTiles(conflict.DraftAfter),
		AuthoritativeTileCount: conflict.AuthoritativeTileCount, DraftTileCount: conflict.DraftTileCount,
		Actions: []ConflictAction{ConflictActionRefresh, ConflictActionDiscard, ConflictActionRebuild, ConflictActionExport},
	}
}

// Redaction belongs to the view, never to the reusable status or retained draft.
func clonePreviewTiles(tiles []AuthoritativeTileView) []AuthoritativeTileView {
	result := append([]AuthoritativeTileView(nil), tiles...)
	for i := range result {
		result[i].Prefabs = append([]PrefabValueView(nil), result[i].Prefabs...)
		for j := range result[i].Prefabs {
			result[i].Prefabs[j].Variables = append([]VariableView(nil), result[i].Prefabs[j].Variables...)
		}
	}
	return result
}
