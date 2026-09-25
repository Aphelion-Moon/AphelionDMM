package editing

import (
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
)

// ShapeDeleteFence pins one shape erase assessment to the displayed document.
type ShapeDeleteFence struct {
	DocumentID           model.DocumentID
	Revision             model.Revision
	AttachmentGeneration uint64
	ViewGeneration       uint64
	Level                int
}

// ShapeDeleteTarget carries stable identity across bounded UI assessment steps.
type ShapeDeleteTarget struct {
	Coord    util.Point
	StableID model.StableID
}

type ShapeDeleteTargets map[string]util.Point

// SelectionCursor walks immutable membership without materializing all points.
type SelectionCursor struct {
	selection Selection
	index     int
	run       int
	x, y      int
	started   bool
	done      bool
}

func (s Selection) Cursor() *SelectionCursor { return &SelectionCursor{selection: s} }

func (c *SelectionCursor) Next() (util.Point, bool) {
	if c == nil || c.done || c.selection.z == 0 {
		return util.Point{}, false
	}
	s := c.selection
	if s.runBased {
		for c.run < len(s.runs) {
			run := s.runs[c.run].Plus(float32(s.offset.X), float32(s.offset.Y))
			if !c.started {
				c.x, c.y = int(run.X1), int(run.Y1)
				c.started = true
			}
			if float32(c.x) > run.X2 {
				c.run++
				c.started = false
				continue
			}
			point := util.Point{X: c.x, Y: c.y, Z: s.z}
			c.y++
			if float32(c.y) > run.Y2 {
				c.y = int(run.Y1)
				c.x++
			}
			return point, true
		}
		c.done = true
		return util.Point{}, false
	}
	if s.Sparse() {
		if c.index >= len(s.points) {
			c.done = true
			return util.Point{}, false
		}
		point := s.points[c.index].Plus(s.offset)
		c.index++
		return point, true
	}
	if !c.started {
		c.x, c.y = int(s.area.X1), int(s.area.Y1)
		c.started = true
	}
	if float32(c.x) > s.area.X2 {
		c.done = true
		return util.Point{}, false
	}
	point := util.Point{X: c.x, Y: c.y, Z: s.z}
	c.y++
	if float32(c.y) > s.area.Y2 {
		c.y = int(s.area.Y1)
		c.x++
	}
	return point, true
}
