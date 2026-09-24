package editing

import (
	"fmt"
	"sdmm/internal/util"
)

type MutationOutcome uint8

const (
	MutationEmpty MutationOutcome = iota
	MutationChanged
	MutationBlocked
	MutationFailed
)

type EraseResult struct {
	Outcome MutationOutcome
	Coord   util.Point
	Err     error
}

// EraseStroke owns coverage and mode for one gesture. Pixel traversal preserves
// thin/offset sprite hits between OS samples; successful tiles are skipped before
// picking again, so holding or revisiting cannot drill down through a stack.
type EraseStroke struct {
	size      int
	all       bool
	sample    func(int, int, int, bool, func(util.Point) bool) EraseResult
	processed map[util.Point]bool
	last      util.Point
	started   bool
	changes   int
	err       error
}

func NewEraseStroke(iconSize int, all bool, sample func(int, int, int, bool, func(util.Point) bool) EraseResult) *EraseStroke {
	return &EraseStroke{size: max(1, iconSize), all: all, sample: sample, processed: make(map[util.Point]bool)}
}
func (s *EraseStroke) Processed(p util.Point) bool { return s.processed[p] }
func (s *EraseStroke) Changes() int                { return s.changes }
func (s *EraseStroke) Err() error                  { return s.err }
func (s *EraseStroke) All() bool                   { return s.all }
func (s *EraseStroke) VisitProcessed(visit func(util.Point)) {
	for p := range s.processed {
		visit(p)
	}
}
func (s *EraseStroke) Sample(x, y, z int) {
	if s.err != nil {
		return
	}
	point := util.Point{X: x, Y: y, Z: z}
	if !s.started {
		s.started = true
		s.last = point
		s.at(x, y, z)
		return
	}
	if z != s.last.Z {
		s.err = fmt.Errorf("erase stroke interrupted by level change")
		return
	}
	if point == s.last {
		return
	}
	x0, y0 := s.last.X, s.last.Y
	dx, dy := absStroke(x-x0), -absStroke(y-y0)
	sx, sy := -1, -1
	if x0 < x {
		sx = 1
	}
	if y0 < y {
		sy = 1
	}
	distance := dx + dy
	for x0 != x || y0 != y {
		twice := 2 * distance
		if twice >= dy {
			distance += dy
			x0 += sx
		}
		if twice <= dx {
			distance += dx
			y0 += sy
		}
		s.at(x0, y0, z)
		if s.err != nil {
			break
		}
	}
	s.last = point
}
func absStroke(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func StrokeTile(pixel, size int) int {
	if pixel < 0 {
		return (pixel + 1) / size
	}
	return pixel/size + 1
}
func (s *EraseStroke) at(x, y, z int) {
	p := util.Point{X: StrokeTile(x, s.size), Y: StrokeTile(y, s.size), Z: z}
	if s.processed[p] {
		return
	}
	result := s.sample(x, y, z, s.all, s.Processed)
	switch result.Outcome {
	case MutationChanged:
		s.processed[p] = true
		s.processed[result.Coord] = true
		s.changes++
	case MutationBlocked, MutationFailed:
		s.err = result.Err
		if s.err == nil {
			s.err = fmt.Errorf("erase stroke could not continue")
		}
	}
}
