// APHELION EDIT ADDITION START - OWNED MAP OPEN
package render

import (
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	"sort"
	"time"
)

type levelBuild struct {
	dmm    *dmmap.Dmm
	level  int
	points []util.Point
	next   int
}

func (r *Render) BeginLevelBuild(dmm *dmmap.Dmm, level int) {
	points := r.bucket.PrepareLevel(dmm, level)
	// Near-center chunks become visible first on newly opened maps.
	centerX, centerY := dmm.MaxX/2, dmm.MaxY/2
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		return (a.X-centerX)*(a.X-centerX)+(a.Y-centerY)*(a.Y-centerY) < (b.X-centerX)*(b.X-centerX)+(b.Y-centerY)*(b.Y-centerY)
	})
	r.levelBuild = &levelBuild{dmm: dmm, level: level, points: points}
}
func (r *Render) ProcessLevelBuild() {
	build := r.levelBuild
	if build == nil {
		return
	}
	deadline := time.Now().Add(2 * time.Millisecond)
	for count := 0; count < 2 && build.next < len(build.points); count++ {
		point := build.points[build.next]
		build.next++
		r.bucket.UpdateLevel(build.dmm, build.level, []util.Point{point})
		if time.Now().After(deadline) {
			break
		}
	}
	if build.next == len(build.points) {
		r.levelBuild = nil
	}
}

// Pending geometry never changes committed map contents. Input can continue;
// chunk refreshes read the latest display and never install stale geometry.
func (r *Render) LevelLoading() bool { return r.levelBuild != nil }

// APHELION EDIT ADDITION END
