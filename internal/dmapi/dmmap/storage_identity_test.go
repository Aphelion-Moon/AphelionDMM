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

func TestInternedContentOwnsVariablesAndWarmPutAllocatesNothing(t *testing.T) {
	s := &prefabStorage{}
	s.Free()
	values := &dmvars.MutableVariables{}
	values.Put("custom", `"original"`)
	input := values.ToImmutable()
	p := s.Put(dmmprefab.New(dmmprefab.IdNone, "/obj/test", input))
	id := p.Id()
	key := p.ContentKey()
	parent := &dmvars.MutableVariables{}
	parent.Put("dir", "4")
	p.Vars().LinkParent(parent.ToImmutable())
	if p.ContentKey() != key || p.Id() != id || p.Vars().IntV("dir", 0) != 4 {
		t.Fatal("parent appearance changed explicit identity")
	}
	input.Iterate()[0] = "corrupted input"
	names := p.Vars().Iterate()
	names[0] = "corrupted output"
	wrapped := &dmvars.MutableVariables{Variables: *p.Vars()}
	wrapped.Put("custom", `"changed"`)
	if got, _ := p.Vars().Value("custom"); got != `"original"` || p.Vars().Iterate()[0] != "custom" || p.Id() != id {
		t.Fatal("interned contents changed through an alias", got)
	}
	if got := testing.AllocsPerRun(100, func() {
		if s.Put(p) != p {
			panic("canonical reference changed")
		}
	}); got != 0 {
		t.Fatalf("warm Put allocated %g times", got)
	}
	modified := s.Put(dmmprefab.New(dmmprefab.IdNone, p.Path(), dmvars.Set(p.Vars(), "custom", `"new"`)))
	if modified == p || modified.Id() == p.Id() {
		t.Fatal("copy-on-write edit reused old identity")
	}
	s.Delete(p)
	if _, ok := s.GetById(modified.Id()); !ok {
		t.Fatal("deleting canonical content removed a different value")
	}
}
