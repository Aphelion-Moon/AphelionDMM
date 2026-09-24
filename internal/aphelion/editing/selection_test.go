package editing

import (
	"reflect"
	"sdmm/internal/util"
	"testing"
)

func TestSelectionOwnsSparseMembershipAndTransformsHoles(t *testing.T) {
	points := []util.Point{{X: 2, Y: 1, Z: 1}, {X: 1, Y: 2, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: 1, Y: 1, Z: 1}}
	selection, err := MaskSelection(points)
	if err != nil {
		t.Fatal(err)
	}
	points[0].X = 99
	if selection.Len() != 3 || selection.Contains(util.Point{X: 2, Y: 2, Z: 1}) || selection.Contains(util.Point{X: 1, Y: 1, Z: 2}) {
		t.Fatal("bounds or aliases became membership")
	}
	turned := selection
	for range 4 {
		turned = turned.Rotate(true)
	}
	if !reflect.DeepEqual(turned.Coordinates(), selection.Coordinates()) {
		t.Fatal("four turns lost membership")
	}
	if !reflect.DeepEqual(selection.Mirror(MirrorHorizontal).Mirror(MirrorHorizontal).Coordinates(), selection.Coordinates()) {
		t.Fatal("mirror lost membership")
	}
	out := selection.Coordinates()
	out[0].X = 99
	if selection.Coordinates()[0].X != 1 {
		t.Fatal("returned coordinates alias selection")
	}
	if allocs := testing.AllocsPerRun(100, func() {
		moved := selection.Translate(util.Point{X: 5, Y: 7})
		if !moved.Contains(util.Point{X: 6, Y: 8, Z: 1}) {
			panic("bad translated membership")
		}
		moved.VisitRuns(func(util.Bounds) {})
	}); allocs != 0 {
		t.Fatalf("pointer pose allocated %v", allocs)
	}
	if _, err := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 2}}); err == nil {
		t.Fatal("mixed levels allowed")
	}
}

func TestRectangleSelectionIsLazyAndNormalizesCorners(t *testing.T) {
	s := RectangleSelection(util.Bounds{X1: 5, Y1: 4, X2: 1, Y2: 1}, 2)
	if s.Len() != 20 || !s.Contains(util.Point{X: 3, Y: 2, Z: 2}) || s.Sparse() {
		t.Fatal("invalid rectangle")
	}
	if a := testing.AllocsPerRun(100, func() { _ = RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 10000, Y2: 10000}, 1) }); a != 0 {
		t.Fatalf("rectangle allocated %v", a)
	}
}
