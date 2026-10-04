package editing

import (
	"context"
	"fmt"
	"os"
	"sdmm/internal/dmapi/dmenv"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func directionalFixture() (string, PrefabLookup) {
	family := "/obj/device/directional"
	parent := &dmvars.MutableVariables{}
	parent.Put("abstract_type", family)
	values := map[string]*dmvars.Variables{family: parent.ToImmutable()}
	for _, d := range []struct {
		name      string
		dir, x, y int
	}{
		{"north", 1, 0, 27}, {"east", 4, 27, 0}, {"south", 2, 0, -27}, {"west", 8, -27, 0},
		{"northeast", 5, 27, 27}, {"southeast", 6, 27, -27}, {"southwest", 10, -27, -27}, {"northwest", 9, -27, 27},
	} {
		v := &dmvars.MutableVariables{}
		v.Put("dir", fmt.Sprint(d.dir))
		v.Put("pixel_x", fmt.Sprint(d.x))
		v.Put("pixel_y", fmt.Sprint(d.y))
		v.Put("icon_state", fmt.Sprintf("%q", d.name))
		v.Put("pixel_w", "0")
		v.Put("pixel_z", "0")
		immutable := v.ToImmutable()
		immutable.LinkParent(values[family])
		values[family+"/"+d.name] = immutable
	}
	return family, func(path string) *dmvars.Variables { return values[path] }
}

func TestDirectionalRotationReparentsVariantsAndKeepsOverrides(t *testing.T) {
	family, lookup := directionalFixture()
	for _, start := range []struct{ source, right, left string }{
		{"north", "east", "west"}, {"northeast", "southeast", "northwest"},
	} {
		for _, clockwise := range []bool{false, true} {
			vars := dmvars.FromParent(lookup(family + "/" + start.source))
			vars = dmvars.Set(vars, "name", "\"custom device\"")
			vars = dmvars.Set(vars, "opaque", "CUSTOM_EXPRESSION")
			vars = dmvars.Set(vars, "pixel_w", "3")
			vars = dmvars.Set(vars, "pixel_z", "7")
			source := dmmprefab.New(0, family+"/"+start.source, vars)
			got, err := rotatePrefab(source, clockwise, lookup)
			if err != nil {
				t.Fatal(err)
			}
			target := start.left
			wx, wy := "-7", "3"
			if clockwise {
				target, wx, wy = start.right, "7", "-3"
			}
			wantPath := family + "/" + target
			if got.Path() != wantPath || got.Vars().Parent() != lookup(wantPath) {
				t.Fatalf("variant/parent = %s %p, want %s %p", got.Path(), got.Vars().Parent(), wantPath, lookup(wantPath))
			}
			if got.Vars().ValueV("icon_state", "") != fmt.Sprintf("%q", target) ||
				got.Vars().ValueV("name", "") != "\"custom device\"" || got.Vars().ValueV("opaque", "") != "CUSTOM_EXPRESSION" ||
				got.Vars().ValueV("pixel_w", "") != wx || got.Vars().ValueV("pixel_z", "") != wy {
				t.Fatal("target inheritance or explicit properties lost")
			}
			for _, name := range []string{"dir", "pixel_x", "pixel_y"} {
				if _, explicit := got.Vars().ExplicitValue(name); explicit {
					t.Fatalf("redundant target orientation override: %s", name)
				}
			}
			held := HeldPrefab{}
			held.SetSource(source)
			for range 4 {
				if err := held.Rotate(clockwise, lookup); err != nil {
					t.Fatal(err)
				}
			}
			if held.Value() != source {
				t.Fatal("four held turns did not restore original representation")
			}
			if source.Vars() != vars || source.Path() != family+"/"+start.source {
				t.Fatal("source changed")
			}
		}
	}
}

func TestDirectionalRotationMissingOrUnrecognizedVariantFallsBack(t *testing.T) {
	family, lookup := directionalFixture()
	source := dmmprefab.New(0, family+"/north", dmvars.FromParent(lookup(family+"/north")))
	missing := func(path string) *dmvars.Variables {
		if path == family+"/east" {
			return nil
		}
		return lookup(path)
	}
	for _, resolve := range []PrefabLookup{nil, missing, func(path string) *dmvars.Variables {
		if path == family {
			return (&dmvars.MutableVariables{}).ToImmutable()
		}
		return lookup(path)
	}} {
		got, err := rotatePrefab(source, true, resolve)
		if err != nil || got.Path() != source.Path() || got.Vars().ValueV("dir", "") != "4" {
			t.Fatalf("numeric fallback changed: %v %v", got, err)
		}
	}
}

func TestDirectionalRotationSelectionAndPlacementCarryPathAndIdentity(t *testing.T) {
	family, lookup := directionalFixture()
	p := dmmprefab.New(0, family+"/north", dmvars.FromParent(lookup(family+"/north")))
	instance := dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, p)
	instance.SetStableID("device-id")
	tile := dmmap.Tile{Coord: instance.Coord()}
	tile.Set(dmmap.Instances{instance})
	for _, clockwise := range []bool{true, false} {
		transform, suffix := PlacementRotateRight, "/east"
		if !clockwise {
			transform, suffix = PlacementRotateLeft, "/west"
		}
		tiles, err := IdentityOrientation().Transform(transform).Prepare(context.Background(), []dmmap.Tile{tile}, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if got := tiles[0].Instances()[0]; got.StableID() != "device-id" || got.Prefab().Path() != family+suffix {
			t.Fatal("placement lost directional path or identity")
		}
		state := model.TileState{Prefabs: []model.PrefabState{{Path: p.Path(), StableID: "device-id"}}}
		changes, err := TransformModelSelection(context.Background(), RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1),
			transform, func(string) bool { return true }, func(model.Coord) (model.TileState, bool) { return state, true }, lookup, nil)
		if err != nil || len(changes) != 1 {
			t.Fatalf("model transform: %v %v", changes, err)
		}
		got := changes[0].After.Prefabs[0]
		if got.Path != family+suffix || got.StableID != "device-id" {
			t.Fatal("model transform discarded directional path or identity")
		}
	}
}

func TestDirectionalRotationBaseTypeUsesDeclaredHelperDefaults(t *testing.T) {
	family, helpers := directionalFixture()
	basePath := "/obj/device"
	defaults := &dmvars.MutableVariables{}
	defaults.Put("dir", "1")
	defaults.Put("pixel_x", "0")
	defaults.Put("pixel_y", "0")
	parent := defaults.ToImmutable()
	lookup := func(path string) *dmvars.Variables {
		if path == basePath {
			return parent
		}
		return helpers(path)
	}
	for _, explicitOffset := range []bool{false, true} {
		vars := dmvars.FromParent(parent)
		if explicitOffset {
			vars = dmvars.Set(vars, "pixel_x", "6")
			vars = dmvars.Set(vars, "pixel_y", "5")
		}
		source := dmmprefab.New(0, basePath, vars)
		got, err := rotatePrefab(source, true, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if got.Path() != family+"/east" {
			t.Fatalf("base type did not use declared direction helper: %s", got.Path())
		}
		wantX, wantY := "27", "0"
		if explicitOffset {
			wantX, wantY = "5", "-6"
		}
		if got.Vars().ValueV("pixel_x", "") != wantX || got.Vars().ValueV("pixel_y", "") != wantY {
			t.Fatal("helper defaults or explicit offsets lost")
		}
	}
}

func TestDirectionalRotationTransformsInheritedAlternateOffsets(t *testing.T) {
	family, baseLookup := directionalFixture()
	north := dmvars.Set(baseLookup(family+"/north"), "pixel_w", "3")
	north = dmvars.Set(north, "pixel_z", "7")
	lookup := func(path string) *dmvars.Variables {
		if path == family+"/north" {
			return north
		}
		return baseLookup(path)
	}
	source := dmmprefab.New(0, family+"/north", dmvars.FromParent(north))
	got, err := rotatePrefab(source, true, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got.Vars().ValueV("pixel_w", "") != "7" || got.Vars().ValueV("pixel_z", "") != "-3" {
		t.Fatal("direction helper discarded inherited alternate offsets")
	}
}

// Opt-in coverage of evaluated project macros; no map or project files are
// changed. The ordinary synthetic tests remain independent of external data.
func TestDirectionalRotationProjectMappingHelpers(t *testing.T) {
	path := os.Getenv("APHELION_ROTATION_DME")
	if path == "" {
		t.Skip("set APHELION_ROTATION_DME to a project DME with mapping helpers")
	}
	environment, err := dmenv.New(path)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(path string) *dmvars.Variables {
		if o := environment.Objects[path]; o != nil {
			return o.Vars
		}
		return nil
	}
	for _, base := range []string{
		"/obj/machinery/power/apc/auto_name",
		"/obj/machinery/airalarm",
		"/obj/machinery/firealarm",
		"/obj/machinery/camera",
		"/obj/machinery/button/door",
	} {
		t.Run(base, func(t *testing.T) {
			path := base + "/directional/north"
			if lookup(path) == nil {
				t.Fatal("missing project mapping helper", path)
			}
			source := dmmprefab.New(0, path, dmvars.FromParent(lookup(path)))
			for _, clockwise := range []bool{false, true} {
				got, err := rotatePrefab(source, clockwise, lookup)
				if err != nil {
					t.Fatal(err)
				}
				target := base + "/directional/west"
				if clockwise {
					target = base + "/directional/east"
				}
				if got.Path() != target || got.Vars().Parent() != lookup(target) {
					t.Fatal("mapping helper or target defaults lost", got.Path())
				}
				for _, name := range []string{"dir", "pixel_x", "pixel_y"} {
					if got.Vars().ValueV(name, "") != lookup(target).ValueV(name, "") {
						t.Fatalf("wrong %s for %s", name, target)
					}
					if _, ok := got.Vars().ExplicitValue(name); ok {
						t.Fatal("redundant mapping override", name)
					}
				}
			}
		})
	}
}
