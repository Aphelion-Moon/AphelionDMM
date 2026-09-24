package dmmap

import (
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"testing"
)

func TestStorageResolvesContentCollisions(t *testing.T) {
	s := &prefabStorage{}
	s.Free()
	a := dmmprefab.New(42, "/obj/a", &dmvars.Variables{})
	b := dmmprefab.New(42, "/obj/b", &dmvars.Variables{})
	x, y := s.Put(a), s.Put(b)
	if x == y || y.Path() != "/obj/b" || x.Id() == y.Id() {
		t.Fatal("interning aliases colliding content")
	}
	if s.Put(b) != y {
		t.Fatal("repeated collision loses interned identity")
	}
	s.Delete(b)
	if found, ok := s.GetById(x.Id()); !ok || found != x {
		t.Fatal("stale collision deleted unrelated content")
	}
	if _, ok := s.GetById(y.Id()); ok {
		t.Fatal("stale collision did not delete its content")
	}
	y = s.Put(b)
	s.Delete(x)
	if found, ok := s.GetById(y.Id()); !ok || found != y {
		t.Fatal("deletion removed colliding prefab")
	}
}
