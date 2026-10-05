package dmmap

import (
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
	"testing"
)

func TestInitialRefusesUnknownType(t *testing.T) {
	PrefabStorage.Free()
	t.Cleanup(PrefabStorage.Free)
	t.Cleanup(Free)
	known := &dmvars.MutableVariables{}
	known.Put("dir", "2")
	for _, loaded := range []*dmenv.Dme{nil, {Objects: map[string]*dmenv.Object{"/obj/known": {Path: "/obj/known", Vars: known.ToImmutable()}}}} {
		environment = loaded
		func() {
			defer func() {
				if failure := recover(); failure != nil {
					t.Fatalf("unknown initial prefab panicked: %v", failure)
				}
			}()
			if prefab, ok := PrefabStorage.InitialV("/obj/unknown"); ok || prefab != nil {
				t.Fatal("unknown type produced an initial prefab")
			}
			if PrefabStorage.Initial("/obj/unknown") != nil || IsKnownType("/obj/unknown") {
				t.Fatal("unknown type reported environment defaults")
			}
		}()
		if len(PrefabStorage.GetAllByPath("/obj/unknown")) != 0 {
			t.Fatal("refused initial prefab was persisted")
		}
	}
	prefab, ok := PrefabStorage.InitialV("/obj/known")
	if !ok || prefab == nil || prefab.Path() != "/obj/known" || prefab.Vars().Len() != 0 || prefab.Vars().ValueV("dir", "") != "2" {
		t.Fatal("known type did not produce its initial prefab")
	}
	if PrefabStorage.Initial("/obj/known") != prefab || !IsKnownType("/obj/known") {
		t.Fatal("Initial diverged from InitialV for a known type")
	}
}
