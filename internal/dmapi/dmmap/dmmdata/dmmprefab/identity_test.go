package dmmprefab

import (
	"sdmm/internal/dmapi/dmvars"
	"testing"
)

func TestIdentityFramesFieldsAndSortsVariables(t *testing.T) {
	a, b := &dmvars.MutableVariables{}, &dmvars.MutableVariables{}
	a.Put("a", "12")
	b.Put("a1", "2")
	if Id("/obj/example", a.ToImmutable()) == Id("/obj/example", b.ToImmutable()) {
		t.Fatal("different variable boundaries alias")
	}
	a.Put("z", "3")
	c := &dmvars.MutableVariables{}
	c.Put("z", "3")
	c.Put("a", "12")
	if Id("/obj/example", a.ToImmutable()) != Id("/obj/example", c.ToImmutable()) {
		t.Fatal("variable insertion order changes content identity")
	}
}
