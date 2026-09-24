package dmmdata

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"testing"
)

func TestPrefabsEqualityResolvesIDCollisions(t *testing.T) {
	a := Prefabs{dmmprefab.New(42, "/obj/one", &dmvars.Variables{})}
	b := Prefabs{dmmprefab.New(42, "/obj/two", &dmvars.Variables{})}
	if a.Equals(b) {
		t.Fatal("ID collision aliases distinct contents")
	}
}

func TestPrefabListHashUsesContentRatherThanLocalIDs(t *testing.T) {
	a := Prefabs{dmmprefab.New(42, "/obj", &dmvars.Variables{})}
	b := Prefabs{dmmprefab.New(43, "/obj", &dmvars.Variables{})}
	if !a.Equals(b) || a.Hash() != b.Hash() {
		t.Fatal("equal content has different hashes after interning")
	}
}
