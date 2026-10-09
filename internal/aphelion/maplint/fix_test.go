package maplint

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fakeTypes is a TypeTree over explicit per-type definitions. Values inherit
// from the nearest ancestor that sets them.
type fakeTypes map[string]map[string]string

func (f fakeTypes) Subtypes(path string) []string {
	var out []string
	for p := range f {
		if strings.HasPrefix(p, path+"/") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func (f fakeTypes) OwnVars(path string) []string {
	var out []string
	for name := range f[path] {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (f fakeTypes) Value(path, name string) (string, bool) {
	for p := path; p != ""; p = p[:max(0, strings.LastIndex(p, "/"))] {
		if v, ok := f[p][name]; ok {
			return v, true
		}
	}
	return "", false
}

func fixPaths(atoms []Atom) []string {
	out := make([]string, len(atoms))
	for i, a := range atoms {
		out[i] = a.Path
		if len(a.Vars) != 0 {
			var kv []string
			for k, v := range a.Vars {
				kv = append(kv, k+"="+v)
			}
			sort.Strings(kv)
			out[i] += "{" + strings.Join(kv, ";") + "}"
		}
	}
	return out
}

func expectFix(t *testing.T, got TileFix, wantAtoms []string, wantKinds ...FixKind) {
	t.Helper()
	if paths := fixPaths(got.Atoms); !reflect.DeepEqual(paths, wantAtoms) {
		t.Fatalf("atoms = %q, want %q", paths, wantAtoms)
	}
	var kinds []FixKind
	for _, f := range got.Fixes {
		kinds = append(kinds, f.Kind)
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("fix kinds = %v, want %v", kinds, wantKinds)
	}
	if len(got.Source) != len(got.Atoms) {
		t.Fatalf("source has %d entries for %d atoms", len(got.Source), len(got.Atoms))
	}
}

const fixRules = `
/obj/structure/cable:
  banned_neighbors:
    /obj/structure/cable:
      identical: true
`

func TestFixRemovesLaterIdenticalDuplicate(t *testing.T) {
	rs := mustLoad(t, map[string]string{"identical_cables.yml": fixRules})
	tile := []Atom{atom("/turf/open/floor"), atom("/obj/structure/cable", "icon_state", `"1-2"`), atom("/obj/structure/cable", "icon_state", `"1-2"`)}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{"/turf/open/floor", `/obj/structure/cable{icon_state="1-2"}`}, FixRemoveDuplicate)
	if !reflect.DeepEqual(got.Source, []int{0, 1}) || len(got.Remaining) != 0 {
		t.Fatalf("source %v remaining %v", got.Source, got.Remaining)
	}
	if got.Fixes[0].RuleFile != "identical_cables.yml" {
		t.Fatalf("fix rule = %q", got.Fixes[0].RuleFile)
	}
}

func TestFixRemovesAncestorSupersededBySubtype(t *testing.T) {
	rs := mustLoad(t, map[string]string{"multiple_lattice.yml": "/obj/structure/lattice:\n  banned_neighbors:\n    - /obj/structure/lattice\n"})
	tile := []Atom{atom("/turf/open/space/basic"), atom("/obj/structure/lattice"), atom("/obj/structure/lattice/catwalk")}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{"/turf/open/space/basic", "/obj/structure/lattice/catwalk"}, FixRemoveSuperseded)
	if !reflect.DeepEqual(got.Source, []int{0, 2}) {
		t.Fatalf("source = %v", got.Source)
	}
}

func TestFixLeavesUnrelatedNeighborsForReview(t *testing.T) {
	rs := mustLoad(t, map[string]string{"multiple_airlocks.yml": "/obj/machinery/door/airlock:\n  banned_neighbors:\n    - /obj/machinery/door/airlock\n"})
	tile := []Atom{atom("/obj/machinery/door/airlock/public"), atom("/obj/machinery/door/airlock/maintenance")}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{"/obj/machinery/door/airlock/public", "/obj/machinery/door/airlock/maintenance"})
	if len(got.Remaining) != 2 {
		t.Fatalf("remaining = %d, want 2", len(got.Remaining))
	}
}

func TestFixStripsBannedVariableEdits(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"cable_varedits.yml":  "/obj/structure/cable:\n  banned_variables: true\n",
		"banned_obj_vars.yml": "/obj:\n  banned_variables:\n    layer:\n",
	})
	tile := []Atom{atom("/obj/structure/cable", "icon_state", `"0-4"`, "d2", "4"), atom("/obj/structure/rack", "layer", "2.9", "name", `"Rack"`)}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{"/obj/structure/cable", `/obj/structure/rack{name="Rack"}`}, FixStripVariables, FixStripVariables)
	if tile[0].Vars["d2"] != "4" {
		t.Fatal("FixTile modified its input")
	}
	none := rs.FixTile("", tile, nil, AllFixes.Without(FixStripVariables))
	expectFix(t, none, []string{`/obj/structure/cable{d2=4;icon_state="0-4"}`, `/obj/structure/rack{layer=2.9;name="Rack"}`})
}

// Pixel offsets and dir position an object; stripping them would silently
// move it. They are only resolved through a subtype that sets the same values.
var posterTypes = fakeTypes{
	"/obj/structure/sign/poster":                                {"dir": "2", "pixel_y": "0"},
	"/obj/structure/sign/poster/a":                              {"name": `"A"`},
	"/obj/structure/sign/poster/a/directional/north":            {"dir": "1", "pixel_y": "32"},
	"/obj/structure/sign/poster/a/directional/south":            {"dir": "2", "pixel_y": "-32"},
	"/obj/structure/sign/poster/a/directional/east":             {"dir": "4", "pixel_x": "32"},
	"/obj/structure/sign/poster/a/torn":                         {"name": `"Torn A"`, "pixel_y": "32"},
	"/obj/structure/sign/poster/b":                              {},
	"/obj/structure/sign/poster/b/directional/north":            {"dir": "1", "pixel_y": "32"},
	"/obj/structure/sign/poster/b/directional/north/also_north": {},
}

const posterRules = "/obj/structure/sign/poster:\n  banned_variables:\n    pixel_x:\n    pixel_y:\n"

func TestFixPromotesPositionalEditsToDirectionalSubtype(t *testing.T) {
	rs := mustLoad(t, map[string]string{"posters_directionals.yml": posterRules})
	tile := []Atom{atom("/obj/structure/sign/poster/a", "pixel_y", "32", "desc", `"x"`)}
	got := rs.FixTile("", tile, posterTypes, AllFixes)
	expectFix(t, got, []string{`/obj/structure/sign/poster/a/directional/north{desc="x"}`}, FixPromoteSubtype)
	if !strings.Contains(got.Fixes[0].Detail, "/obj/structure/sign/poster/a/directional/north") {
		t.Fatalf("detail = %q", got.Fixes[0].Detail)
	}
}

// Mirrors tgstation's helper layout: an abstract `directional` root carrying
// catalogue data, and loader-synthesized names on every unnamed type.
func TestFixPromotionIgnoresCatalogueDataAndAbstractTypes(t *testing.T) {
	rs := mustLoad(t, map[string]string{"posters_directionals.yml": posterRules})
	const base = "/obj/structure/sign/poster/c"
	types := fakeTypes{
		"/obj/structure/sign/poster": {"dir": "2", "pixel_x": "0", "pixel_y": "0", "printable": "1", "name": `"poster"`},
		base:                         {"name": `"c"`},
		base + "/directional":        {"abstract_type": base + "/directional", "printable": "0", "name": `"directional"`},
		base + "/directional/east":   {"dir": "4", "pixel_x": "32", "name": `"east"`},
		base + "/directional/west":   {"dir": "8", "pixel_x": "-32", "name": `"west"`},
		base + "/directional/shiny":  {"dir": "4", "pixel_x": "32", "icon_state": `"shiny"`, "name": `"shiny"`},
		base + "/realname":           {"pixel_x": "32", "name": `"Something Else"`},
	}
	got := rs.FixTile("", []Atom{atom(base, "pixel_x", "32", "pixel_y", "0")}, types, DefaultFixes)
	expectFix(t, got, []string{base + "/directional/east"}, FixPromoteSubtype)
	if isAbstract(types, base+"/directional/east") || !isAbstract(types, base+"/directional") {
		t.Fatal("abstract detection")
	}
	// A real (non-synthesized) name and an appearance var still block it.
	if diff := differingVars(types, base, base+"/realname"); !reflect.DeepEqual(diff, []string{"name", "pixel_x"}) {
		t.Fatalf("realname diff = %v", diff)
	}
	if diff := differingVars(types, base, base+"/directional/shiny"); !reflect.DeepEqual(diff, []string{"dir", "icon_state", "pixel_x"}) {
		t.Fatalf("shiny diff = %v", diff)
	}
}

// A directional helper edited to face elsewhere becomes its sibling helper.
func TestFixPromotesEditedDirectionalHelperToSibling(t *testing.T) {
	rs := mustLoad(t, map[string]string{"windoor_var_edits.yml": "/obj/machinery/door/window:\n  banned_variables:\n    dir:\n      deny: [1, 2, 4, 8]\n"})
	const root = "/obj/machinery/door/window/left"
	types := fakeTypes{
		"/obj/machinery/door/window": {"dir": "2", "name": `"windoor"`},
		root:                         {"name": `"left"`},
		root + "/directional":        {"abstract_type": root + "/directional", "name": `"directional"`},
		root + "/directional/north":  {"dir": "1", "name": `"north"`},
		root + "/directional/east":   {"dir": "4", "name": `"east"`},
		"/obj/machinery/door/window/right/directional/east": {"dir": "4", "icon_state": `"right"`},
	}
	got := rs.FixTile("", []Atom{atom(root+"/directional/north", "dir", "4", "name", `"Robotics Desk"`)}, types, DefaultFixes)
	expectFix(t, got, []string{root + `/directional/east{name="Robotics Desk"}`}, FixPromoteSubtype)
}

func TestFixNeverStripsPositionalEditsWithoutASubtype(t *testing.T) {
	rs := mustLoad(t, map[string]string{"posters_directionals.yml": posterRules})
	tile := []Atom{atom("/obj/structure/sign/poster/a", "pixel_y", "20")}
	got := rs.FixTile("", tile, posterTypes, DefaultFixes)
	expectFix(t, got, []string{"/obj/structure/sign/poster/a{pixel_y=20}"})
}

func TestFixRefusesAmbiguousPromotion(t *testing.T) {
	rs := mustLoad(t, map[string]string{"posters_directionals.yml": posterRules})
	// b/directional/north and its empty child set identical values.
	tile := []Atom{atom("/obj/structure/sign/poster/b", "pixel_y", "32")}
	got := rs.FixTile("", tile, posterTypes, DefaultFixes)
	expectFix(t, got, []string{"/obj/structure/sign/poster/b{pixel_y=32}"})
}

func TestFixRemovesBannedObjectsButNeverTurfsOrAreas(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"merge_conflict_marker.yml": "/obj/merge_conflict_marker:\n  banned: true\n",
		"base_turf.yml":             "=/turf:\n  banned: true\n",
	})
	tile := []Atom{atom("/area/station"), atom("/turf"), atom("/obj/merge_conflict_marker")}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{"/area/station", "/turf"}, FixRemoveBanned)
	if len(got.Remaining) != 1 || got.Remaining[0].RuleFile != "base_turf.yml" {
		t.Fatalf("remaining = %v", messages(got.Remaining))
	}
	kept := rs.FixTile("", tile, nil, AllFixes.Without(FixRemoveBanned))
	expectFix(t, kept, []string{"/area/station", "/turf", "/obj/merge_conflict_marker"})
}

// A fix that resolves one violation but introduces another is rejected.
func TestFixRejectsCandidatesThatIntroduceViolations(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"cable_varedits.yml": "/obj/structure/cable:\n  banned_variables: true\n",
		"identical.yml":      fixRules,
	})
	// Stripping the edit would make the two cables identical duplicates; the
	// planner must then also remove the duplicate, never stop half way.
	tile := []Atom{atom("/obj/structure/cable"), atom("/obj/structure/cable", "d1", "1")}
	got := rs.FixTile("", tile, nil, AllFixes.Without(FixRemoveDuplicate))
	expectFix(t, got, []string{"/obj/structure/cable", "/obj/structure/cable{d1=1}"})
	all := rs.FixTile("", tile, nil, AllFixes)
	if len(all.Remaining) != 0 || len(all.Atoms) != 1 {
		t.Fatalf("with duplicates enabled: atoms %q remaining %q", fixPaths(all.Atoms), messages(all.Remaining))
	}
}

// Names and descriptions are content: they are corrected, never deleted.
func TestFixCapitalizesDeniedTextInsteadOfStrippingIt(t *testing.T) {
	rs := mustLoad(t, map[string]string{"door_name_capitalization.yml": "/obj/machinery/door:\n  banned_variables:\n    name:\n      deny: { pattern: '.*\\s(?!of|and|to)[a-z].*' }\n"})
	tile := []Atom{atom("/obj/machinery/door/airlock", "name", `"chemistry shutters of doom"`)}
	got := rs.FixTile("", tile, nil, AllFixes)
	expectFix(t, got, []string{`/obj/machinery/door/airlock{name="Chemistry Shutters of Doom"}`}, FixCapitalizeText)
	none := rs.FixTile("", tile, nil, AllFixes.Without(FixCapitalizeText))
	expectFix(t, none, []string{`/obj/machinery/door/airlock{name="chemistry shutters of doom"}`})
	// Embedded expressions are left alone.
	expr := rs.FixTile("", []Atom{atom("/obj/machinery/door/airlock", "name", `"door [x] here"`)}, nil, AllFixes)
	if expr.Changed() {
		t.Fatalf("rewrote an embedded expression: %q", fixPaths(expr.Atoms))
	}
}

// Areas are never drawn by direction or offset, so those edits are inert and
// always strippable. Elsewhere stripping them is a separate opt-in kind.
func TestFixStripsPositionalEditsOnAreasAndOnlyOptInElsewhere(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"area_varedits.yml":  "/area:\n  banned_variables: true\n",
		"cable_varedits.yml": "/obj/structure/cable:\n  banned_variables: true\n",
	})
	tile := []Atom{atom("/area/station/janitor", "dir", "8"), atom("/obj/structure/cable", "dir", "8")}
	got := rs.FixTile("", tile, nil, DefaultFixes)
	expectFix(t, got, []string{"/area/station/janitor", "/obj/structure/cable{dir=8}"}, FixStripVariables)
	all := rs.FixTile("", tile, nil, DefaultFixes.With(FixStripPositional))
	expectFix(t, all, []string{"/area/station/janitor", "/obj/structure/cable"}, FixStripVariables, FixStripPositional)
	if DefaultFixes.Has(FixStripPositional) {
		t.Fatal("positional stripping must be opt-in")
	}
	// Layers connect pipes and cables: they are placement, not decoration.
	pipes := mustLoad(t, map[string]string{"atmos_var_edits.yml": "/obj/machinery/atmospherics:\n  banned_variables:\n    piping_layer:\n"})
	tank := []Atom{atom("/obj/machinery/atmospherics/components/tank/air", "piping_layer", "4")}
	expectFix(t, pipes.FixTile("", tank, nil, DefaultFixes), []string{"/obj/machinery/atmospherics/components/tank/air{piping_layer=4}"})
}

func TestFixKindsRoundTrip(t *testing.T) {
	k := AllFixes.Without(FixPromoteSubtype)
	if k.Has(FixPromoteSubtype) || !k.Has(FixRemoveDuplicate) {
		t.Fatal("Without/Has disagree")
	}
	for _, kind := range FixKindList {
		if kind.String() == "" || kind.Description() == "" {
			t.Fatalf("kind %d lacks text", kind)
		}
	}
}
