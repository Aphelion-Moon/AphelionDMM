package editing

import (
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// Beds and bedsheets draw four directions with only two looks: SOUTH matches
// WEST and NORTH matches EAST. Rotation keeps them on EAST/WEST and flips the
// look on every turn instead of writing look-alike NORTH/SOUTH edits.
func TestHorizontalOnlySpriteFlipsBetweenEastAndWest(t *testing.T) {
	lookup := scopeFixture()
	SetSpriteDirections(func(icon, state string) (int, bool) { return 4, true })
	SetHorizontalTwins(func(icon, state string) (int, int, bool) {
		return 4, 8, state == "sheet"
	})
	t.Cleanup(func() { SetSpriteDirections(nil); SetHorizontalTwins(nil) })
	sheet := func(dir string) *dmmprefab.Prefab {
		vars := dmvars.Set(dmvars.FromParent(lookup("/obj")), "icon_state", `"sheet"`)
		if dir != "" {
			vars = dmvars.Set(vars, "dir", dir)
		}
		return dmmprefab.New(0, "/obj/item/bedsheet", vars)
	}
	for _, tc := range []struct {
		from      string
		clockwise bool
		want      string
	}{
		{"", true, "4"}, {"", false, "4"}, // Inherited SOUTH looks like WEST.
		{"2", true, "4"}, {"1", true, "8"}, {"1", false, "8"},
		{"4", true, "8"}, {"4", false, "8"}, {"8", true, "4"}, {"8", false, "4"},
	} {
		got, err := rotatePrefab(sheet(tc.from), tc.clockwise, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if dir := got.Vars().ValueV("dir", ""); dir != tc.want {
			t.Fatalf("dir %q clockwise=%v: got %q want %q", tc.from, tc.clockwise, dir, tc.want)
		}
	}

	var held HeldPrefab
	east := sheet("4")
	held.SetSource(east)
	for turn, want := range []string{"8", "4", "8", "4"} {
		if err := held.Rotate(true, lookup); err != nil {
			t.Fatal(err)
		}
		if dir := held.Value().Vars().ValueV("dir", ""); dir != want {
			t.Fatalf("held turn %d: got %q want %q", turn+1, dir, want)
		}
	}
	assertSameRepresentation(t, held.Value(), east)

	// Other sprites keep exact dir arithmetic.
	chair := dmmprefab.New(0, "/obj/chair", dmvars.Set(dmvars.FromParent(lookup("/obj/chair")), "icon_state", `"chair"`))
	if got, _ := rotatePrefab(chair, true, lookup); got.Vars().ValueV("dir", "") != "4" {
		t.Fatalf("four-look chair: got dir %q want 4", got.Vars().ValueV("dir", ""))
	}
	// Stasis beds and pods only use dir for the same headrest side the sprite
	// shows, so machinery flips too; mob facing and pipe sides stay exact.
	stasis := dmmprefab.New(0, "/obj/machinery/stasis", dmvars.Set(dmvars.FromParent(lookup("/obj/chair")), "icon_state", `"sheet"`))
	if got, _ := rotatePrefab(stasis, true, lookup); got.Vars().ValueV("dir", "") != "8" {
		t.Fatalf("stasis bed: got dir %q want NORTH twin EAST flipped to WEST", got.Vars().ValueV("dir", ""))
	}
	for _, path := range []string{"/mob/living/carp", "/obj/machinery/atmospherics/pipe"} {
		exact := dmmprefab.New(0, path, dmvars.Set(dmvars.FromParent(lookup("/obj/chair")), "icon_state", `"sheet"`))
		if got, _ := rotatePrefab(exact, true, lookup); got.Vars().ValueV("dir", "") != "4" {
			t.Fatalf("%s: got dir %q want exact NORTH->EAST", path, got.Vars().ValueV("dir", ""))
		}
	}
}
