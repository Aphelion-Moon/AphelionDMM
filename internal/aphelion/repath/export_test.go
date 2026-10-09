package repath

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repath/updatepaths"
)

func TestExportedScriptReproducesThePlan(t *testing.T) {
	target := testTarget(t)
	plan := NewPlan()
	plan.Paths["/obj/old"] = ruleDecision(RepathRule("/obj/old", "/obj/effect/decal", map[string]string{"dir": "4"}))
	plan.SetVariant("/obj/old", VariantKey(map[string]string{"name": `"x"`}), ruleDecision(RepathRule("/obj/old", "/obj/machinery/keycard_auth", nil)))
	plan.Paths["/obj/gone"] = Decision{Kind: Delete, Origin: OriginHuman}
	plan.Paths["/obj/kept"] = Decision{Kind: Keep}
	plan.Paths["/obj/scripted"] = Decision{Kind: Apply, Via: ViaScripts, Summary: "codebase scripts"}
	script := ExportScript(plan, "MiniStation port")
	text := string(script)
	for _, want := range []string{"# MiniStation port\n", "# /obj/scripted is resolved by codebase scripts\n", "/obj/gone : @DELETE\n", `/obj/old{name = "x"} : /obj/machinery/keycard_auth{@OLD}` + "\n", "/obj/old : /obj/effect/decal{@OLD; dir = 4}\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("script missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "/obj/old{name") > strings.Index(text, "/obj/old : ") {
		t.Fatalf("variant rule must precede the path rule:\n%s", text)
	}

	// Applying the exported script sequentially matches the plan's transform.
	parsed, errs := updatepaths.Parse("1_EXPORT.txt", script)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	set := updatepaths.NewSet(parsed)
	unknown := []string{"/obj/old", "/obj/gone", "/obj/kept", "/obj/scripted"}
	planOnly := NewPlan()
	for path, decision := range plan.Paths {
		if path != "/obj/scripted" {
			planOnly.Paths[path] = decision
		}
	}
	planOnly.Variants = plan.Variants
	transformer, err := Compile(planOnly, target, unknown, Resolvers{})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []updatepaths.Instance{{Path: "/obj/old", Vars: map[string]string{"name": `"x"`}}, {Path: "/obj/old", Vars: map[string]string{"dir": "2"}}, {Path: "/obj/kept"}} {
		viaPlan, resolved, err := transformer.Instance(in)
		if err != nil {
			t.Fatal(err)
		}
		if !resolved {
			viaPlan = []updatepaths.Instance{in}
		}
		viaScript, _ := set.Apply(in, target.Exists)
		if !reflect.DeepEqual(viaPlan, viaScript) {
			t.Fatalf("%v: plan %#v script %#v", in, viaPlan, viaScript)
		}
	}
}

func TestScriptNames(t *testing.T) {
	for name, want := range map[string]bool{"12345_RENAME.txt": true, "1_a-b.txt": true, "RENAME.txt": false, "1_../x.txt": false, "1_a.txt.exe": false} {
		if ValidScriptName(name) != want {
			t.Errorf("%q", name)
		}
	}
}

func TestMemoryRemembersAndReplaces(t *testing.T) {
	memory := NewMemory()
	key := EnvironmentKey("C:/code/tgstation.dme")
	plan := NewPlan()
	plan.Paths["/obj/old"] = ruleDecision(RepathRule("/obj/old", "/obj/a", nil))
	plan.Paths["/obj/scripted"] = Decision{Kind: Apply, Via: ViaScripts}
	if added := memory.Remember(key, plan); added != 1 {
		t.Fatalf("added %d", added)
	}
	plan.Paths["/obj/old"] = ruleDecision(RepathRule("/obj/old", "/obj/b", nil))
	memory.Remember(key, plan)
	data, err := memory.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMemory(data)
	if err != nil {
		t.Fatal(err)
	}
	rules, errs := decoded.Rules(key)
	if len(errs) != 0 || len(rules) != 1 || rules[0].Outputs[0].Path != "/obj/b" {
		t.Fatalf("rules = %#v errs %v", rules, errs)
	}
	if _, err := DecodeMemory([]byte(`{"Version": 99}`)); err == nil {
		t.Fatal("accepted an unknown memory version")
	}
}

func TestRememberedDecisionsAreCertain(t *testing.T) {
	target := testTarget(t)
	rules := []updatepaths.Rule{RepathRule("/obj/old", "/obj/effect/decal", nil)}
	p := suggestOne(t, entry("/obj/old", 2, nil), Sources{Target: target, Remembered: rules}, AutoCertain)
	if p.Auto != 0 || p.Candidates[0].Decision.Via != ViaRemembered {
		t.Fatalf("proposal = %#v", p)
	}
	transformer, err := Compile(DefaultPlan([]Proposal{p}), target, []string{"/obj/old"}, Resolvers{Remembered: rules})
	if err != nil {
		t.Fatal(err)
	}
	tiles := map[model.Coord]model.TileState{{X: 1, Y: 1, Z: 1}: {Prefabs: []model.PrefabState{prefab(t, "/obj/old", nil)}}}
	changes, _, err := BuildChanges(context.Background(), visitAll(tiles), readMap(tiles), transformer, nil, Defaults{})
	if err != nil || len(changes) != 1 || changes[0].After.Prefabs[0].Path != "/obj/effect/decal" {
		t.Fatalf("changes = %#v err %v", changes, err)
	}
}
