package editing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// scopeFixture models the inheritance every atom shares: /atom owns dir and
// offset defaults, and nothing below it restates them unless it is a real
// override.
func scopeFixture() PrefabLookup {
	atom := &dmvars.MutableVariables{}
	atom.Put("dir", "2")
	atom.Put("pixel_x", "0")
	atom.Put("pixel_y", "0")
	values := map[string]*dmvars.Variables{"/atom": atom.ToImmutable()}
	child := func(path, parent string, pairs ...string) {
		v := &dmvars.MutableVariables{}
		for i := 0; i < len(pairs); i += 2 {
			v.Put(pairs[i], pairs[i+1])
		}
		immutable := v.ToImmutable()
		immutable.LinkParent(values[parent])
		values[path] = immutable
	}
	child("/turf", "/atom")
	child("/turf/open/floor", "/turf")
	child("/area", "/atom")
	child("/area/station", "/area")
	child("/obj", "/atom")
	child("/obj/crate", "/obj")
	child("/obj/crate/offset", "/obj/crate", "pixel_x", "3")
	child("/obj/chair", "/obj", "dir", "1") // Type-owned facing.
	return func(path string) *dmvars.Variables { return values[path] }
}

func TestRotateAndMirrorLeavePlainAtomsIdentical(t *testing.T) {
	lookup := scopeFixture()
	for _, path := range []string{"/turf/open/floor", "/area/station", "/obj/crate", "/obj/crate/offset"} {
		source := dmmprefab.New(0, path, dmvars.FromParent(lookup(path)))
		for _, clockwise := range []bool{false, true} {
			got, err := rotatePrefab(source, clockwise, lookup)
			if err != nil || got != source {
				t.Fatalf("rotate %s: got %p want identical %p (%v)", path, got, source, err)
			}
		}
		for _, axis := range []MirrorAxis{MirrorHorizontal, MirrorVertical} {
			got, err := mirrorPrefab(source, axis, lookup)
			if err != nil || got != source {
				t.Fatalf("mirror %s: got %p want identical %p (%v)", path, got, source, err)
			}
		}
	}
}

// A type that declares dir but whose sprite has a single direction gains
// nothing visible from a turn, so rotation leaves it alone instead of adding
// an edit. An explicit dir edit, or an unknown sprite, still turns.
func TestTypeOwnedDirTurnsOnlyWhenTheSpriteHasDirections(t *testing.T) {
	lookup := scopeFixture()
	sprites := map[string]int{"wall": 1, "chair": 4}
	SetSpriteDirections(func(icon, state string) (int, bool) { d, ok := sprites[state]; return d, ok })
	t.Cleanup(func() { SetSpriteDirections(nil) })
	owned := func(state string) *dmmprefab.Prefab {
		return dmmprefab.New(0, "/obj/chair", dmvars.Set(dmvars.FromParent(lookup("/obj/chair")), "icon_state", `"`+state+`"`))
	}
	for state, turns := range map[string]bool{"wall": false, "chair": true, "unknown": true} {
		source := owned(state)
		got, err := rotatePrefab(source, true, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if turned := got != source; turned != turns {
			t.Fatalf("%s: turned=%v want %v", state, turned, turns)
		}
		mirrored, _ := mirrorPrefab(source, MirrorVertical, lookup)
		if mirroredTurned := mirrored != source; mirroredTurned != turns {
			t.Fatalf("%s mirror: turned=%v want %v", state, mirroredTurned, turns)
		}
	}
	// Machinery dir picks pipe sides even with a one-direction map sprite.
	machine := dmmprefab.New(0, "/obj/machinery/freezer", dmvars.Set(dmvars.FromParent(lookup("/obj/chair")), "icon_state", `"wall"`))
	if got, _ := rotatePrefab(machine, true, func(path string) *dmvars.Variables {
		if path == "/obj/machinery/freezer" || path == "/obj/machinery" {
			return lookup("/obj/chair")
		}
		return lookup(path)
	}); got == machine {
		t.Fatal("machinery with a single-direction map sprite must still turn")
	}
	explicit := dmmprefab.New(0, "/obj/chair", dmvars.Set(owned("wall").Vars(), "dir", "4"))
	if got, _ := rotatePrefab(explicit, true, lookup); got == explicit || got.Vars().ValueV("dir", "") != "2" {
		t.Fatal("an explicit dir edit on a single-direction sprite must still turn")
	}
}

func TestRotateAndMirrorNeverWriteDirOntoArea(t *testing.T) {
	lookup := scopeFixture()
	explicit := dmvars.Set(dmvars.FromParent(lookup("/area/station")), "dir", "1")
	source := dmmprefab.New(0, "/area/station", explicit)
	if got, err := rotatePrefab(source, true, lookup); err != nil || got != source {
		t.Fatal("area was rotated", err)
	}
	if got, err := mirrorPrefab(source, MirrorHorizontal, lookup); err != nil || got != source {
		t.Fatal("area was mirrored", err)
	}
}

func TestExplicitAndTypeOwnedDirStillTurn(t *testing.T) {
	lookup := scopeFixture()
	for _, path := range []string{"/turf/open/floor", "/obj/crate"} {
		source := dmmprefab.New(0, path, dmvars.Set(dmvars.FromParent(lookup(path)), "dir", "1"))
		got, err := rotatePrefab(source, true, lookup)
		if err != nil || got.Vars().ValueV("dir", "") != "4" {
			t.Fatalf("explicit dir on %s did not rotate: %v", path, err)
		}
		for range 3 {
			if got, err = rotatePrefab(got, true, lookup); err != nil {
				t.Fatal(err)
			}
		}
		if got.Vars().ValueV("dir", "") != "1" {
			t.Fatalf("explicit dir on %s did not return after four turns", path)
		}
		mirrored, err := mirrorPrefab(source, MirrorVertical, lookup)
		if err != nil || mirrored.Vars().ValueV("dir", "") != "2" {
			t.Fatalf("explicit dir on %s did not mirror: %v", path, err)
		}
	}
	chair := dmmprefab.New(0, "/obj/chair", dmvars.FromParent(lookup("/obj/chair")))
	got, err := rotatePrefab(chair, true, lookup)
	if err != nil || got.Vars().ValueV("dir", "") != "4" {
		t.Fatal("type-owned facing did not rotate", err)
	}
	if _, explicit := scopeFixture()("/obj/crate").ExplicitValue("dir"); explicit {
		t.Fatal("fixture must not restate dir below /atom")
	}
}

func TestExplicitOffsetsRotateWithoutInventingInheritedOnes(t *testing.T) {
	lookup := scopeFixture()
	source := dmmprefab.New(0, "/obj/crate", dmvars.Set(dmvars.FromParent(lookup("/obj/crate")), "pixel_x", "5"))
	got, err := rotatePrefab(source, true, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Vars().ExplicitValue("dir"); ok {
		t.Fatal("dir invented for an object that never set it")
	}
	if got.Vars().ValueV("pixel_x", "") != "0" || got.Vars().ValueV("pixel_y", "") != "-5" {
		t.Fatalf("explicit offset: %s,%s", got.Vars().ValueV("pixel_x", ""), got.Vars().ValueV("pixel_y", ""))
	}
}

func assertSameRepresentation(t *testing.T, got, want *dmmprefab.Prefab) {
	t.Helper()
	if got.Path() != want.Path() {
		t.Fatalf("path %s, want %s", got.Path(), want.Path())
	}
	names := map[string]bool{}
	for _, n := range got.Vars().Iterate() {
		names[n] = true
	}
	for _, n := range want.Vars().Iterate() {
		names[n] = true
	}
	for n := range names {
		g, gok := got.Vars().ExplicitValue(n)
		w, wok := want.Vars().ExplicitValue(n)
		if g != w || gok != wok {
			t.Fatalf("%s: explicit %q/%v, want %q/%v", n, g, gok, w, wok)
		}
	}
}

func TestDirectionalHelpersRoundTripThroughRotationAndMirror(t *testing.T) {
	family, lookup := directionalFixture()
	for _, name := range []string{"north", "east", "south", "west", "northeast", "southeast", "southwest", "northwest"} {
		start := dmvars.Set(dmvars.FromParent(lookup(family+"/"+name)), "name", `"custom"`)
		source := dmmprefab.New(0, family+"/"+name, start)
		for _, clockwise := range []bool{false, true} {
			got := source
			for range 4 {
				var err error
				if got, err = rotatePrefab(got, clockwise, lookup); err != nil {
					t.Fatal(err)
				}
			}
			assertSameRepresentation(t, got, source)
		}
		for _, axis := range []MirrorAxis{MirrorHorizontal, MirrorVertical} {
			once, err := mirrorPrefab(source, axis, lookup)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"dir", "pixel_x", "pixel_y"} {
				if _, ok := once.Vars().ExplicitValue(n); ok {
					t.Fatalf("%s mirrored %s restated %s", name, family, n)
				}
			}
			twice, err := mirrorPrefab(once, axis, lookup)
			if err != nil {
				t.Fatal(err)
			}
			assertSameRepresentation(t, twice, source)
		}
	}
}

func TestMirrorSwitchesHelperVariantsPerAxis(t *testing.T) {
	family, lookup := directionalFixture()
	for _, test := range []struct {
		axis       MirrorAxis
		from, want string
	}{
		{MirrorHorizontal, "west", "east"}, {MirrorHorizontal, "east", "west"},
		{MirrorHorizontal, "north", "north"}, {MirrorVertical, "north", "south"},
		{MirrorVertical, "south", "north"}, {MirrorVertical, "west", "west"},
		{MirrorHorizontal, "northeast", "northwest"}, {MirrorVertical, "northeast", "southeast"},
		{MirrorHorizontal, "southwest", "southeast"}, {MirrorVertical, "southwest", "northwest"},
	} {
		source := dmmprefab.New(0, family+"/"+test.from, dmvars.FromParent(lookup(family+"/"+test.from)))
		got, err := mirrorPrefab(source, test.axis, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if got.Path() != family+"/"+test.want || got.Vars().Parent() != lookup(family+"/"+test.want) {
			t.Fatalf("mirror %s axis %d -> %s, want %s", test.from, test.axis, got.Path(), test.want)
		}
		if test.from == test.want && got != source {
			t.Fatal("symmetric helper changed")
		}
		if _, ok := got.Vars().ExplicitValue("dir"); ok {
			t.Fatal("helper path must supply dir")
		}
	}
	// Explicit offsets on a helper mirror to the new helper's value and drop.
	source := dmmprefab.New(0, family+"/west", dmvars.Set(dmvars.FromParent(lookup(family+"/west")), "pixel_x", "-27"))
	got, err := mirrorPrefab(source, MirrorHorizontal, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Vars().ExplicitValue("pixel_x"); ok || got.Vars().ValueV("pixel_x", "") != "27" {
		t.Fatal("redundant offset not dropped")
	}
}

func TestTypepathSpellingsAreRecognisedAsHelperFamilies(t *testing.T) {
	family, lookup := directionalFixture()
	for _, spelling := range []string{family, "  " + family + " ", `"` + family + `"`, family + "/"} {
		parent := &dmvars.MutableVariables{}
		parent.Put("abstract_type", spelling)
		families := parent.ToImmutable()
		wrapped := func(path string) *dmvars.Variables {
			if path == family {
				return families
			}
			return lookup(path)
		}
		source := dmmprefab.New(0, family+"/north", dmvars.FromParent(lookup(family+"/north")))
		got, err := rotatePrefab(source, true, wrapped)
		if err != nil || got.Path() != family+"/east" {
			t.Fatalf("abstract_type %q: %v %v", spelling, got.Path(), err)
		}
	}
}

// The parser stores token-pasted typepaths as the bare path, so held rotation
// reaches helpers on real environments. This parses a tiny environment through
// the real parser rather than assuming that form.
func TestParsedMappingHelpersRotateAndMirrorFromRealParser(t *testing.T) {
	source := `#define NORTH 1
#define SOUTH 2
#define EAST 4
#define WEST 8
#define NORTHEAST 5
#define NORTHWEST 9
#define MAPPING_DIRECTIONAL_HELPERS(path, offset) ##path/directional {\
	abstract_type = ##path/directional; \
} \
##path/directional/north {\
	dir = NORTH; \
	pixel_y = offset; \
} \
##path/directional/south {\
	dir = SOUTH; \
	pixel_y = -offset; \
} \
##path/directional/east {\
	dir = EAST; \
	pixel_x = offset; \
} \
##path/directional/west {\
	dir = WEST; \
	pixel_x = -offset; \
}
#define MAPPING_DIAGONAL_HELPERS(path, offset) ##path/directional/northeast {\
	dir = NORTHEAST; \
	pixel_x = offset; \
	pixel_y = offset; \
} \
##path/directional/northwest {\
	dir = NORTHWEST; \
	pixel_x = -offset; \
	pixel_y = offset; \
}
/atom
	var/abstract_type
	dir = 2
	pixel_x = 0
	pixel_y = 0
/obj
/obj/machinery
/obj/machinery/power
/obj/machinery/power/apc
/obj/machinery/power/apc/auto_name
/obj/machinery/firealarm
MAPPING_DIRECTIONAL_HELPERS(/obj/machinery/power/apc/auto_name, 25)
MAPPING_DIRECTIONAL_HELPERS(/obj/machinery/firealarm, 26)
MAPPING_DIAGONAL_HELPERS(/obj/machinery/firealarm, 26)
`
	path := filepath.Join(t.TempDir(), "helpers.dme")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	environment, err := dmenv.New(path)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(p string) *dmvars.Variables {
		if o := environment.Objects[p]; o != nil {
			return o.Vars
		}
		return nil
	}
	if got, _ := lookup("/obj/machinery/power/apc/auto_name/directional").ExplicitValue("abstract_type"); got != "/obj/machinery/power/apc/auto_name/directional" {
		t.Fatalf("parser stores abstract_type as %q", got)
	}
	cases := []struct {
		base  string
		start string
		right string
	}{
		{"/obj/machinery/power/apc/auto_name", "north", "east"},
		{"/obj/machinery/firealarm", "west", "north"},
		{"/obj/machinery/firealarm", "northeast", ""}, // No southeast helper: numeric fallback.
	}
	for _, c := range cases {
		start := c.base + "/directional/" + c.start
		original := dmmprefab.New(0, start, dmvars.FromParent(lookup(start)))
		got, err := rotatePrefab(original, true, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if c.right != "" && got.Path() != c.base+"/directional/"+c.right {
			t.Fatalf("held rotation of %s produced %s", start, got.Path())
		}
		for range 3 {
			if got, err = rotatePrefab(got, true, lookup); err != nil {
				t.Fatal(err)
			}
		}
		if c.right != "" {
			assertSameRepresentation(t, got, original)
		}
	}
	west := dmmprefab.New(0, "/obj/machinery/firealarm/directional/west", dmvars.FromParent(lookup("/obj/machinery/firealarm/directional/west")))
	mirrored, err := mirrorPrefab(west, MirrorHorizontal, lookup)
	if err != nil || mirrored.Path() != "/obj/machinery/firealarm/directional/east" {
		t.Fatalf("parsed mirror: %v %v", mirrored.Path(), err)
	}
	diagonal := dmmprefab.New(0, "/obj/machinery/firealarm/directional/northeast", dmvars.FromParent(lookup("/obj/machinery/firealarm/directional/northeast")))
	mirrored, err = mirrorPrefab(diagonal, MirrorHorizontal, lookup)
	if err != nil || mirrored.Path() != "/obj/machinery/firealarm/directional/northwest" {
		t.Fatalf("parsed diagonal mirror: %v %v", mirrored.Path(), err)
	}
	// An ordinary floor on a real parse is untouched too.
	floor := dmmprefab.New(0, "/obj/machinery", dmvars.FromParent(lookup("/obj/machinery")))
	if got, err := rotatePrefab(floor, true, lookup); err != nil || got != floor {
		t.Fatal("plain parsed object changed", err)
	}
}

func TestMirrorCallPathsShareHelperVariantsAndSkipPlainAtoms(t *testing.T) {
	family, lookup := directionalFixture()
	plain := scopeFixture()
	combined := func(path string) *dmvars.Variables {
		if v := lookup(path); v != nil {
			return v
		}
		return plain(path)
	}
	helper := dmmprefab.New(0, family+"/west", dmvars.FromParent(lookup(family+"/west")))
	floor := dmmprefab.New(0, "/turf/open/floor", dmvars.FromParent(plain("/turf/open/floor")))
	coord := util.Point{X: 1, Y: 1, Z: 1}
	tile := dmmap.Tile{Coord: coord}
	tile.Set(dmmap.Instances{dmminstance.New(coord, helper), dmminstance.New(coord, floor)})

	templated, err := TransformPlacementTemplate(context.Background(), []dmmap.Tile{tile}, PlacementMirrorHorizontal, combined)
	if err != nil {
		t.Fatal(err)
	}
	got := templated[0].Instances()
	if got[0].Prefab().Path() != family+"/east" || got[1].Prefab() != floor {
		t.Fatalf("template mirror: %s same-floor=%v", got[0].Prefab().Path(), got[1].Prefab() == floor)
	}

	m := &dmmap.Dmm{MaxX: 2, MaxY: 1, MaxZ: 1}
	left := &dmmap.Tile{Coord: coord}
	left.InstancesAdd(helper)
	right := &dmmap.Tile{Coord: util.Point{X: 2, Y: 1, Z: 1}}
	right.InstancesAdd(floor)
	m.Tiles = append(m.Tiles, left, right)
	plan, err := Mirror(m, util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 1}, 1, MirrorHorizontal, func(string) bool { return true }, combined)
	if err != nil {
		t.Fatal(err)
	}
	if p := plan.Tiles[1].Instances()[0].Prefab(); p.Path() != family+"/east" {
		t.Fatalf("Mirror selection: %s", p.Path())
	}
	if p := plan.Tiles[0].Instances()[0].Prefab(); p != floor {
		t.Fatal("Mirror selection rewrote a plain floor")
	}
}
