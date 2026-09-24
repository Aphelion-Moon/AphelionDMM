package editing

import (
	"context"
	"sdmm/internal/util"
)

// VisitRectangle preserves x/y iteration order while visiting a perimeter in
// O(width+height). Degenerate rows/columns never duplicate a corner.
func VisitRectangle(ctx context.Context, bounds util.Bounds, z int, border bool, visit func(util.Point) error) error {
	x1, y1, x2, y2 := int(bounds.X1), int(bounds.Y1), int(bounds.X2), int(bounds.Y2)
	for x := x1; x <= x2; x++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if border && x > x1 && x < x2 {
			if err := visit(util.Point{X: x, Y: y1, Z: z}); err != nil {
				return err
			}
			if y2 > y1 {
				if err := visit(util.Point{X: x, Y: y2, Z: z}); err != nil {
					return err
				}
			}
			continue
		}
		for y := y1; y <= y2; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(util.Point{X: x, Y: y, Z: z}); err != nil {
				return err
			}
		}
	}
	return nil
}
