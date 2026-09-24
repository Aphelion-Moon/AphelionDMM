package editing

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
)

func TestMovePayloadComposesSparseUnionAndPreservesSourceIdentity(t *testing.T) {
	selection, err := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 4, Y: 1, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	base := map[model.Coord]model.TileState{
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "hidden-source", Path: "/obj/hidden"}, {StableID: "01890f3e-7b5c-7abc-8def-000000000001", Path: "/obj/source-a"}}},
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "destination-a", Path: "/obj/old"}}},
		{X: 3, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "hole", Path: "/obj/untouched"}}},
		{X: 4, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "hidden-source-b", Path: "/obj/hidden"}, {StableID: "01890f3e-7b5c-7abc-8def-000000000004", Path: "/obj/source-b"}}},
		{X: 5, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "destination-b", Path: "/obj/old"}}},
	}
	lookup := func(coord model.Coord) (model.TileState, bool) { state, ok := base[coord]; return state, ok }
	visible := func(path string) bool { return path != "/obj/hidden" }
	payload, err := CompileMovePayload(context.Background(), selection, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	holeBefore := model.CloneTileState(base[model.Coord{X: 3, Y: 1, Z: 1}])
	changes, err := payload.BuildMoveChanges(context.Background(), util.Point{X: 1}, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := changeCoords(changes), []model.Coord{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 4, Y: 1, Z: 1}, {X: 5, Y: 1, Z: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed sparse union %v, want %v", got, want)
	}
	after := changedStates(changes)
	if got, want := prefabIDs(after[model.Coord{X: 1, Y: 1, Z: 1}]), []model.StableID{"hidden-source"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source after move %v, want retained hidden content %v", got, want)
	}
	if got, want := prefabIDs(after[model.Coord{X: 2, Y: 1, Z: 1}]), []model.StableID{"01890f3e-7b5c-7abc-8def-000000000001"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("destination after move %v, want source identity %v", got, want)
	}
	if got, want := prefabIDs(after[model.Coord{X: 4, Y: 1, Z: 1}]), []model.StableID{"hidden-source-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second source after move %v, want retained hidden content %v", got, want)
	}
	if got, want := prefabIDs(after[model.Coord{X: 5, Y: 1, Z: 1}]), []model.StableID{"01890f3e-7b5c-7abc-8def-000000000004"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second destination after move %v, want source identity %v", got, want)
	}
	if got := base[model.Coord{X: 3, Y: 1, Z: 1}]; !got.Equal(holeBefore) {
		t.Fatalf("compilation or planning mutated the sparse hole: %+v", got)
	}
	if !payload.Suppresses(util.Point{X: 2, Y: 1, Z: 1}, util.Point{X: 1}) || payload.Suppresses(util.Point{X: 3, Y: 1, Z: 1}, util.Point{X: 1}) {
		t.Fatal("presentation suppression did not track the source/destination selection union")
	}
}

func TestMovePayloadKeepsOverlapAndRejectsChangedVisibleSource(t *testing.T) {
	selection, err := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	base := map[model.Coord]model.TileState{
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-000000000001", Path: "/obj/a"}}},
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-000000000002", Path: "/obj/b"}}},
		{X: 3, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-000000000003", Path: "/obj/old"}}},
	}
	lookup := func(coord model.Coord) (model.TileState, bool) { state, ok := base[coord]; return state, ok }
	visible := func(string) bool { return true }
	payload, err := CompileMovePayload(context.Background(), selection, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := payload.BuildMoveChanges(context.Background(), util.Point{X: 1}, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	after := changedStates(changes)
	if got, want := prefabIDs(after[model.Coord{X: 2, Y: 1, Z: 1}]), []model.StableID{"01890f3e-7b5c-7abc-8def-000000000001"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("overlapped tile has IDs %v, want incoming identity %v", got, want)
	}
	if got, want := prefabIDs(after[model.Coord{X: 3, Y: 1, Z: 1}]), []model.StableID{"01890f3e-7b5c-7abc-8def-000000000002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("last destination has IDs %v, want incoming identity %v", got, want)
	}
	base[model.Coord{X: 1, Y: 1, Z: 1}] = model.TileState{Prefabs: []model.PrefabState{{StableID: "01890f3e-7b5c-7abc-8def-000000000009", Path: "/obj/changed"}}}
	if _, err := payload.BuildMoveChanges(context.Background(), util.Point{X: 1}, visible, lookup); err == nil {
		t.Fatal("move accepted a source whose visible contents changed after preview preparation")
	}
}

func TestMovePayloadRegeneratesVisibleBaseAtVacatedSource(t *testing.T) {
	selection := RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
	base := map[model.Coord]model.TileState{
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{
			{StableID: "01890f3e-7b5c-7abc-8def-000000000001", Path: "/area/source"},
			{StableID: "01890f3e-7b5c-7abc-8def-000000000002", Path: "/turf/source"},
			{StableID: "01890f3e-7b5c-7abc-8def-000000000003", Path: "/obj/source"},
		}},
		{X: 2, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{
			{StableID: "01890f3e-7b5c-7abc-8def-000000000004", Path: "/area/destination"},
			{StableID: "01890f3e-7b5c-7abc-8def-000000000005", Path: "/turf/destination"},
			{StableID: "01890f3e-7b5c-7abc-8def-000000000006", Path: "/obj/old"},
		}},
	}
	lookup := func(coord model.Coord) (model.TileState, bool) { state, ok := base[coord]; return state, ok }
	visible := func(string) bool { return true }
	payload, err := CompileMovePayload(context.Background(), selection, visible, lookup)
	if err != nil {
		t.Fatal(err)
	}
	defaults := MoveDefaults{
		Area: model.PrefabState{Path: "/area/default", Vars: map[string]string{"name": "default area"}},
		Turf: model.PrefabState{Path: "/turf/default", Vars: map[string]string{"icon_state": "default turf"}},
	}
	changes, err := payload.BuildMoveChangesWithDefaults(context.Background(), util.Point{X: 1}, visible, lookup, defaults)
	if err != nil {
		t.Fatal(err)
	}
	after := changedStates(changes)
	sourceIDs := map[model.StableID]struct{}{}
	for _, prefab := range base[model.Coord{X: 1, Y: 1, Z: 1}].Prefabs {
		sourceIDs[prefab.StableID] = struct{}{}
	}
	byPath := func(state model.TileState, root string) (model.PrefabState, bool) {
		for _, prefab := range state.Prefabs {
			if len(prefab.Path) >= len(root) && prefab.Path[:len(root)] == root {
				return prefab, true
			}
		}
		return model.PrefabState{}, false
	}
	for _, root := range []string{"/area", "/turf"} {
		got, ok := byPath(after[model.Coord{X: 1, Y: 1, Z: 1}], root)
		_, reused := sourceIDs[got.StableID]
		wantPath := defaults.Area.Path
		if root == "/turf" {
			wantPath = defaults.Turf.Path
		}
		if !ok || got.StableID == "" || reused || got.Path != wantPath {
			t.Fatalf("vacated source did not regenerate %s: %+v", root, got)
		}
	}
	if got, want := prefabIDs(after[model.Coord{X: 2, Y: 1, Z: 1}]), []model.StableID{
		"01890f3e-7b5c-7abc-8def-000000000001", "01890f3e-7b5c-7abc-8def-000000000002", "01890f3e-7b5c-7abc-8def-000000000003",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("destination lost source identities: got %v want %v", got, want)
	}
}

func changeCoords(changes []model.TileChange) []model.Coord {
	coords := make([]model.Coord, len(changes))
	for i, change := range changes {
		coords[i] = change.Coord
	}
	return coords
}

func changedStates(changes []model.TileChange) map[model.Coord]model.TileState {
	states := make(map[model.Coord]model.TileState, len(changes))
	for _, change := range changes {
		states[change.Coord] = change.After
	}
	return states
}

func prefabIDs(state model.TileState) []model.StableID {
	ids := make([]model.StableID, len(state.Prefabs))
	for i, prefab := range state.Prefabs {
		ids[i] = prefab.StableID
	}
	return ids
}
