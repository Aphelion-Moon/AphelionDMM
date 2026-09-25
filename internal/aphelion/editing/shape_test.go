package editing

import (
	"sdmm/internal/util"
	"testing"
)

func TestShapeCellCoverageAndOutline(t *testing.T) {
	for _, tc := range []struct{ diameter, count int }{{1, 1}, {2, 4}, {3, 9}, {4, 12}, {5, 21}} {
		s, err := ShapeSelection(ShapeDescriptor{Kind: ShapeCircle, Width: tc.diameter, Height: tc.diameter}, util.Point{X: 1, Y: 1, Z: 1})
		if err != nil || s.Len() != tc.count {
			t.Fatalf("diameter %d: %d cells, %v", tc.diameter, s.Len(), err)
		}
	}
	s, err := ShapeSelection(ShapeDescriptor{Kind: ShapeCircle, Width: 5, Height: 5, Outline: true, Thickness: 1}, util.Point{X: 1, Y: 1, Z: 1})
	if err != nil || s.Contains(util.Point{X: 3, Y: 3, Z: 1}) || !s.Contains(util.Point{X: 3, Y: 1, Z: 1}) {
		t.Fatal("outline lost perimeter or filled its hole")
	}
}

func TestShapeRestrictionAndSweep(t *testing.T) {
	shape, _ := ShapeSelection(ShapeDescriptor{Kind: ShapeCircle, Width: 1, Height: 1}, util.Point{X: 1, Y: 1, Z: 1})
	stroke := NewShapeStroke(shape, 4, 4, Selection{}, false)
	stroke.Sample(util.Point{X: 1, Y: 1, Z: 1})
	stroke.Sample(util.Point{X: 4, Y: 1, Z: 1})
	if stroke.Selection().Len() != 4 {
		t.Fatal("fast stroke left gaps")
	}
	mask, _ := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	stroke = NewShapeStroke(shape, 4, 4, mask, true)
	stroke.Sample(util.Point{X: 1, Y: 1, Z: 1})
	stroke.Sample(util.Point{X: 4, Y: 1, Z: 1})
	if stroke.Selection().Len() != 2 || stroke.Selection().Contains(util.Point{X: 2, Y: 1, Z: 1}) {
		t.Fatal("stroke filled restricted hole")
	}
}

func TestQueuedShapeSweepMatchesImmediateCoverage(t *testing.T) {
	shape, _ := ShapeSelection(ShapeDescriptor{Kind: ShapeCircle, Width: 5, Height: 5}, util.Point{X: 1, Y: 1, Z: 1})
	immediate := NewShapeStroke(shape, 256, 64, Selection{}, false)
	queued := NewShapeStroke(shape, 256, 64, Selection{}, false)
	for _, point := range []util.Point{{X: 1, Y: 1, Z: 1}, {X: 200, Y: 40, Z: 1}, {X: 13, Y: 3, Z: 1}} {
		immediate.Sample(point)
		queued.Queue(point)
	}
	if queued.Advance() {
		t.Fatal("long stroke ignored per-frame work limit")
	}
	for n := 0; n < 10000; n++ {
		if queued.Advance() {
			break
		}
	}
	want, got := immediate.Selection(), queued.Selection()
	if want.Len() != got.Len() {
		t.Fatalf("queued stroke lost cells: %d != %d", got.Len(), want.Len())
	}
	want.Visit(func(p util.Point) {
		if !got.Contains(p) {
			t.Fatal("queued sweep has a gap", p)
		}
	})
}
