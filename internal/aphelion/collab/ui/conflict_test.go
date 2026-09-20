package ui

import (
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
)

func TestBuildConflictViewExposesAuthoritativeValuesAndSafeActions(t *testing.T) {
	t.Parallel()

	conflict := client.Conflict{
		OperationID: model.OperationID("01890f3e-7b5c-7abc-8def-0123456789bb"),
		Code:        "precondition_failed",
		Message:     "operation was rejected",
		Revision:    4,
		AuthoritativeValues: []model.Tile{
			{
				Coord: model.Coord{X: 2, Y: 1, Z: 1},
				State: model.TileState{Prefabs: []model.PrefabState{
					{
						StableID: model.StableID("01890f3e-7b5c-7abc-8def-0123456789bc"),
						Path:     "/obj/example",
						Vars:     map[string]string{"z": "2", "a": "1"},
					},
				}},
			},
		},
	}
	view := BuildConflictView(conflict)
	if len(view.Values) != 1 || len(view.Values[0].Prefabs) != 1 || view.Values[0].Prefabs[0].Variables[0].Name != "a" {
		t.Fatalf("conflict values = %#v", view.Values)
	}
	wantActions := []ConflictAction{ConflictActionRefresh, ConflictActionDiscard, ConflictActionRebuild, ConflictActionExport}
	if len(view.Actions) != len(wantActions) {
		t.Fatalf("actions = %#v", view.Actions)
	}
	for index, action := range wantActions {
		if view.Actions[index] != action {
			t.Fatalf("action %d = %q, want %q", index, view.Actions[index], action)
		}
	}
}
