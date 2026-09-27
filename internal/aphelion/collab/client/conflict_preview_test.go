package client

import (
	"fmt"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func previewConflictFixture() Conflict {
	conflict := Conflict{OperationID: "01890f3e-7b5c-7abc-8def-0123456789bb"}
	for x := 7; x >= 1; x-- {
		state := model.TileState{}
		for p := 0; p < 4; p++ {
			state.Prefabs = append(state.Prefabs, model.PrefabState{Path: fmt.Sprint(p), Vars: map[string]string{"z": "last", "b": "second", "a": "first"}})
		}
		coord := model.Coord{X: x, Y: 1, Z: 1}
		conflict.Draft.Changes = append(conflict.Draft.Changes, model.TileChange{Coord: coord, Before: state, After: state})
		conflict.AuthoritativeValues = append(conflict.AuthoritativeValues, model.Tile{Coord: coord, State: state})
	}
	return conflict
}

func TestConflictPreviewKeepsCountsOrderingAndDetachedValues(t *testing.T) {
	conflict := previewConflictFixture()
	want := cloneConflict(conflict)
	preview := PreviewConflict(conflict, ConflictPreviewLimits{Tiles: 3, Prefabs: 2, Variables: 2})
	if preview.DraftTileCount != 7 || preview.AuthoritativeTileCount != 7 {
		t.Fatal("lost total counts")
	}
	for _, tiles := range [][]ConflictTilePreview{preview.DraftBefore, preview.DraftAfter, preview.Values} {
		if len(tiles) != 3 {
			t.Fatal("unbounded tile preview")
		}
		for i, tile := range tiles {
			if tile.Coord.X != i+1 || len(tile.Prefabs) != 2 || tile.PrefabCount != 4 {
				t.Fatal("lost sorted coordinates or prefab counts")
			}
			for j, prefab := range tile.Prefabs {
				if prefab.Path != fmt.Sprint(j) || prefab.VariableCount != 3 || len(prefab.Variables) != 2 || prefab.Variables[0].Name != "a" || prefab.Variables[1].Name != "b" {
					t.Fatal("lost stack order, variable order or hidden count")
				}
				prefab.Variables[0].Value = "caller"
			}
			tile.Prefabs[0].Path = "caller"
		}
	}
	if !reflect.DeepEqual(conflict, want) {
		t.Fatal("preview aliases recovery data")
	}
}

func TestConflictPublicationKeepsConcurrentReadersDetached(t *testing.T) {
	network, err := NewNetworkExecutor(newFakeTransport(), projectionSnapshot(t), mustActorID(t), "session")
	if err != nil {
		t.Fatal(err)
	}
	conflict := previewConflictFixture()
	network.mutex.Lock()
	network.retainConflictLocked(conflict)
	network.beginMetadataPublicationLocked()
	network.mutex.Unlock()
	// Incoming retention owns its payload before publication shares immutable entries.
	conflict.Draft.Changes[0].After.Prefabs[0].Vars["a"] = "input caller"
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			previews, count := network.ConflictPreviews(1, ConflictPreviewLimits{Tiles: 2, Prefabs: 1, Variables: 1})
			if count == 0 || len(previews) != 1 || previews[0].DraftAfter[0].Prefabs[0].Variables[0].Value != "first" {
				t.Error("publication changed retained values")
				return
			}
			previews[0].DraftAfter[0].Prefabs[0].Variables[0].Value = "preview caller"
			full, ok := network.Conflict(conflict.OperationID)
			if !ok || len(full.Draft.Changes) != 7 {
				t.Error("targeted read lost complete draft")
				return
			}
			full.Draft.Changes[0].After.Prefabs[0].Vars["a"] = "export caller"
		}
	}()
	for range 100 {
		next := previewConflictFixture()
		next.OperationID, err = model.NewOperationID()
		if err != nil {
			t.Fatal(err)
		}
		network.mutex.Lock()
		network.retainConflictLocked(next)
		network.beginMetadataPublicationLocked()
		network.mutex.Unlock()
		if !network.DismissConflict(next.OperationID) {
			t.Fatal("lost added draft")
		}
	}
	<-done
	if previews, count := network.ConflictPreviews(0, ConflictPreviewLimits{}); len(previews) != 0 || count != 1 || network.ConflictCount() != 1 {
		t.Fatal("hidden conflicts lost recovery count")
	}
}
