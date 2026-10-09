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
