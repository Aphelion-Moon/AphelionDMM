package mapping

import (
	"context"
	"os"
	"path/filepath"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/util"
	"testing"
)

func TestReferenceSourcePreservesIdentityAndOrderedUnknowns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reference.dmm")
	text := []byte("\"a\" = (/turf/template_noop,/area/template_noop,/obj/unknown{v = 1},/obj/unknown{v = 2})\n(1,1,1) = {\"\na\n\"}\n")
	if err := os.WriteFile(path, text, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := LoadSource(context.Background(), path, &dmenv.Dme{Objects: map[string]*dmenv.Object{}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cell := source.Cell(util.Point{X: 1, Y: 1, Z: 1})
	if len(cell) != 4 || cell[2].Vars["v"] != "1" || cell[3].Vars["v"] != "2" {
		t.Fatal("source order/unknown values changed")
	}
	cell[2].Vars["v"] = "changed"
	if source.Cell(util.Point{X: 1, Y: 1, Z: 1})[2].Vars["v"] != "1" {
		t.Fatal("source snapshot is mutable through Cell")
	}
	if err := source.CheckFresh(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(text, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := source.CheckFresh(); err == nil {
		t.Fatal("external source replacement was not detected")
	}
}

func TestReferenceComparisonKeepsChannelsAndMultiplicity(t *testing.T) {
	a := []Atom{{Path: "/turf/a"}, {Path: "/area/a"}, {Path: "/obj/a"}, {Path: "/obj/a"}}
	b := []Atom{{Path: "/turf/a"}, {Path: "/area/b"}, {Path: "/obj/a"}}
	diff := CompareCell(a, b)
	if diff.Turf || !diff.Area || !diff.Objects {
		t.Fatalf("wrong channel differences: %+v", diff)
	}
}

func TestBoundReferencePathCannotEscapeProject(t *testing.T) {
	root := t.TempDir()
	if _, err := BoundPath(root, "../elsewhere.dmm"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := BoundPath(root, filepath.Join(root, "absolute.dmm")); err == nil {
		t.Fatal("absolute binding accepted")
	}
}

func TestReferenceCoordinatesKeepParserBYONDAxes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "axes.dmm")
	if err := os.WriteFile(path, []byte("\"a\" = (/obj/top)\n\"b\" = (/obj/bottom)\n(1,1,1) = {\"\na\nb\n\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSource(context.Background(), path, &dmenv.Dme{Objects: map[string]*dmenv.Object{}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Cell(util.Point{X: 1, Y: 2, Z: 1})[0].Path != "/obj/top" {
		t.Fatal("reference inverted the already converted BYOND Y axis")
	}
}
