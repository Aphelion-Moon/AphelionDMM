package editing

import (
	"fmt"
	"math"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
	"sort"
	"time"
)

type ShapeKind uint8

const (
	ShapeRectangle ShapeKind = iota
	ShapeEllipse
	ShapeCircle
)

type ShapeDescriptor struct {
	Kind          ShapeKind
	Width, Height int
	Outline       bool
	Thickness     int
}

// ShapeSelection tests cell centers against an ellipse centered in its integer
// tile rectangle. Odd/even dimensions share that convention. The anchor is the
// bottom-left tile; span storage is O(width), including filled large shapes.
func ShapeSelection(d ShapeDescriptor, anchor util.Point) (Selection, error) {
	if d.Kind > ShapeCircle || d.Width < 1 || d.Height < 1 || d.Width > model.MaxMapDimension || d.Height > model.MaxMapDimension || anchor.Z < 1 {
		return Selection{}, fmt.Errorf("shape dimensions must be between 1 and %d tiles", model.MaxMapDimension)
	}
	if d.Kind == ShapeCircle {
		d.Height = d.Width
	}
	if !d.Outline && d.Kind == ShapeRectangle {
		return RectangleSelection(util.Bounds{X1: float32(anchor.X), Y1: float32(anchor.Y), X2: float32(anchor.X + d.Width - 1), Y2: float32(anchor.Y + d.Height - 1)}, anchor.Z), nil
	}
	s := Selection{z: anchor.Z, runBased: true, offset: util.Point{X: anchor.X - 1, Y: anchor.Y - 1}}
	s.area = util.Bounds{X1: float32(anchor.X), Y1: float32(anchor.Y), X2: float32(anchor.X + d.Width - 1), Y2: float32(anchor.Y + d.Height - 1)}
	thickness := max(1, d.Thickness)
	appendRun := func(x, y1, y2 int) {
		if y1 > y2 {
			return
		}
		s.runs = append(s.runs, util.Bounds{X1: float32(x), X2: float32(x), Y1: float32(y1), Y2: float32(y2)})
		s.runCount += y2 - y1 + 1
	}
	for x := 1; x <= d.Width; x++ {
		low, high := 1, d.Height
		if d.Kind != ShapeRectangle {
			low, high = ellipseSpan(x, d.Width, d.Height, 0)
		}
		if low > high {
			continue
		}
		if !d.Outline {
			appendRun(x, low, high)
			continue
		}
		innerLow, innerHigh := thickness+1, d.Height-thickness
		if x <= thickness || x > d.Width-thickness {
			innerLow, innerHigh = 1, 0
		} else if d.Kind != ShapeRectangle {
			innerLow, innerHigh = ellipseSpan(x, d.Width, d.Height, thickness)
		}
		if innerLow > innerHigh {
			appendRun(x, low, high)
		} else {
			appendRun(x, low, innerLow-1)
			appendRun(x, innerHigh+1, high)
		}
	}
	return s, nil
}

func ellipseSpan(x, w, h, inset int) (int, int) {
	iw, ih := w-2*inset, h-2*inset
	if iw <= 0 || ih <= 0 {
		return 1, 0
	}
	x2 := int64(2*x - w - 1)
	w2, h2 := int64(iw)*int64(iw), int64(ih)*int64(ih)
	if x2*x2 > w2 {
		return 1, 0
	}
	// The floating estimate only locates the edge. Integer checks below decide
	// inclusion exactly, so roundoff never changes a preview/commit boundary.
	radius := math.Sqrt(float64(h2) * float64(w2-x2*x2) / float64(w2))
	low := max(1, int(math.Ceil((float64(h+1)-radius)/2)))
	inside := func(y int) bool { y2 := int64(2*y - h - 1); return x2*x2*h2+y2*y2*w2 <= w2*h2 }
	for low <= h && !inside(low) {
		low++
	}
	for low > 1 && inside(low-1) {
		low--
	}
	return low, h + 1 - low
}

// ShapeStroke latches footprint and eligibility at press time and interpolates
// grid centers. Revisited cells occur only once in the eventual operation.
type ShapeStroke struct {
	shape, restriction        Selection
	restrict                  bool
	maxX, maxY                int
	previous                  util.Point
	columns                   map[int][]util.Bounds
	cached                    Selection
	dirty                     bool
	queued                    []util.Point
	segmentStart              util.Point
	segmentStep, segmentSteps int
}

// Queue records pointer samples without doing footprint work in input dispatch.
func (s *ShapeStroke) Queue(p util.Point) { s.queued = append(s.queued, p) }

// Advance limits swept footprint preparation on the UI owner. A released
// gesture is committed only after all queued centers have been covered.
func (s *ShapeStroke) Advance() bool {
	deadline := time.Now().Add(2 * time.Millisecond)
	for count := 0; len(s.queued) > 0 && count < 32; count++ {
		end := s.queued[0]
		if s.segmentSteps == 0 {
			s.segmentStart = s.previous
			if s.segmentStart.Z == 0 {
				s.segmentStart = end
			}
			s.segmentSteps = max(1, max(absShape(end.X-s.segmentStart.X), absShape(end.Y-s.segmentStart.Y)))
			s.segmentStep = 0
		}
		i, steps := s.segmentStep, s.segmentSteps
		point := util.Point{X: s.segmentStart.X + int(math.Round(float64((end.X-s.segmentStart.X)*i)/float64(steps))), Y: s.segmentStart.Y + int(math.Round(float64((end.Y-s.segmentStart.Y)*i)/float64(steps))), Z: end.Z}
		s.Sample(point)
		s.segmentStep++
		if s.segmentStep > s.segmentSteps {
			s.queued = s.queued[1:]
			s.segmentSteps = 0
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return len(s.queued) == 0
}

func NewShapeStroke(shape Selection, maxX, maxY int, restriction Selection, restrict bool) *ShapeStroke {
	return &ShapeStroke{shape: shape, maxX: maxX, maxY: maxY, restriction: restriction, restrict: restrict, columns: make(map[int][]util.Bounds)}
}
func (s *ShapeStroke) Sample(p util.Point) {
	start := s.previous
	if start.Z == 0 {
		start = p
	}
	dx, dy := p.X-start.X, p.Y-start.Y
	steps := max(absShape(dx), absShape(dy))
	steps = max(1, steps)
	for i := 0; i <= steps; i++ {
		center := util.Point{X: start.X + int(math.Round(float64(dx*i)/float64(steps))), Y: start.Y + int(math.Round(float64(dy*i)/float64(steps))), Z: p.Z}
		shape := ClipSelection(s.shape.Translate(util.Point{X: center.X - 1, Y: center.Y - 1, Z: p.Z - s.shape.Level()}), s.maxX, s.maxY)
		shape.VisitRuns(func(run util.Bounds) {
			for x := int(run.X1); x <= int(run.X2); x++ {
				r := util.Bounds{X1: float32(x), X2: float32(x), Y1: run.Y1, Y2: run.Y2}
				column := append(s.columns[x], r)
				sort.Slice(column, func(i, j int) bool { return column[i].Y1 < column[j].Y1 })
				merged := column[:0]
				for _, item := range column {
					if len(merged) > 0 && item.Y1 <= merged[len(merged)-1].Y2+1 {
						merged[len(merged)-1].Y2 = max(merged[len(merged)-1].Y2, item.Y2)
					} else {
						merged = append(merged, item)
					}
				}
				s.columns[x] = merged
				s.dirty = true
			}
		})
	}
	s.previous = p
}
func (s *ShapeStroke) Selection() Selection {
	if s.dirty {
		result := Selection{z: s.previous.Z, runBased: true}
		columns := make([]int, 0, len(s.columns))
		for x := range s.columns {
			columns = append(columns, x)
		}
		sort.Ints(columns)
		for _, x := range columns {
			for _, r := range s.columns[x] {
				if result.runCount == 0 {
					result.area = r
				} else {
					result.area.X2 = max(result.area.X2, r.X2)
					result.area.Y1 = min(result.area.Y1, r.Y1)
					result.area.Y2 = max(result.area.Y2, r.Y2)
				}
				result.runs = append(result.runs, r)
				result.runCount += int(r.Y2-r.Y1) + 1
			}
		}
		if s.restrict {
			result = CombineSelection(result, s.restriction, SelectionIntersect)
		}
		s.cached = result
		s.dirty = false
	}
	return s.cached
}
func absShape(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
