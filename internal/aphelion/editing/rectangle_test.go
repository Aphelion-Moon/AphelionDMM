package editing

import (
	"context"
	"errors"
	"sdmm/internal/util"
	"testing"
)

func TestRectanglePerimeterVisitsOnlyBoundaryOnce(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {1, 300}, {300, 1}, {300, 200}} {
		seen := make(map[util.Point]bool)
		err := VisitRectangle(context.Background(), util.Bounds{X1: 1, Y1: 1, X2: float32(size[0]), Y2: float32(size[1])}, 1, true, func(p util.Point) error {
			if seen[p] || p.Z != 1 || p.X != 1 && p.X != size[0] && p.Y != 1 && p.Y != size[1] {
				t.Fatal("invalid perimeter point", p)
			}
			seen[p] = true
			return nil
		})
		want := size[0] * size[1]
		if size[0] > 1 && size[1] > 1 {
			want = 2*size[0] + 2*size[1] - 4
		}
		if err != nil || len(seen) != want {
			t.Fatal(size, len(seen), want, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VisitRectangle(ctx, util.Bounds{X1: 1, Y1: 1, X2: 300, Y2: 300}, 1, false, func(util.Point) error { t.Fatal("cancelled enumeration continued"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
