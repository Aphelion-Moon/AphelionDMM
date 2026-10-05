package repath

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repath/updatepaths"
)

func screenshotTarget(t *testing.T) *Index {
	return testIndex(t, MapSource{
		"/area":                      {"name": `"area"`},
		"/area/ai_monitored":         {},
		"/area/ai_monitored/command": {},
		"/area/ai_monitored/command/nuke_storage": {},
		"/obj":                                     {"name": `"obj"`, "desc": "null", "dir": "2", "pixel_x": "0", "pixel_y": "0", "icon": "null", "icon_state": `""`, "req_access": "null"},
		"/obj/item":                                {},
		"/obj/item/stamp":                          {"icon": "'icons/obj/bureaucracy.dmi'", "icon_state": `"stamp-ok"`, "name": `"rubber stamp"`},
		"/obj/item/stamp/head":                     {},
		"/obj/item/stamp/head/captain":             {"icon_state": `"stamp-cap"`, "name": `"captain's rubber stamp"`},
		"/obj/item/stamp/head/hop":                 {"icon_state": `"stamp-hop"`, "name": `"head of personnel's rubber stamp"`},
		"/obj/item/stack":                          {},
		"/obj/item/stack/medical":                  {},
		"/obj/item/stack/medical/gauze":            {"icon": "'icons/obj/medical.dmi'", "icon_state": `"gauze"`, "name": `"medical gauze"`},
		"/obj/item/stack/medical/bruise_pack":      {},
		"/obj/machinery":                           {},
		"/obj/machinery/keycard_auth":              {},
		"/obj/structure":                           {},
		"/obj/structure/sign":                      {},
		"/obj/structure/sign/delamination_counter": {},
		"/obj/item/sticky_tape":                    {},
		"/obj/item/sticky_tape/surgical":           {},
	})
}

func entry(path string, count int, vars map[string]string) Entry {
	return Entry{Path: path, Count: count, Tiles: count, Variants: []Variant{{Key: VariantKey(vars), Vars: vars, Count: count}}}
}

func suggestOne(t *testing.T, e Entry, sources Sources, level AutoLevel) Proposal {
	t.Helper()
	proposals, err := Suggest(context.Background(), Inventory{Entries: []Entry{e}}, sources, level)
	if err != nil {
		t.Fatal(err)
	}
	return proposals[0]
}

func TestSuffixMatchFindsRemovedParentSegment(t *testing.T) {
	p := suggestOne(t, entry("/area/station/ai_monitored/command/nuke_storage", 4, nil), Sources{Target: screenshotTarget(t)}, AutoHigh)
	best := p.Candidates[0]
	if best.Target != "/area/ai_monitored/command/nuke_storage" || best.Tier != High || p.Auto != 0 {
		t.Fatalf("best = %#v auto %d", best, p.Auto)
	}
	if again := suggestOne(t, entry("/area/station/ai_monitored/command/nuke_storage", 4, nil), Sources{Target: screenshotTarget(t)}, AutoCertain); again.Auto != -1 {
		t.Fatal("High candidate selected at the Certain-only level")
	}
}

func TestLeafMoveIsSuggestedButNotAutomatic(t *testing.T) {
	p := suggestOne(t, entry("/obj/item/stamp/captain", 1, nil), Sources{Target: screenshotTarget(t)}, AutoHigh)
	if p.Candidates[0].Target != "/obj/item/stamp/head/captain" || p.Candidates[0].Tier != Medium || p.Auto != -1 {
		t.Fatalf("candidates = %#v auto %d", p.Candidates, p.Auto)
	}
}

func TestDistantMatchesStayLow(t *testing.T) {
	target := testIndex(t, MapSource{
		"/obj": {}, "/obj/item": {}, "/obj/item/disk": {}, "/obj/item/disk/neuroware": {}, "/obj/item/disk/neuroware/reset": {},
		"/obj/machinery": {}, "/obj/machinery/computer": {},
		"/obj/item/storage": {}, "/obj/item/storage/belt": {}, "/obj/item/storage/belt/department_guard": {}, "/obj/item/storage/belt/department_guard/engineering": {},
	})
	p := suggestOne(t, entry("/obj/item/ai_module/reset", 1, nil), Sources{Target: target}, AutoHigh)
	if p.Candidates[0].Target != "/obj/item/disk/neuroware/reset" || p.Candidates[0].Tier != Low {
		t.Fatalf("leaf in another branch = %#v", p.Candidates[0])
	}
	p = suggestOne(t, entry("/obj/machinery/computer/department_orders/engineering", 1, nil), Sources{Target: target}, AutoHigh)
	for _, candidate := range p.Candidates {
		if strings.HasPrefix(candidate.Target, "/obj/item/") {
			t.Fatalf("suggested an unrelated branch: %#v", candidate)
		}
	}
}

func TestReferenceMetadataUpgradesLeafMove(t *testing.T) {
	reference := testIndex(t, MapSource{
		"/obj":                    {"name": `"obj"`, "icon": "null", "icon_state": `""`},
		"/obj/item/stamp":         {"icon": "'icons/obj/bureaucracy.dmi'"},
		"/obj/item/stamp/captain": {"icon_state": `"stamp-cap"`, "name": `"captain's rubber stamp"`},
	})
	p := suggestOne(t, entry("/obj/item/stamp/captain", 1, nil), Sources{Target: screenshotTarget(t), Reference: reference}, AutoHigh)
	best := p.Candidates[0]
	if best.Target != "/obj/item/stamp/head/captain" || best.Tier != High || p.Auto != 0 || !strings.Contains(best.Generator, "metadata") {
		t.Fatalf("best = %#v auto %d", best, p.Auto)
	}
}

func TestDirectionalHelperWithAndWithoutReference(t *testing.T) {
	path := "/obj/machinery/keycard_auth/directional/east"
	p := suggestOne(t, entry(path, 2, nil), Sources{Target: screenshotTarget(t)}, AutoHigh)
	best := p.Candidates[0]
	if best.Target != "/obj/machinery/keycard_auth" || best.Tier != Medium || p.Auto != -1 {
		t.Fatalf("best = %#v", best)
	}
	reference := testIndex(t, MapSource{
		"/obj":                        {"dir": "2", "pixel_x": "0"},
		"/obj/machinery/keycard_auth": {},
		path:                          {"dir": "4", "pixel_x": "26"},
	})
	p = suggestOne(t, entry(path, 2, nil), Sources{Target: screenshotTarget(t), Reference: reference}, AutoHigh)
	best = p.Candidates[0]
	if best.Tier != High || p.Auto != 0 {
		t.Fatalf("best = %#v auto %d", best, p.Auto)
	}
	out, ok := best.Decision.Rule.Apply(updatepaths.Instance{Path: path})
	if !ok || out[0].Vars["dir"] != "4" || out[0].Vars["pixel_x"] != "26" {
		t.Fatalf("outputs = %#v", out)
	}
}

func TestDirectionalHelperFindsMovedFamilyByName(t *testing.T) {
	target := testIndex(t, MapSource{"/obj": {"dir": "2"}, "/obj/structure": {}, "/obj/structure/secure_safe": {}, "/obj/structure/secure_safe/caps_spare": {}})
	p := suggestOne(t, entry("/obj/item/storage/secure/safe/caps_spare/directional/west", 1, nil), Sources{Target: target}, AutoHigh)
	best := p.Candidates[0]
	out, _ := best.Decision.Rule.Apply(updatepaths.Instance{Path: "/obj/item/storage/secure/safe/caps_spare/directional/west"})
	if best.Target != "/obj/structure/secure_safe/caps_spare" || best.Tier != Low || p.Auto != -1 || out[0].Vars["dir"] != "8" {
		t.Fatalf("best = %#v", best)
	}
}

func TestDirectionalConflictWithMapEditedDirIsNeverAutomatic(t *testing.T) {
	reference := testIndex(t, MapSource{"/obj": {"dir": "2"}, "/obj/machinery/keycard_auth/directional/east": {"dir": "4"}})
	p := suggestOne(t, entry("/obj/machinery/keycard_auth/directional/east", 1, map[string]string{"dir": "8"}), Sources{Target: screenshotTarget(t), Reference: reference}, AutoHigh)
	if len(p.Candidates[0].Conflicts) == 0 || p.Auto != -1 {
		t.Fatalf("candidate = %#v auto %d", p.Candidates[0], p.Auto)
	}
}

func TestScriptsAreCertainAndChainAware(t *testing.T) {
	target := screenshotTarget(t)
	one, _ := updatepaths.Parse("100_A.txt", []byte("/obj/item/stack/medical/gauze/old : /obj/item/stack/medical/gauze{@OLD}\n"))
	p := suggestOne(t, entry("/obj/item/stack/medical/gauze/old", 3, map[string]string{"amount": "5"}), Sources{Target: target, Scripts: updatepaths.NewSet(one)}, AutoCertain)
	if best := p.Candidates[0]; best.Tier != Certain || best.Decision.Via != ViaScripts || p.Auto != 0 || !strings.Contains(best.Reasons[0], "100_A.txt:1") {
		t.Fatalf("best = %#v", best)
	}
	two, _ := updatepaths.Parse("200_B.txt", []byte("/obj/item/stack/medical/gauze : /obj/item/stack/medical/bruise_pack{@OLD}\n"))
	p = suggestOne(t, entry("/obj/item/stack/medical/gauze/old", 3, nil), Sources{Target: target, Scripts: updatepaths.NewSet(one, two)}, AutoHigh)
	if p.Auto != -1 || p.Candidates[0].Tier != High {
		t.Fatalf("diverging script history was selected: %#v", p.Candidates)
	}
}

func TestScriptDeletionNeedsAPerson(t *testing.T) {
	scripts, _ := updatepaths.Parse("1_A.txt", []byte("/obj/effect/landmark/xeno_spawn : @DELETE\n"))
	p := suggestOne(t, entry("/obj/effect/landmark/xeno_spawn", 1, nil), Sources{Target: screenshotTarget(t), Scripts: updatepaths.NewSet(scripts)}, AutoHigh)
	if p.Candidates[0].Decision.Kind != Delete || p.Auto != -1 {
		t.Fatalf("candidates = %#v", p.Candidates)
	}
	if _, err := Compile(DefaultPlan([]Proposal{p}), screenshotTarget(t), []string{"/obj/effect/landmark/xeno_spawn"}, Resolvers{}); err != nil {
		t.Fatal(err)
	}
}

func TestAncestorFallbackIsAlwaysLossy(t *testing.T) {
	p := suggestOne(t, entry("/obj/item/stack/medical/gauze/sterile/extra", 1, nil), Sources{Target: screenshotTarget(t)}, AutoHigh)
	last := p.Candidates[len(p.Candidates)-1]
	if last.Target != "/obj/item/stack/medical/gauze" || last.Tier != Lossy || p.Auto != -1 {
		t.Fatalf("candidates = %#v", p.Candidates)
	}
}

func TestMissingVariablesLowerScore(t *testing.T) {
	target := screenshotTarget(t)
	plain := suggestOne(t, entry("/area/station/ai_monitored/command/nuke_storage", 1, nil), Sources{Target: target}, AutoHigh)
	edited := suggestOne(t, entry("/area/station/ai_monitored/command/nuke_storage", 1, map[string]string{"unheard_of": "1"}), Sources{Target: target}, AutoHigh)
	if edited.Candidates[0].Score >= plain.Candidates[0].Score || len(edited.Candidates[0].MissingVars) != 1 || edited.Auto != -1 {
		t.Fatalf("plain %#v edited %#v", plain.Candidates[0], edited.Candidates[0])
	}
}

func TestSuggestionsApplyThroughDefaultPlan(t *testing.T) {
	target := screenshotTarget(t)
	paths := []string{"/area/station/ai_monitored/command/nuke_storage", "/obj/item/stamp/captain"}
	inventory := Inventory{Entries: []Entry{entry(paths[0], 1, nil), entry(paths[1], 1, nil)}}
	proposals, err := Suggest(context.Background(), inventory, Sources{Target: target}, AutoHigh)
	if err != nil {
		t.Fatal(err)
	}
	transformer, err := Compile(DefaultPlan(proposals), target, paths, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	report := NewReport()
	tile := model.TileState{Prefabs: []model.PrefabState{prefab(t, paths[1], nil), prefab(t, "/turf", nil), prefab(t, paths[0], nil)}}
	after, changed, err := transformer.Tile(model.Coord{X: 1, Y: 1, Z: 1}, tile, nil, Defaults{}, &report)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if after.Prefabs[0].Path != paths[1] || after.Prefabs[2].Path != "/area/ai_monitored/command/nuke_storage" {
		t.Fatalf("after = %#v", after.Prefabs)
	}
}

func TestSimilarityHelpers(t *testing.T) {
	if similarity("abc", "abc") != 1 || similarity("", "a") != 0 || similarity("kitten", "sitting") < 0.5 {
		t.Fatal("similarity")
	}
	if jaccard([]string{"sticky", "tape"}, []string{"tape"}) != 0.5 {
		t.Fatal("jaccard")
	}
	if got := tokens("five_k2_Apc"); len(got) != 2 || got[0] != "five" || got[1] != "apc" {
		t.Fatalf("tokens = %v", got)
	}
}
