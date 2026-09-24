package editing

import (
	"fmt"
	"sdmm/internal/util"
	"sort"
)

// Selection is immutable geometry. Rectangles remain lazy; sparse masks own
// their coordinates. The editor/tool owning it supplies document identity.
// Bounds never grant permission to write a hole in a mask.
type Selection struct {
	area    util.Bounds
	z       int
	points  []util.Point
	members map[util.Point]struct{}
	offset  util.Point
	runs    []util.Bounds
}

func RectangleSelection(area util.Bounds, z int) Selection {
	if area.X1 > area.X2 {
		area.X1, area.X2 = area.X2, area.X1
	}
	if area.Y1 > area.Y2 {
		area.Y1, area.Y2 = area.Y2, area.Y1
	}
	return Selection{area: area, z: z}
}
func MaskSelection(points []util.Point) (Selection, error) {
	s := Selection{members: make(map[util.Point]struct{}, len(points))}
	for _, p := range points {
		if p.X < 1 || p.Y < 1 || p.Z < 1 || s.z != 0 && p.Z != s.z {
			return Selection{}, fmt.Errorf("selection must contain valid tiles on one level")
		}
		if _, ok := s.members[p]; ok {
			continue
		}
		s.members[p] = struct{}{}
		if s.z == 0 {
			s.area = util.Bounds{X1: float32(p.X), Y1: float32(p.Y), X2: float32(p.X), Y2: float32(p.Y)}
			s.z = p.Z
		}
		s.area.X1 = min(s.area.X1, float32(p.X))
		s.area.Y1 = min(s.area.Y1, float32(p.Y))
		s.area.X2 = max(s.area.X2, float32(p.X))
		s.area.Y2 = max(s.area.Y2, float32(p.Y))
		s.points = append(s.points, p)
	}
	sort.Slice(s.points, func(i, j int) bool {
		a, b := s.points[i], s.points[j]
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Y < b.Y
	})
	for _, p := range s.points {
		n := len(s.runs)
		if n > 0 && s.runs[n-1].X1 == float32(p.X) && s.runs[n-1].Y2+1 == float32(p.Y) {
			s.runs[n-1].Y2++
		} else {
			s.runs = append(s.runs, util.Bounds{X1: float32(p.X), X2: float32(p.X), Y1: float32(p.Y), Y2: float32(p.Y)})
		}
	}
	return s, nil
}
func (s Selection) Bounds() util.Bounds { return s.area }
func (s Selection) Level() int          { return s.z }
func (s Selection) Sparse() bool        { return s.members != nil }
func (s Selection) Len() int {
	if s.Sparse() {
		return len(s.points)
	}
	if s.z == 0 {
		return 0
	}
	return (int(s.area.X2-s.area.X1) + 1) * (int(s.area.Y2-s.area.Y1) + 1)
}
func (s Selection) Contains(p util.Point) bool {
	if s.Sparse() {
		_, ok := s.members[p.Minus(s.offset)]
		return ok
	}
	return p.Z == s.z && s.z != 0 && s.area.Contains(float32(p.X), float32(p.Y))
}
func (s Selection) Visit(visit func(util.Point)) {
	if s.Sparse() {
		for _, p := range s.points {
			visit(p.Plus(s.offset))
		}
		return
	}
	if s.z == 0 {
		return
	}
	for x := int(s.area.X1); x <= int(s.area.X2); x++ {
		for y := int(s.area.Y1); y <= int(s.area.Y2); y++ {
			visit(util.Point{X: x, Y: y, Z: s.z})
		}
	}
}
func (s Selection) Coordinates() []util.Point {
	out := make([]util.Point, 0, s.Len())
	s.Visit(func(p util.Point) { out = append(out, p) })
	return out
}
func (s Selection) translated(shift util.Point) Selection {
	s.area = s.area.Plus(float32(shift.X), float32(shift.Y))
	s.z += shift.Z
	s.offset = s.offset.Plus(shift)
	return s
}

// VisitRuns draws geometry without scanning every tile or filling mask holes.
func (s Selection) VisitRuns(visit func(util.Bounds)) {
	if !s.Sparse() {
		if s.Len() > 0 {
			visit(s.area)
		}
		return
	}
	for _, run := range s.runs {
		visit(run.Plus(float32(s.offset.X), float32(s.offset.Y)))
	}
}
func (s Selection) Translate(shift util.Point) Selection { return s.translated(shift) }
func (s Selection) Rotate(clockwise bool) Selection {
	area := s.area
	w, h := int(area.X2-area.X1+1), int(area.Y2-area.Y1+1)
	if !s.Sparse() {
		area.X2 = area.X1 + float32(h) - 1
		area.Y2 = area.Y1 + float32(w) - 1
		return RectangleSelection(area, s.z)
	}
	points := s.Coordinates()
	for i, p := range points {
		x, y := p.X-int(area.X1), p.Y-int(area.Y1)
		dx, dy := y, w-1-x
		if !clockwise {
			dx, dy = h-1-y, x
		}
		points[i] = util.Point{X: int(area.X1) + dx, Y: int(area.Y1) + dy, Z: s.z}
	}
	out, _ := MaskSelection(points)
	return out
}
func (s Selection) Mirror(axis MirrorAxis) Selection {
	if !s.Sparse() {
		return s
	}
	points := s.Coordinates()
	for i, p := range points {
		if axis == MirrorHorizontal {
			p.X = int(s.area.X1+s.area.X2) - p.X
		} else {
			p.Y = int(s.area.Y1+s.area.Y2) - p.Y
		}
		points[i] = p
	}
	out, _ := MaskSelection(points)
	return out
}
