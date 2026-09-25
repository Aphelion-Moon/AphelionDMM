package editing

import "sdmm/internal/util"

// ClipSelection clips render/brush geometry by spans without expanding a large
// filled ellipse into one stored coordinate per tile.
func ClipSelection(selection Selection, maxX, maxY int) Selection {
	if selection.Len() == 0 {
		return selection
	}
	a := selection.Bounds()
	if a.X1 >= 1 && a.Y1 >= 1 && a.X2 <= float32(maxX) && a.Y2 <= float32(maxY) {
		return selection
	}
	result := Selection{z: selection.Level(), runBased: true}
	selection.VisitRuns(func(r util.Bounds) {
		r.X1 = max(1, r.X1)
		r.Y1 = max(1, r.Y1)
		r.X2 = min(float32(maxX), r.X2)
		r.Y2 = min(float32(maxY), r.Y2)
		if r.X1 > r.X2 || r.Y1 > r.Y2 {
			return
		}
		for x := r.X1; x <= r.X2; x++ {
			run := util.Bounds{X1: x, X2: x, Y1: r.Y1, Y2: r.Y2}
			result.runs = append(result.runs, run)
			result.runCount += int(r.Y2-r.Y1) + 1
			if result.runCount == int(r.Y2-r.Y1)+1 {
				result.area = run
			} else {
				result.area.X1 = min(result.area.X1, x)
				result.area.X2 = max(result.area.X2, x)
				result.area.Y1 = min(result.area.Y1, r.Y1)
				result.area.Y2 = max(result.area.Y2, r.Y2)
			}
		}
	})
	return result
}
