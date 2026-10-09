package maplint

import (
	"strings"
	"testing"
)

func TestPlacementReportMessage(t *testing.T) {
	if !(PlacementReport{}).Empty() || (PlacementReport{}).Message() != "" {
		t.Fatal("zero report is not empty")
	}
	r := PlacementReport{X: 3, Y: 4, Z: 1, Summary: "multiple_tables.yml: One table.", Warned: 3, Skipped: 2, SkippedPath: "/obj/structure/window"}
	m := r.Message()
	for _, want := range []string{"3,4,1", "One table.", "+2 more tiles", "Skipped 2 tiles", "identical /obj/structure/window"} {
		if !strings.Contains(m, want) {
			t.Errorf("message %q lacks %q", m, want)
		}
	}
	one := PlacementReport{Skipped: 1}.Message()
	if !strings.Contains(one, "Skipped 1 tile that") {
		t.Fatalf("singular: %q", one)
	}
}

func TestNewViolationsIgnoresPreexistingAndReordering(t *testing.T) {
	rs := mustLoad(t, map[string]string{"w.yml": windowRules, "t.yml": tableRules})
	w := atom("/obj/structure/window", "dir", "4")
	before := []Atom{w, w, atom("/obj/item/pen")}
	if got := rs.NewViolations("m.dmm", before, before); len(got) != 0 {
		t.Fatalf("preexisting violations reported: %q", messages(got))
	}
	after := []Atom{atom("/obj/structure/table"), w, atom("/obj/item/pen"), w, atom("/obj/structure/table/wood")}
	got := rs.NewViolations("m.dmm", before, after)
	if len(got) == 0 {
		t.Fatal("new table conflict not reported")
	}
	for _, v := range got {
		if v.RuleFile != "t.yml" {
			t.Fatalf("reported %+v", v)
		}
	}
}

func TestSameAtom(t *testing.T) {
	if !SameAtom(atom("/a", "x", "1"), atom("/a", "x", "1")) || SameAtom(atom("/a", "x", "1"), atom("/a", "x", "2")) ||
		SameAtom(atom("/a"), atom("/a", "x", "1")) || SameAtom(atom("/a"), atom("/b")) {
		t.Fatal("SameAtom")
	}
}
