package editing

import (
	"fmt"
	"math/bits"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"strings"
)

// AreaSelectionQuery is a resumable UI-owner traversal. Callers fence it by
// document/view version, and publish only its complete immutable result.
type AreaSelectionQuery struct {
	m              *dmmap.Dmm
	z              int
	all            bool
	target         string
	queue          []util.Point
	seen, selected []uint64
	next, scan     int
	collecting     bool
	result         Selection
}

func areaContentKey(m *dmmap.Dmm, p util.Point) string {
	for _, i := range m.GetTile(p).Instances() {
		if i == nil || i.Prefab() == nil {
			continue
		}
		path := i.Prefab().Path()
		if path == "/area" || strings.HasPrefix(path, "/area/") {
			return i.Prefab().ContentKey()
		}
	}
	return ""
}
func NewAreaSelectionQuery(m *dmmap.Dmm, start util.Point, all bool) (*AreaSelectionQuery, error) {
	if m == nil || !m.HasTile(start) {
		return nil, fmt.Errorf("select a tile on the map")
	}
	key := areaContentKey(m, start)
	if key == "" {
		return nil, fmt.Errorf("selected tile has no area definition")
	}
	if m.MaxX > model.MaxMapDimension || m.MaxY > model.MaxMapDimension {
		return nil, fmt.Errorf("area selection exceeds supported map dimensions")
	}
	q := &AreaSelectionQuery{m: m, z: start.Z, all: all, target: key, queue: []util.Point{start}, result: Selection{z: start.Z, runBased: true}}
	if !all {
		words := (m.MaxX*m.MaxY + 63) / 64
		q.seen, q.selected = make([]uint64, words), make([]uint64, words)
		q.mark(q.seen, start)
	}
	return q, nil
}

func (q *AreaSelectionQuery) mark(set []uint64, p util.Point) bool {
	i := (p.X-1)*q.m.MaxY + p.Y - 1
	bit := uint64(1) << uint(i%64)
	previous := set[i/64]&bit != 0
	set[i/64] |= bit
	return previous
}
func (q *AreaSelectionQuery) appendPoint(p util.Point) {
	s := &q.result
	if s.runCount == 0 {
		s.area = util.Bounds{X1: float32(p.X), X2: float32(p.X), Y1: float32(p.Y), Y2: float32(p.Y)}
	}
	s.area.X1 = min(s.area.X1, float32(p.X))
	s.area.X2 = max(s.area.X2, float32(p.X))
	s.area.Y1 = min(s.area.Y1, float32(p.Y))
	s.area.Y2 = max(s.area.Y2, float32(p.Y))
	n := len(s.runs)
	if n > 0 && s.runs[n-1].X1 == float32(p.X) && s.runs[n-1].Y2+1 == float32(p.Y) {
		s.runs[n-1].Y2++
	} else {
		s.runs = append(s.runs, util.Bounds{X1: float32(p.X), X2: float32(p.X), Y1: float32(p.Y), Y2: float32(p.Y)})
	}
	s.runCount++
}
func (q *AreaSelectionQuery) Step(limit int) (Selection, bool) {
	for work := 0; work < limit; work++ {
		if q.collecting {
			// Consume selected bits in coordinate order, without a final whole-mask
			// sort/allocation on the graphics thread. Empty words cost one step.
			if q.scan >= len(q.selected) {
				q.seen = nil
				q.selected = nil
				return q.result, true
			}
			word := q.selected[q.scan]
			if word == 0 {
				q.scan++
				continue
			}
			bit := bits.TrailingZeros64(word)
			q.selected[q.scan] &= word - 1
			i := q.scan*64 + bit
			q.appendPoint(util.Point{X: i/q.m.MaxY + 1, Y: i%q.m.MaxY + 1, Z: q.z})
			continue
		}
		if q.all {
			if q.next >= q.m.MaxX*q.m.MaxY {
				return q.result, true
			}
			p := util.Point{X: q.next/q.m.MaxY + 1, Y: q.next%q.m.MaxY + 1, Z: q.z}
			q.next++
			if areaContentKey(q.m, p) == q.target {
				q.appendPoint(p)
			}
		} else {
			if q.next >= len(q.queue) {
				q.queue = nil
				q.collecting = true
				continue
			}
			p := q.queue[q.next]
			q.next++
			q.mark(q.selected, p)
			for _, step := range []util.Point{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}} {
				n := p.Plus(step)
				if q.m.HasTile(n) && !q.mark(q.seen, n) {
					if areaContentKey(q.m, n) == q.target {
						q.queue = append(q.queue, n)
					}
				}
			}
			// Retain the frontier rather than every processed tile.
			if q.next >= 4096 {
				copy(q.queue, q.queue[q.next:])
				q.queue = q.queue[:len(q.queue)-q.next]
				q.next = 0
			}
		}
	}
	return Selection{}, false
}
