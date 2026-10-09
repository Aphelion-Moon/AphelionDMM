package lighting

import (
	"reflect"
	"testing"
)

func TestSwitchOrdinaryLightUsesLightOn(t *testing.T) {
	typ := map[string]string{"light_range": "3", "light_power": "1", "light_on": "TRUE"}
	sw, ok := LightSwitch("/obj/item/flashlight/lamp", lookup(typ), lookup(typ), DefaultProfiles())
	if !ok || !sw.On {
		t.Fatalf("switch = %+v, %v; want a light that is on", sw, ok)
	}
	if want := []VarChange{{Name: "light_on", Value: "FALSE"}}; !reflect.DeepEqual(sw.Toggle, want) {
		t.Fatalf("turn off = %+v, want %+v", sw.Toggle, want)
	}

	// Turning it back on removes the edit: TRUE is the type default.
	inst := map[string]string{"light_range": "3", "light_power": "1", "light_on": "FALSE"}
	sw, ok = LightSwitch("/obj/item/flashlight/lamp", lookup(inst), lookup(typ), DefaultProfiles())
	if !ok || sw.On {
		t.Fatalf("switch = %+v, %v; want a light that is off", sw, ok)
	}
	if want := []VarChange{{Name: "light_on", Remove: true}}; !reflect.DeepEqual(sw.Toggle, want) {
		t.Fatalf("turn on = %+v, want %+v", sw.Toggle, want)
	}

	// The parser hands type defaults over evaluated: TRUE arrives as "1".
	evaluated := map[string]string{"light_range": "3", "light_on": "1"}
	sw, _ = LightSwitch("/obj/item/flashlight/lamp", lookup(inst), lookup(evaluated), DefaultProfiles())
	if want := []VarChange{{Name: "light_on", Remove: true}}; !reflect.DeepEqual(sw.Toggle, want) {
		t.Fatalf("turn on over an evaluated default = %+v, want %+v", sw.Toggle, want)
	}
}

func TestSwitchFixtureUsesStatusAndEmptyIcon(t *testing.T) {
	typ := map[string]string{"status": "0", "base_state": `"tube"`, "icon_state": `"tube"`, "brightness": "7.5", "bulb_power": "0.9"}
	sw, ok := LightSwitch("/obj/machinery/light/warm", lookup(typ), lookup(typ), DefaultProfiles())
	if !ok || !sw.On {
		t.Fatalf("switch = %+v, %v; want a lit fixture", sw, ok)
	}
	want := []VarChange{{Name: "status", Value: "1"}, {Name: "icon_state", Value: `"tube-empty"`}}
	if !reflect.DeepEqual(sw.Toggle, want) {
		t.Fatalf("turn off = %+v, want %+v", sw.Toggle, want)
	}

	// A mapped broken fixture turns on with explicit edits, since its type
	// defaults are the broken ones.
	broken := map[string]string{"status": "2", "base_state": `"tube"`, "icon_state": `"tube-broken"`}
	sw, ok = LightSwitch("/obj/machinery/light/broken", lookup(broken), lookup(broken), DefaultProfiles())
	if !ok || sw.On {
		t.Fatalf("switch = %+v, %v; want a dark fixture", sw, ok)
	}
	want = []VarChange{{Name: "status", Value: "0"}, {Name: "icon_state", Value: `"tube"`}}
	if !reflect.DeepEqual(sw.Toggle, want) {
		t.Fatalf("turn on = %+v, want %+v", sw.Toggle, want)
	}
}

func TestSwitchIgnoresAtomsWithoutLight(t *testing.T) {
	if _, ok := LightSwitch("/obj/structure/table", lookup(nil), lookup(nil), DefaultProfiles()); ok {
		t.Fatal("an atom without light_range is not switchable")
	}
	// Off by type: space (starlight is derived) and items the game lights at
	// runtime, such as a gun's flashlight. Real MiniStation had 58,156 space
	// tiles that would otherwise offer "Turn Light On".
	offByType := map[string]string{"light_range": "2", "light_on": "0"}
	for _, path := range []string{"/turf/open/space/basic", "/obj/item/gun/energy/laser"} {
		if _, ok := LightSwitch(path, lookup(offByType), lookup(offByType), DefaultProfiles()); ok {
			t.Fatalf("%s is off by type and must not be switchable", path)
		}
	}
	noSupport := map[string]string{"light_range": "2", "light_system": "0"}
	if _, ok := LightSwitch("/obj/thing", lookup(noSupport), lookup(noSupport), DefaultProfiles()); ok {
		t.Fatal("NO_LIGHT_SUPPORT is not switchable")
	}
}

func TestFixtureHonoursColorAndStatus(t *testing.T) {
	base := map[string]string{"brightness": "8", "bulb_power": "1", "bulb_colour": `"#ffffff"`, "dir": "2"}
	tinted := map[string]string{"color": `"#ff0000"`}
	for k, v := range base {
		tinted[k] = v
	}
	ex := ExtractSource(Atom{Path: "/obj/machinery/light", Var: lookup(tinted)}, DefaultProfiles())
	if !ex.Emits || ex.Source.Color != (RGB{1, 0, 0}) {
		t.Fatalf("color override: %+v", ex)
	}
	empty := map[string]string{"status": "1"}
	for k, v := range base {
		empty[k] = v
	}
	if ex := ExtractSource(Atom{Path: "/obj/machinery/light", Var: lookup(empty)}, DefaultProfiles()); ex.Emits || ex.Reason != ReasonNoBulb {
		t.Fatalf("empty fixture: %+v", ex)
	}
}
