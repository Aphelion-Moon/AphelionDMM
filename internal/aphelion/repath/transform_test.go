package repath

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repath/updatepaths"
)

func testIndex(t *testing.T, source MapSource) *Index {
	t.Helper()
	index, err := NewIndex(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func testTarget(t *testing.T) *Index {
	return testIndex(t, MapSource{
		"/area":                        {"name": `"area"`},
		"/area/station":                {},
		"/turf":                        {"name": `"turf"`},
		"/turf/open/floor":             {},
		"/turf/open/floor/iron":        {},
		"/obj":                         {"name": `"obj"`, "dir": "2", "pixel_x": "0", "icon": "'icons/obj.dmi'", "icon_state": `""`},
		"/obj/item/stamp/head/captain": {"name": `"captain's stamp"`},
		"/obj/machinery/keycard_auth":  {},
		"/obj/effect/decal":            {},
	})
}

func id(t *testing.T) model.StableID {
	t.Helper()
	value, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func prefab(t *testing.T, path string, vars map[string]string) model.PrefabState {
	if vars == nil {
		vars = map[string]string{}
	}
	return model.PrefabState{StableID: id(t), Path: path, Vars: vars}
}

func ruleDecision(rule updatepaths.Rule) Decision {
	return Decision{Kind: Apply, Via: ViaRule, Rule: rule}
}

func TestTransformerKeepsStableIDsAndVariables(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	plan.Paths["/obj/item/stamp/captain"] = ruleDecision(RepathRule("/obj/item/stamp/captain", "/obj/item/stamp/head/captain", nil))
	plan.Paths["/obj/machinery/keycard_auth/directional/east"] = ruleDecision(RepathRule("/obj/machinery/keycard_auth/directional/east", "/obj/machinery/keycard_auth", map[string]string{"dir": "4", "pixel_x": "26"}))
	transformer, err := Compile(plan, target, []string{"/obj/item/stamp/captain", "/obj/machinery/keycard_auth/directional/east", "/obj/unrelated"}, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	stamp := prefab(t, "/obj/item/stamp/captain", map[string]string{"name": `"old"`})
	keycard := prefab(t, "/obj/machinery/keycard_auth/directional/east", map[string]string{"req_access": "list(1)"})
	unrelated := prefab(t, "/obj/unrelated", map[string]string{"x": "1"})
	turf := prefab(t, "/turf/open/floor/iron", nil)
	area := prefab(t, "/area/station", nil)
	before := model.TileState{Prefabs: []model.PrefabState{stamp, keycard, unrelated, turf, area}}
	report := NewReport()
	after, changed, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, before, nil, Defaults{}, &report)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got := after.Prefabs
	if got[0].StableID != stamp.StableID || got[0].Path != "/obj/item/stamp/head/captain" || got[0].Vars["name"] != `"old"` {
		t.Fatalf("stamp = %#v", got[0])
	}
	if got[1].StableID != keycard.StableID || got[1].Path != "/obj/machinery/keycard_auth" || got[1].Vars["dir"] != "4" || got[1].Vars["pixel_x"] != "26" || got[1].Vars["req_access"] != "list(1)" {
		t.Fatalf("keycard = %#v", got[1])
	}
	if got[2].Path != "/obj/unrelated" || got[2].StableID != unrelated.StableID || got[3].StableID != turf.StableID || got[4].StableID != area.StableID {
		t.Fatalf("untouched content changed: %#v", got)
	}
	if before.Prefabs[0].Path != "/obj/item/stamp/captain" {
		t.Fatal("input tile was mutated")
	}
	if report.Changed["/obj/item/stamp/captain"] != 1 || report.Changed["/obj/machinery/keycard_auth/directional/east"] != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestTransformerNeverTouchesKnownPaths(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	if _, err := Compile(plan, target, []string{"/obj/effect/decal"}, Resolvers{}); err == nil {
		t.Fatal("compiled a plan treating a known path as unknown")
	}
	scripts, _ := updatepaths.Parse("1_A.txt", []byte("/obj/effect/decal : /obj/machinery/keycard_auth\n/obj/old : /obj/effect/decal{@OLD}\n"))
	plan.Paths["/obj/old"] = Decision{Kind: Apply, Via: ViaScripts}
	transformer, err := Compile(plan, target, []string{"/obj/old"}, Resolvers{Scripts: updatepaths.NewSet(scripts)})
	if err != nil {
		t.Fatal(err)
	}
	decal := prefab(t, "/obj/effect/decal", nil)
	old := prefab(t, "/obj/old", map[string]string{"a": "1"})
	report := NewReport()
	after, _, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, model.TileState{Prefabs: []model.PrefabState{decal, old}}, nil, Defaults{}, &report)
	if err != nil {
		t.Fatal(err)
	}
	if after.Prefabs[0].Path != "/obj/effect/decal" || after.Prefabs[1].Path != "/obj/effect/decal" || after.Prefabs[1].Vars["a"] != "1" {
		t.Fatalf("after = %#v", after.Prefabs)
	}
}

func TestTransformerLeavesUnreachableOutputsUnknown(t *testing.T) {
	target := testTarget(t)
	scripts, _ := updatepaths.Parse("1_A.txt", []byte("/obj/old : /obj/still_missing\n"))
	plan := NewPlan()
	plan.Paths["/obj/old"] = Decision{Kind: Apply, Via: ViaScripts}
	transformer, err := Compile(plan, target, []string{"/obj/old"}, Resolvers{Scripts: updatepaths.NewSet(scripts)})
	if err != nil {
		t.Fatal(err)
	}
	report := NewReport()
	before := model.TileState{Prefabs: []model.PrefabState{prefab(t, "/obj/old", nil)}}
	if _, changed, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, before, nil, Defaults{}, &report); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if report.Unresolved["/obj/old"] != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestMultipleOutputsGetNewIDsAfterTheFirst(t *testing.T) {
	target := testTarget(t)
	rule, err := updatepaths.ParseRule("/obj/old : /obj/effect/decal{@OLD}, /obj/machinery/keycard_auth")
	if err != nil {
		t.Fatal(err)
	}
	plan := NewPlan()
	plan.Paths["/obj/old"] = ruleDecision(rule)
	transformer, err := Compile(plan, target, []string{"/obj/old"}, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	old := prefab(t, "/obj/old", map[string]string{"a": "1"})
	extra := id(t)
	ids := func() (model.StableID, error) { return extra, nil }
	report := NewReport()
	after, _, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, model.TileState{Prefabs: []model.PrefabState{old, prefab(t, "/turf/open/floor", nil)}}, ids, Defaults{}, &report)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Prefabs) != 3 || after.Prefabs[0].StableID != old.StableID || after.Prefabs[1].StableID != extra || after.Prefabs[1].Path != "/obj/machinery/keycard_auth" || len(after.Prefabs[1].Vars) != 0 {
		t.Fatalf("after = %#v", after.Prefabs)
	}
}

func TestCompileRejectsUnsafeDecisions(t *testing.T) {
	target := testTarget(t)
	unknown := []string{"/obj/old", "/turf/old"}
	tests := []struct {
		name     string
		path     string
		decision Decision
		want     string
	}{
		{"missing target", "/obj/old", ruleDecision(RepathRule("/obj/old", "/obj/nowhere", nil)), "not defined"},
		{"cross base", "/turf/old", ruleDecision(RepathRule("/turf/old", "/obj/effect/decal", nil)), "root type"},
		{"auto delete", "/obj/old", Decision{Kind: Delete, Origin: OriginAuto}, "confirmed by a person"},
		{"wrong match", "/obj/old", ruleDecision(RepathRule("/obj/other", "/obj/effect/decal", nil)), "matches /obj/other"},
		{"no scripts", "/obj/old", Decision{Kind: Apply, Via: ViaScripts}, "no scripts"},
		{"not unknown", "/obj/else", ruleDecision(RepathRule("/obj/else", "/obj/effect/decal", nil)), "not an unknown path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := NewPlan()
			plan.Paths[test.path] = test.decision
			if _, err := Compile(plan, target, unknown, Resolvers{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
	plan := NewPlan()
	plan.Paths["/turf/old"] = Decision{Kind: Apply, Via: ViaRule, Rule: RepathRule("/turf/old", "/obj/effect/decal", nil), AllowCrossBase: true}
	if _, err := Compile(plan, target, unknown, Resolvers{}); err != nil {
		t.Fatalf("explicit cross-base: %v", err)
	}
}

func TestBuildChangesFailsClosedOnTurfCount(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	plan.Paths["/turf/old"] = Decision{Kind: Apply, Via: ViaRule, Rule: RepathRule("/turf/old", "/obj/effect/decal", nil), AllowCrossBase: true}
	transformer, err := Compile(plan, target, []string{"/turf/old"}, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	tiles := map[model.Coord]model.TileState{
		{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{prefab(t, "/obj/effect/decal", nil), prefab(t, "/turf/old", nil), prefab(t, "/area/station", nil)}},
	}
	changes, report, err := BuildChanges(context.Background(), visitAll(tiles), readMap(tiles), transformer, nil, Defaults{})
	if err == nil || changes != nil || len(report.Conflicts) != 1 {
		t.Fatalf("changes=%v report=%#v err=%v", changes, report, err)
	}
}

func TestPersonDeletingTheOnlyTurfRestoresTheDefault(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	plan.Paths["/turf/old"] = Decision{Kind: Delete, Origin: OriginHuman}
	transformer, err := Compile(plan, target, []string{"/turf/old"}, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	coord := model.Coord{X: 1, Y: 1, Z: 1}
	area := prefab(t, "/area/station", nil)
	tiles := map[model.Coord]model.TileState{coord: {Prefabs: []model.PrefabState{prefab(t, "/obj/effect/decal", nil), prefab(t, "/turf/old", nil), area}}}
	fresh := id(t)
	changes, report, err := BuildChanges(context.Background(), visitAll(tiles), readMap(tiles), transformer, func() (model.StableID, error) { return fresh, nil }, Defaults{Turf: &model.PrefabState{Path: "/turf/open/floor"}})
	if err != nil {
		t.Fatal(err)
	}
	got := changes[0].After.Prefabs
	if len(got) != 3 || got[1].Path != "/turf/open/floor" || got[1].StableID != fresh || got[2].StableID != area.StableID || report.Tiles != 1 {
		t.Fatalf("after = %#v", got)
	}
}

func TestVariantOverridesPathDecision(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	plan.Paths["/obj/old"] = ruleDecision(RepathRule("/obj/old", "/obj/effect/decal", nil))
	plan.SetVariant("/obj/old", VariantKey(map[string]string{"dir": "4"}), Decision{Kind: Keep})
	transformer, err := Compile(plan, target, []string{"/obj/old"}, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	report := NewReport()
	tile := model.TileState{Prefabs: []model.PrefabState{prefab(t, "/obj/old", map[string]string{"dir": "4"}), prefab(t, "/obj/old", nil)}}
	after, _, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, tile, nil, Defaults{}, &report)
	if err != nil {
		t.Fatal(err)
	}
	if after.Prefabs[0].Path != "/obj/old" || after.Prefabs[1].Path != "/obj/effect/decal" {
		t.Fatalf("after = %#v", after.Prefabs)
	}
}

func visitAll(tiles map[model.Coord]model.TileState) func(func(model.Coord) bool) {
	return func(visit func(model.Coord) bool) {
		for z := 1; z <= 2; z++ {
			for y := 1; y <= 4; y++ {
				for x := 1; x <= 4; x++ {
					if _, ok := tiles[model.Coord{X: x, Y: y, Z: z}]; ok && !visit(model.Coord{X: x, Y: y, Z: z}) {
						return
					}
				}
			}
		}
	}
}

func readMap(tiles map[model.Coord]model.TileState) func(model.Coord) (model.TileState, bool) {
	return func(coord model.Coord) (model.TileState, bool) {
		tile, ok := tiles[coord]
		return model.CloneTileState(tile), ok
	}
}
