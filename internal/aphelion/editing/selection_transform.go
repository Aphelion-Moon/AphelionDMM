package editing

import (
	"cmp"
	"slices"
	"sort"

	"sdmm/internal/util"
)

// A quarter turn makes vertical spans horizontal. Sweep only their boundaries
// to recover canonical vertical spans, without creating one point/map entry per
// selected cell or visiting the empty space between disconnected components.
func rotateSelectionSpans(source Selection, clockwise bool) Selection {
	type event struct{ x, y, delta int }
	events := make([]event, 0, len(source.runs)*2)
	area := source.Bounds()
	x1, y1 := int(area.X1), int(area.Y1)
	width, height := int(area.X2-area.X1)+1, int(area.Y2-area.Y1)+1
	source.VisitRuns(func(run util.Bounds) {
		left, right, row := x1+int(run.Y1)-y1, x1+int(run.Y2)-y1, y1+width-1-(int(run.X1)-x1)
		if !clockwise {
			left, right, row = x1+height-1-(int(run.Y2)-y1), x1+height-1-(int(run.Y1)-y1), y1+int(run.X1)-x1
		}
		events = append(events, event{left, row, 1}, event{right + 1, row, -1})
	})
	slices.SortFunc(events, func(a, b event) int {
		if order := cmp.Compare(a.x, b.x); order != 0 {
			return order
		}
		return cmp.Compare(a.y, b.y)
	})
	active := make([]int, 0, min(len(source.runs), width))
	var runs []util.Bounds
	for i := 0; i < len(events); {
		x := events[i].x
		for i < len(events) && events[i].x == x {
			y, delta := events[i].y, 0
			for i < len(events) && events[i].x == x && events[i].y == y {
				delta += events[i].delta
				i++
			}
			at := sort.SearchInts(active, y)
			if delta > 0 && (at == len(active) || active[at] != y) {
				active = append(active, 0)
				copy(active[at+1:], active[at:])
				active[at] = y
			} else if delta < 0 && at < len(active) && active[at] == y {
				copy(active[at:], active[at+1:])
				active = active[:len(active)-1]
			}
		}
		if len(active) == 0 || i == len(events) {
			continue
		}
		// Membership stays constant until the next event. Adjacent active rows
		// are one output span, even when the input used many separate columns.
		for column := x; column < events[i].x; column++ {
			for row := 0; row < len(active); {
				low, high := active[row], active[row]
				row++
				for row < len(active) && active[row] == high+1 {
					high = active[row]
					row++
				}
				runs = append(runs, util.Bounds{X1: float32(column), X2: float32(column), Y1: float32(low), Y2: float32(high)})
			}
		}
	}
	return selectionFromSortedSpans(runs, source.Level())
}

func mirrorSelectionSpans(source Selection, axis MirrorAxis) Selection {
	area := source.Bounds()
	runs := make([]util.Bounds, 0, len(source.runs))
	source.VisitRuns(func(run util.Bounds) {
		switch axis {
		case MirrorHorizontal:
			run.X1, run.X2 = area.X1+area.X2-run.X2, area.X1+area.X2-run.X1
		case MirrorVertical:
			run.Y1, run.Y2 = area.Y1+area.Y2-run.Y2, area.Y1+area.Y2-run.Y1
		}
		runs = append(runs, run)
	})
	slices.SortFunc(runs, func(a, b util.Bounds) int {
		if order := cmp.Compare(a.X1, b.X1); order != 0 {
			return order
		}
		return cmp.Compare(a.Y1, b.Y1)
	})
	return selectionFromSortedSpans(runs, source.Level())
}

// Match MaskSelection's tight bounds and invalid/empty coordinate handling.
// Callers own the fresh canonical vertical spans supplied here.
func selectionFromSortedSpans(runs []util.Bounds, level int) Selection {
	out := Selection{runBased: true}
	for _, run := range runs {
		if run.X1 < 1 || run.Y1 < 1 || level < 1 {
			return Selection{}
		}
		if out.runCount == 0 {
			out.area = run
		} else {
			out.area.X2 = run.X2
			out.area.Y1 = min(out.area.Y1, run.Y1)
			out.area.Y2 = max(out.area.Y2, run.Y2)
		}
		out.runCount += int(run.Y2-run.Y1) + 1
	}
	if out.runCount > 0 {
		out.runs, out.z = runs, level
	}
	return out
}
