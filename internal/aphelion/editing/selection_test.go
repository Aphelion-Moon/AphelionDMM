package editing

import (
	"math/rand"
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

func TestSelectionTransformsKeepCompactSpans(t *testing.T) {
	source, err := ShapeSelection(ShapeDescriptor{Kind: ShapeEllipse, Width: 256, Height: 192}, util.Point{X: 7, Y: 11, Z: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, turned := range []Selection{source.Rotate(true), source.Rotate(false), source.Mirror(MirrorHorizontal), source.Mirror(MirrorVertical)} {
		if len(turned.points) != 0 || turned.members != nil || len(turned.runs) > 512 {
			t.Fatal("compact selection expanded to per-cell geometry")
		}
		if turned.Len() != source.Len() || turned.Level() != source.Level() {
			t.Fatal("transform lost membership")
		}
	}
}

func TestSelectionSpanTransformsMatchCellCoverageAndConsumers(t *testing.T) {
	mask, _ := MaskSelection([]util.Point{{X: 2, Y: 1, Z: 2}, {X: 1, Y: 2, Z: 2}, {X: 1, Y: 1, Z: 2}, {X: 4, Y: 6, Z: 2}})
	empty, _ := MaskSelection(nil)
	sources := []Selection{mask.Translate(util.Point{X: 4, Y: 6, Z: 1}), empty, ClipSelection(mask, 0, 0), mask.Translate(util.Point{X: -8}), mask.Translate(util.Point{Z: -3})}
	for _, shape := range []ShapeDescriptor{
		{Kind: ShapeEllipse, Width: 9, Height: 5},
		{Kind: ShapeEllipse, Width: 7, Height: 11, Outline: true, Thickness: 2},
		{Width: 3, Height: 9, Outline: true},
		{Kind: ShapeEllipse, Width: 1, Height: 8},
	} {
		s, err := ShapeSelection(shape, util.Point{X: 5, Y: 7, Z: 3})
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, s)
	}
	random := rand.New(rand.NewSource(734))
	for range 25 {
		var points []util.Point
		for x := 1; x <= 9; x++ {
			for y := 1; y <= 7; y++ {
				if random.Intn(3) == 0 {
					points = append(points, util.Point{X: x + 3, Y: y + 5, Z: 3})
				}
			}
		}
		s, _ := MaskSelection(points)
		sources = append(sources, s)
	}
	for _, s := range sources {
		original := s.Coordinates()
		for transform := 0; transform < 4; transform++ {
			points := append([]util.Point(nil), original...)
			a := s.Bounds()
			for i, p := range points {
				x, y := p.X-int(a.X1), p.Y-int(a.Y1)
				w, h := int(a.X2-a.X1)+1, int(a.Y2-a.Y1)+1
				switch transform {
				case 0:
					x, y = y, w-1-x
				case 1:
					x, y = h-1-y, x
				case 2:
					x = w - 1 - x
				case 3:
					y = h - 1 - y
				}
				points[i] = util.Point{X: int(a.X1) + x, Y: int(a.Y1) + y, Z: p.Z}
			}
			want, _ := MaskSelection(points)
			var got Selection
			switch transform {
			case 0:
				got = s.Rotate(true)
			case 1:
				got = s.Rotate(false)
			case 2:
				got = s.Mirror(MirrorHorizontal)
			case 3:
				got = s.Mirror(MirrorVertical)
			}
			if got.Bounds() != want.Bounds() || got.Level() != want.Level() || got.Len() != want.Len() ||
				!reflect.DeepEqual(got.Coordinates(), want.Coordinates()) {
				t.Fatal("transformed geometry differs from selected cells", transform, s.Bounds())
			}
			cursor := got.Cursor()
			for _, p := range want.Coordinates() {
				if next, ok := cursor.Next(); !ok || next != p || !got.Contains(p) {
					t.Fatal("cursor or membership lost canonical coordinates")
				}
				p.Z++
				if got.Contains(p) {
					t.Fatal("membership leaked across levels")
				}
			}
			if _, ok := cursor.Next(); ok {
				t.Fatal("cursor filled a mask hole")
			}
			for x := int(got.Bounds().X1) - 1; x <= int(got.Bounds().X2)+1; x++ {
				for y := int(got.Bounds().Y1) - 1; y <= int(got.Bounds().Y2)+1; y++ {
					p := util.Point{X: x, Y: y, Z: got.Level()}
					if got.Contains(p) != want.Contains(p) {
						t.Fatal("span lookup filled a hole", p)
					}
				}
			}
			clipped := ClipSelection(got, 8, 11)
			wantClip := ClipSelection(want, 8, 11)
			if !reflect.DeepEqual(clipped.Coordinates(), wantClip.Coordinates()) {
				t.Fatal("clipping changed span coverage")
			}
			combined := CombineSelection(got, mask.Translate(util.Point{X: 1, Y: 1, Z: got.Level() - 2}), SelectionSubtract)
			wantCombined := CombineSelection(want, mask.Translate(util.Point{X: 1, Y: 1, Z: want.Level() - 2}), SelectionSubtract)
			if !reflect.DeepEqual(combined.Coordinates(), wantCombined.Coordinates()) {
				t.Fatal("combining changed span coverage")
			}
		}
		if !reflect.DeepEqual(s.Coordinates(), original) {
			t.Fatal("transform mutated original selection")
		}
	}
}

func TestSelectionSpanRotationSkipsLargeEmptyExtent(t *testing.T) {
	source, _ := MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}, {X: 1, Y: 60000, Z: 1}, {X: 2, Y: 60000, Z: 1}})
	got := source.Rotate(true)
	want := []util.Point{{X: 1, Y: 1, Z: 1}, {X: 1, Y: 2, Z: 1}, {X: 60000, Y: 1, Z: 1}, {X: 60000, Y: 2, Z: 1}}
	if !reflect.DeepEqual(got.Coordinates(), want) || len(got.runs) != 2 {
		t.Fatal("rotation filled the empty extent")
	}
}
