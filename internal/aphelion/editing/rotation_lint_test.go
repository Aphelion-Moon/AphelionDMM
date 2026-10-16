package editing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

// pipeFixture models Meridian-Rift's types: disposal pipes declare
// dir = NONE (BYOND shows and runs them facing SOUTH), cables inherit /atom's
// dir and work out their connections in game, and windoors face by dir.
func pipeFixture(none string) PrefabLookup {
	values := map[string]*dmvars.Variables{}
	child := func(path, parent string, pairs ...string) {
		v := &dmvars.MutableVariables{}
		for i := 0; i < len(pairs); i += 2 {
			v.Put(pairs[i], pairs[i+1])
		}
		immutable := v.ToImmutable()
		if parent != "" {
			immutable.LinkParent(values[parent])
		}
		values[path] = immutable
	}
	child("/atom", "", "dir", "2", "pixel_x", "0", "pixel_y", "0")
	child("/obj", "/atom")
	child("/obj/structure", "/obj")
	child("/obj/structure/disposalpipe", "/obj/structure", "dir", none, "icon", `'icons/obj/pipes_n_cables/disposal.dmi'`)
	child("/obj/structure/disposalpipe/segment", "/obj/structure/disposalpipe", "icon_state", `"pipe"`)
	child("/obj/structure/disposalpipe/trunk", "/obj/structure/disposalpipe", "icon_state", `"pipe-t"`)
	child("/obj/structure/cable", "/obj/structure", "icon", `'icons/obj/pipes_n_cables/layer_cable.dmi'`, "icon_state", `"l2-1-2-4-8-node"`)
	child("/obj/machinery", "/obj")
	child("/obj/machinery/door", "/obj/machinery")
	child("/obj/machinery/door/window", "/obj/machinery/door", "icon_state", `"left"`)
	return func(path string) *dmvars.Variables { return values[path] }
}

func pipeSprites(t *testing.T) {
	t.Helper()
	sprites := map[string]int{"pipe": 4, "pipe-t": 4, "l2-1-2-4-8-node": 4, "left": 4}
	SetSpriteDirections(func(icon, state string) (int, bool) { d, ok := sprites[state]; return d, ok })
	t.Cleanup(func() { SetSpriteDirections(nil) })
}

func plain(lookup PrefabLookup, path string, pairs ...string) *dmmprefab.Prefab {
	vars := dmvars.FromParent(lookup(path))
	for i := 0; i < len(pairs); i += 2 {
		vars = dmvars.Set(vars, pairs[i], pairs[i+1])
	}
	return dmmprefab.New(0, path, vars)
}

// An unedited disposal pipe faces SOUTH in game even though its type says
// dir = NONE. Rotation must turn it with the pipes around it rather than
// leave it pointing the old way, and four turns must restore the original.
func TestUneditedDisposalPipesTurnFromSouth(t *testing.T) {
	pipeSprites(t)
	for _, none := range []string{"0", "NONE"} {
		lookup := pipeFixture(none)
		for _, path := range []string{"/obj/structure/disposalpipe/segment", "/obj/structure/disposalpipe/trunk"} {
			source := plain(lookup, path)
			for clockwise, want := range map[bool]string{true: "8", false: "4"} {
				got, err := rotatePrefab(source, clockwise, lookup)
				if err != nil {
					t.Fatalf("%s (%s): %v", path, none, err)
				}
				if dir, ok := got.Vars().ExplicitValue("dir"); !ok || dir != want {
					t.Fatalf("%s (dir = %s) clockwise=%v: dir %q explicit=%v, want %s", path, none, clockwise, dir, ok, want)
				}
				for range 3 {
					if got, err = rotatePrefab(got, clockwise, lookup); err != nil {
						t.Fatal(err)
					}
				}
				assertSameRepresentation(t, got, source)
			}
			mirrored, err := mirrorPrefab(source, MirrorVertical, lookup)
			if err != nil || mirrored.Vars().ValueV("dir", "") != "1" {
				t.Fatalf("%s vertical mirror: dir %q (%v)", path, mirrored.Vars().ValueV("dir", ""), err)
			}
			if mirrored, err := mirrorPrefab(source, MirrorHorizontal, lookup); err != nil || mirrored != source {
				t.Fatalf("%s horizontal mirror changed a south-facing pipe (%v)", path, err)
			}
		}
		// A pipe turned to SOUTH returns to the unedited form, as mapped.
		east := plain(lookup, "/obj/structure/disposalpipe/segment", "dir", "4")
		if got, err := rotatePrefab(east, true, lookup); err != nil {
			t.Fatal(err)
		} else if dir, explicit := got.Vars().ExplicitValue("dir"); explicit {
			t.Fatalf("south-facing segment kept dir = %s", dir)
		}
	}
}

func meridianRules(t *testing.T) *maplint.RuleSet {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"cable_varedits.yml":    "/obj/structure/cable:\n  banned_variables: true\n",
		"windoor_var_edits.yml": "help: \"Use the directional variants when possible.\"\n/obj/machinery/door/window:\n  banned_variables:\n    dir:\n      deny: [1, 2, 4, 8]\n",
		// Not a Meridian rule: a ban on a variable rotation never writes.
		"pipe_names.yml": "/obj/structure/disposalpipe:\n  banned_variables:\n    - name\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rs, errs := maplint.Load(dir)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	return rs
}

func useRules(t *testing.T, rs *maplint.RuleSet) {
	t.Helper()
	SetOrientationRules(func() *maplint.RuleSet { return rs })
	t.Cleanup(func() { SetOrientationRules(nil) })
}

func oneRow(prefabs ...*dmmprefab.Prefab) *dmmap.Dmm {
	m := &dmmap.Dmm{MaxX: len(prefabs), MaxY: len(prefabs), MaxZ: 1}
	for y := 1; y <= len(prefabs); y++ {
		for x := 1; x <= len(prefabs); x++ {
			tile := &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}}
			if y == 1 {
				tile.InstancesAdd(prefabs[x-1])
			}
			m.Tiles = append(m.Tiles, tile)
		}
	}
	return m
}

// Meridian-Rift's cable_varedits.yml bans every cable edit: cables find their
// connections in game, so a turn must move a cable without writing a dir.
func TestRotationNeverWritesEditsTheRepositoryBans(t *testing.T) {
	pipeSprites(t)
	lookup := pipeFixture("0")
	cable := plain(lookup, "/obj/structure/cable")
	pipe := plain(lookup, "/obj/structure/disposalpipe/segment", "dir", "4")
	all := func(string) bool { return true }

	// Without repository rules the multi-direction cable sprite turns.
	if got, _ := rotatePrefab(cable, true, lookup); got == cable {
		t.Fatal("fixture: cable should turn when no rules are loaded")
	}
	useRules(t, meridianRules(t))
	for _, clockwise := range []bool{true, false} {
		plan, err := Rotate(oneRow(cable, pipe), util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 1}, 1, clockwise, all, lookup)
		if err != nil {
			t.Fatal(err)
		}
		cables, pipes := 0, 0
		for _, tile := range plan.Tiles {
			for _, i := range tile.Instances() {
				switch i.Prefab().Path() {
				case "/obj/structure/cable":
					cables++
					if i.Prefab() != cable {
						t.Fatalf("rotated cable gained edits: %v", maplint.AtomFromPrefab(i.Prefab()).Vars)
					}
				case "/obj/structure/disposalpipe/segment":
					pipes++
					if i.Prefab().Vars().ValueV("dir", "") == "4" {
						t.Fatal("disposal pipe did not turn")
					}
				}
			}
		}
		if cables != 1 || pipes != 1 {
			t.Fatalf("rotation lost atoms: %d cables, %d pipes", cables, pipes)
		}
	}
	mirrored, err := Mirror(oneRow(cable), util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1, MirrorVertical, all, lookup)
	if err != nil || mirrored.Tiles[0].Instances()[0].Prefab() != cable {
		t.Fatalf("mirrored cable gained edits (%v)", err)
	}
	var held HeldPrefab
	held.SetSource(cable)
	if err := held.Rotate(true, lookup); err != nil || held.Value() != cable {
		t.Fatalf("held cable gained edits (%v)", err)
	}
	// A cable the map already gave a dir is left for the lint fixer, not
	// rewritten into another banned value.
	edited := plain(lookup, "/obj/structure/cable", "dir", "4")
	if got, err := rotatePrefab(edited, false, lookup); err != nil || got != edited {
		t.Fatalf("existing cable dir was rewritten (%v)", err)
	}
	// Turning onto the inherited SOUTH drops the banned edit, which is legal.
	if got, err := rotatePrefab(edited, true, lookup); err != nil || len(maplint.AtomFromPrefab(got).Vars) != 0 {
		t.Fatalf("cable turned to its default kept edits (%v)", err)
	}
	// A ban on a variable the turn does not write never stops the turn.
	named := plain(lookup, "/obj/structure/disposalpipe/segment", "dir", "4", "name", `"chute"`)
	if got, err := rotatePrefab(named, true, lookup); err != nil || got == named {
		t.Fatalf("unrelated banned variable blocked the turn (%v)", err)
	}
}

// A rule that bans some values of a meaningful variable (windoor dir must use
// the directional helpers) cannot be satisfied by leaving the atom unturned,
// so rotation refuses with the rule's own help instead of writing the value.
func TestRotationRefusesValuesTheRepositoryBans(t *testing.T) {
	pipeSprites(t)
	lookup := pipeFixture("0")
	useRules(t, meridianRules(t))
	windoor := plain(lookup, "/obj/machinery/door/window")
	_, err := Rotate(oneRow(windoor), util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1, true, func(string) bool { return true }, lookup)
	if err == nil || !strings.Contains(err.Error(), "windoor_var_edits.yml") || !strings.Contains(err.Error(), "directional variants") {
		t.Fatalf("windoor rotation: %v", err)
	}
	// An atom that already breaks the rule is not made worse by turning.
	if _, err := rotatePrefab(plain(lookup, "/obj/machinery/door/window", "dir", "8"), true, lookup); err != nil {
		t.Fatalf("existing violation blocked rotation: %v", err)
	}
}
