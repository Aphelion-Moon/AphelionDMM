package maplint

import (
	"testing"
)

func auditFixture(t *testing.T) (*RuleSet, *Audit) {
	t.Helper()
	rs := mustLoad(t, map[string]string{"x.yml": "/obj/merge_conflict_marker:\n  banned: true\n"})
	types := fakeTypes{
		"/turf/open/floor": {"icon": "'icons/turf/floors.dmi'", "icon_state": `"floor"`, "dir": "2", "name": `"floor"`},
		"/obj/chair":       {"icon": "'icons/obj/chairs.dmi'", "icon_state": `"chair"`, "dir": "2"},
	}
	dirs := map[string]int{"floor": 1, "chair": 4, "floor_corner": 4}
	return rs, &Audit{Types: types, Dirs: func(icon, state string) (int, bool) { d, ok := dirs[state]; return d, ok }}
}

func TestAuditFindsRedundantAndInertEditsOnlyWhenAttached(t *testing.T) {
	rs, audit := auditFixture(t)
	tile := []Atom{
		atom("/turf/open/floor", "dir", "4", "name", `"floor"`),
		atom("/obj/chair", "dir", "4"),
		atom("/turf/open/floor", "icon_state", `"floor_corner"`, "dir", "8"),
	}
	if got := rs.CheckTile(tile); len(got) != 0 {
		t.Fatalf("plain rule set reported audit findings: %v", messages(got))
	}
	got := rs.WithAudit(audit).CheckTile(tile)
	expectMsgs(t, got,
		"Typepath /turf/open/floor has a dir edit its sprite cannot show (floor has one direction): dir = 4",
		"Typepath /turf/open/floor has an edit equal to its default: name = \"floor\"",
	)
	if got[0].Kind != KindInertDir || got[1].Kind != KindRedundantEdit || got[0].RuleFile != AuditRuleFile {
		t.Fatalf("kinds = %v %v file %q", got[0].Kind, got[1].Kind, got[0].RuleFile)
	}
	// A thermomachine's map sprite has one direction, but dir picks its pipe side.
	audit.Types.(fakeTypes)["/obj/machinery/freezer"] = map[string]string{"icon": "'icons/obj/x.dmi'", "icon_state": `"floor"`, "dir": "2"}
	if got := rs.WithAudit(audit).CheckTile([]Atom{atom("/obj/machinery/freezer", "dir", "8")}); len(got) != 0 {
		t.Fatalf("machinery dir flagged as inert: %v", messages(got))
	}
}

func TestAuditFixesStripOnlyTheInertEdits(t *testing.T) {
	rs, audit := auditFixture(t)
	tile := []Atom{atom("/turf/open/floor", "dir", "4", "name", `"floor"`, "desc", `"kept"`), atom("/obj/chair", "dir", "4")}
	got := rs.WithAudit(audit).FixTile("", tile, audit.Types, DefaultFixes)
	expectFix(t, got, []string{`/turf/open/floor{desc="kept"}`, "/obj/chair{dir=4}"}, FixStripInertDir, FixStripRedundant)
	none := rs.WithAudit(audit).FixTile("", tile, audit.Types, DefaultFixes.Without(FixStripInertDir).Without(FixStripRedundant))
	if none.Changed() {
		t.Fatal("disabled audit kinds still changed the tile")
	}
}

// Fixtures take their light from the bulb variables; update() overwrites
// light_range, light_power and light_color, and light_on never applies.
func TestAuditFlagsFixtureLightEditsAndMovesThemToBulbVars(t *testing.T) {
	rs, audit := auditFixture(t)
	audit.Types.(fakeTypes)["/obj/machinery/light"] = map[string]string{"brightness": "7.5", "bulb_power": "0.9", "bulb_colour": `"#FFF6ED"`}
	audit.Types.(fakeTypes)["/obj/machinery/light_switch"] = map[string]string{}
	tile := []Atom{
		atom("/obj/machinery/light", "light_color", `"#d1dfff"`, "light_power", "0.9", "light_on", "0"),
		atom("/obj/machinery/light_switch", "light_color", `"#ff0000"`),
	}
	got := rs.WithAudit(audit).CheckTile(tile)
	expectMsgs(t, got,
		"Typepath /obj/machinery/light sets light_color, which a light fixture overwrites in game; set bulb_colour instead: light_color = \"#d1dfff\"",
		"Typepath /obj/machinery/light sets light_on, which a light fixture overwrites in game: light_on = 0",
		"Typepath /obj/machinery/light sets light_power, which a light fixture overwrites in game; set bulb_power instead: light_power = 0.9",
	)
	for _, v := range got {
		if v.Kind != KindFixtureLight {
			t.Fatalf("kind = %v", v.Kind)
		}
	}
	if DefaultFixes.Has(FixMoveFixtureLight) {
		t.Fatal("moving fixture light edits changes the in-game look; it must be opt-in")
	}
	if none := rs.WithAudit(audit).FixTile("", tile, audit.Types, DefaultFixes); none.Changed() {
		t.Fatalf("default fixes changed fixture light edits: %v", fixPaths(none.Atoms))
	}
	// light_power 0.9 equals the bulb_power default, so it is dropped rather
	// than restated; light_on has no bulb counterpart.
	fixed := rs.WithAudit(audit).FixTile("", tile, audit.Types, DefaultFixes.With(FixMoveFixtureLight))
	expectFix(t, fixed, []string{`/obj/machinery/light{bulb_colour="#d1dfff"}`, `/obj/machinery/light_switch{light_color="#ff0000"}`},
		FixMoveFixtureLight, FixMoveFixtureLight, FixMoveFixtureLight)

	// An existing bulb edit wins; the dead edit is only removed.
	kept := rs.WithAudit(audit).FixTile("", []Atom{atom("/obj/machinery/light", "light_color", `"#d1dfff"`, "bulb_colour", `"#00ff00"`)}, audit.Types, DefaultFixes.With(FixMoveFixtureLight))
	expectFix(t, kept, []string{`/obj/machinery/light{bulb_colour="#00ff00"}`}, FixMoveFixtureLight)
}
