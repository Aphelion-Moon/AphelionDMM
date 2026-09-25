package mapping

import (
	"context"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
	"testing"
)

func TestOccurrenceLayerKeepsDescendantsAndExcludesOtherUses(t *testing.T) {
	point := util.Point{X: 1, Y: 1, Z: 1}
	s := &Source{Size: point, grid: map[util.Point]dmmdata.Key{point: "a"}, dictionary: map[dmmdata.Key][]Atom{"a": {{Path: "/obj/base"}, {Path: "/obj/selected", Occurrence: "a"}, {Path: "/obj/child", Occurrence: "b"}, {Path: "/obj/other", Occurrence: "c"}}}}
	p := &Projection{Source: s, Roots: []Root{{ID: "a"}, {ID: "b", Parent: "a"}, {ID: "c"}}}
	layer, err := p.OccurrenceLayer(context.Background(), "a", true)
	if err != nil {
		t.Fatal(err)
	}
	defer layer.Close()
	cell := layer.Cell(point)
	if len(cell) != 2 || cell[0].Path != "/obj/selected" || cell[1].Path != "/obj/child" {
		t.Fatal("preview included unrelated contributions", cell)
	}
	backdrop, err := p.OccurrenceLayer(context.Background(), "a", false)
	if err != nil {
		t.Fatal(err)
	}
	defer backdrop.Close()
	cell = backdrop.Cell(point)
	if len(cell) != 2 || cell[0].Path != "/obj/base" || cell[1].Path != "/obj/other" {
		t.Fatal("backdrop duplicated selected occurrence", cell)
	}
}
