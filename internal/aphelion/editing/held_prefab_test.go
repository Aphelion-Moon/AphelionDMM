package editing

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"testing"
)

func TestHeldPrefabRoundTripOwnsOriginalRawOverrides(t *testing.T) {
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "NORTHEAST")
	vars.Put("pixel_x", "+003")
	vars.Put("pixel_y", "0.0")
	source := dmmprefab.New(0, "/obj/held", vars.ToImmutable())
	held := HeldPrefab{}
	held.SetSource(source)
	for range 4 {
		if err := held.Rotate(true); err != nil {
			t.Fatal(err)
		}
	}
	if held.Value() != source {
		t.Fatal("four turns did not restore exact original representation")
	}
	if err := held.Rotate(false); err != nil {
		t.Fatal(err)
	}
	if err := held.Rotate(true); err != nil {
		t.Fatal(err)
	}
	if held.Value() != source || source.Vars().ValueV("pixel_x", "") != "+003" {
		t.Fatal("opposite turns mutated source")
	}
	bad := dmmprefab.New(0, source.Path(), dmvars.Set(source.Vars(), "dir", "unsupported()"))
	held.SetSource(bad)
	if held.Rotate(true) == nil || held.Value() != bad {
		t.Fatal("unsupported expression replaced good held value")
	}
}
