package editing

import (
	"sdmm/internal/util"
	"sort"
)

type SelectionOperation uint8

const (
	SelectionReplace SelectionOperation = iota
	SelectionAdd
	SelectionSubtract
	SelectionIntersect
)

// WorkingSelection belongs to one document, not the currently active tool.
// Geometry is immutable and deliberately independent of visibility and undo.
type WorkingSelection struct {
	levels    map[int]Selection
	histories map[int]*SelectionHistory
	Restrict  bool
}

func (s *WorkingSelection) Get(z int) Selection { return s.levels[z] }

func (s *WorkingSelection) Set(selection Selection) {
	if selection.Level() == 0 {
		return
	}
	if s.levels == nil {
		s.levels = make(map[int]Selection)
	}
	s.levels[selection.Level()] = selection
}
func (s *WorkingSelection) Clear(z int)                     { delete(s.levels, z); delete(s.histories, z) }
func (s *WorkingSelection) History(z int) *SelectionHistory { return s.histories[z] }
func (s *WorkingSelection) SetHistory(z int, history *SelectionHistory) {
	if s.histories == nil {
		s.histories = make(map[int]*SelectionHistory)
	}
	s.histories[z] = history
}

// CombineSelection uses actual membership; bounding rectangles never fill gaps.
func CombineSelection(a, b Selection, operation SelectionOperation) Selection {
	if operation == SelectionReplace {
		return b
	}
	if a.Len() == 0 {
		if operation == SelectionAdd {
			return b
		}
		return a
	}
	if b.Len() == 0 {
		if operation == SelectionIntersect {
			return b
		}
		return a
	}
	if a.Level() != b.Level() {
		return a
	}
	// Sweep interval boundaries rather than materializing every selected tile.
	type boundary struct{ y, a, b int }
	columns := map[int][]boundary{}
	add := func(s Selection, first bool) {
		s.VisitRuns(func(r util.Bounds) {
			for x := int(r.X1); x <= int(r.X2); x++ {
				start, end := boundary{y: int(r.Y1)}, boundary{y: int(r.Y2) + 1}
				if first {
					start.a, end.a = 1, -1
				} else {
					start.b, end.b = 1, -1
				}
				columns[x] = append(columns[x], start, end)
			}
		})
	}
	add(a, true)
	add(b, false)
	xs := make([]int, 0, len(columns))
	for x := range columns {
		xs = append(xs, x)
	}
	sort.Ints(xs)
	result := Selection{z: a.Level(), runBased: true}
	for _, x := range xs {
		events := columns[x]
		sort.Slice(events, func(i, j int) bool { return events[i].y < events[j].y })
		ac, bc := 0, 0
		for i := 0; i < len(events); {
			y := events[i].y
			for i < len(events) && events[i].y == y {
				ac += events[i].a
				bc += events[i].b
				i++
			}
			inside := operation == SelectionAdd && (ac > 0 || bc > 0) || operation == SelectionSubtract && ac > 0 && bc == 0 || operation == SelectionIntersect && ac > 0 && bc > 0
			if !inside || i == len(events) {
				continue
			}
			run := util.Bounds{X1: float32(x), X2: float32(x), Y1: float32(y), Y2: float32(events[i].y - 1)}
			if result.runCount == 0 {
				result.area = run
			} else {
				result.area.X2 = run.X2
				result.area.Y1 = min(result.area.Y1, run.Y1)
				result.area.Y2 = max(result.area.Y2, run.Y2)
			}
			result.runCount += int(run.Y2-run.Y1) + 1
			n := len(result.runs)
			if n > 0 && result.runs[n-1].X1 == run.X1 && result.runs[n-1].Y2+1 == run.Y1 {
				result.runs[n-1].Y2 = run.Y2
			} else {
				result.runs = append(result.runs, run)
			}
		}
	}
	return result
}
