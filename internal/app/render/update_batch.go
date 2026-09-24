// APHELION EDIT ADDITION START - FRAME GEOMETRY BATCH
package render

import (
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

type renderUpdateBatch struct {
	depth  int
	levels map[int]map[util.Point]struct{}
}

func (r *Render) BeginUpdateBatch() { r.updates.depth++ }
func (r *Render) EndUpdateBatch(dmm *dmmap.Dmm) {
	if r.updates.depth == 0 {
		return
	}
	r.updates.depth--
	if r.updates.depth != 0 {
		return
	}
	for level, points := range r.updates.levels {
		coords := make([]util.Point, 0, len(points))
		for coord := range points {
			coords = append(coords, coord)
		}
		r.bucket.UpdateLevel(dmm, level, coords)
	}
	r.updates.levels = nil
}

func (r *Render) queueBucketUpdate(level int, coords []util.Point) bool {
	if r.updates.depth == 0 {
		return false
	}
	if r.updates.levels == nil {
		r.updates.levels = make(map[int]map[util.Point]struct{})
	}
	points, exists := r.updates.levels[level]
	if exists && points == nil {
		return true
	} // nil means rebuild the entire level.
	if len(coords) == 0 {
		r.updates.levels[level] = nil
		return true
	}
	if points == nil {
		points = make(map[util.Point]struct{})
		r.updates.levels[level] = points
	}
	for _, coord := range coords {
		points[coord] = struct{}{}
	}
	return true
}

// APHELION EDIT ADDITION END
