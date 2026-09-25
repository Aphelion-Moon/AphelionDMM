// APHELION EDIT ADDITION START - OWNED MAP OPEN
package render

import (
	"math"
	"sdmm/internal/aphelion/resources"
	"sort"
	"time"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

const (
	levelBuildFrameTime   = 2 * time.Millisecond
	levelBuildFrameChunks = 2
)

// LevelBuildBudget is shared across every open map for one UI frame.
type LevelBuildBudget struct {
	deadline  time.Time
	remaining int
}

func NewLevelBuildBudget() *LevelBuildBudget {
	return &LevelBuildBudget{time.Now().Add(resources.FrameWorkRemaining(levelBuildFrameTime)), levelBuildFrameChunks}
}
func (b *LevelBuildBudget) Available() bool {
	return b != nil && b.remaining > 0 && time.Now().Before(b.deadline)
}
func (b *LevelBuildBudget) consume() bool {
	if !b.Available() {
		return false
	}
	b.remaining--
	return true
}

type levelBuild struct {
	dmm              *dmmap.Dmm
	level            int
	generation       uint64
	points           []util.Point
	next             int
	ordered          bool
	centerX, centerY float64
}

func (r *Render) ensureLevelBuildMap(dmm *dmmap.Dmm) {
	if dmm == nil {
		return
	}
	dimensions := [3]int{dmm.MaxX, dmm.MaxY, dmm.MaxZ}
	if r.levelBuildDmm == dmm && r.levelBuildDimensions == dimensions {
		return
	}
	r.levelBuildGeneration++
	r.releaseGeometry()
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	r.clearRetainedScene()
	// APHELION EDIT ADDITION END
	r.bucket.Reset()
	r.levelBuildDmm, r.levelBuildDimensions = dmm, dimensions
	r.levelBuilds = make(map[int]*levelBuild, dmm.MaxZ)
	r.levelReady = make(map[int]bool, dmm.MaxZ)
}

// InvalidateLevelBuilds fences old partial work after an in-place full snapshot
// replacement, even when the Dmm pointer and dimensions remain unchanged.
func (r *Render) InvalidateLevelBuilds(dmm *dmmap.Dmm) {
	r.releaseGeometry()
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	r.clearRetainedScene()
	// APHELION EDIT ADDITION END
	r.levelBuildGeneration++
	r.bucket.Reset()
	r.levelBuildDmm = nil
	r.levelBuildDimensions = [3]int{}
	r.levelBuilds = nil
	r.levelReady = nil
	r.ensureLevelBuildMap(dmm)
	r.BeginLevelBuild(dmm, r.Camera.Level)
}

// BeginLevelBuild queues every valid Z. Changing the active Z only changes
// priority; completed chunks in every other job remain retained.
func (r *Render) BeginLevelBuild(dmm *dmmap.Dmm, activeLevel int) {
	r.ensureLevelBuildMap(dmm)
	if dmm == nil {
		return
	}
	for z := 1; z <= dmm.MaxZ; z++ {
		if r.levelReady[z] || r.levelBuilds[z] != nil {
			continue
		}
		r.levelBuilds[z] = &levelBuild{dmm: dmm, level: z, generation: r.levelBuildGeneration}
	}
	r.Camera.Level = activeLevel
}

// ProcessLevelBuild preserves the direct-call test/debug seam. Production uses
// one shared budget in WsArea so hidden documents keep warming too.
func (r *Render) ProcessLevelBuild() {
	b := NewLevelBuildBudget()
	for b.Available() && r.ProcessLevelBuildBudget(b) {
	}
}

// ProcessLevelBuildBudget advances one metadata or chunk step.
func (r *Render) ProcessLevelBuildBudget(b *LevelBuildBudget) bool {
	defer resources.ChargeFrameWork(time.Now())
	if b == nil || !b.Available() {
		return false
	}
	job := r.nextLevelBuild()
	if job == nil {
		return false
	}
	if job.generation != r.levelBuildGeneration || job.dmm != r.levelBuildDmm {
		delete(r.levelBuilds, job.level)
		return true
	}
	if job.points == nil {
		if !b.consume() {
			return false
		}
		if !r.admitGeometry(job.dmm, job.level, nil, true) {
			return false
		}
		job.points = r.bucket.PrepareLevel(job.dmm, job.level)
		if len(job.points) == 0 {
			r.markLevelReady(job.level)
		}
		return true
	}
	if job.next >= len(job.points) {
		r.markLevelReady(job.level)
		return true
	}
	if !b.consume() {
		return false
	}
	point := r.nextViewportChunk(job)
	if !r.admitGeometry(job.dmm, job.level, []util.Point{point}, false) {
		return false
	}
	r.bucket.UpdateLevel(job.dmm, job.level, []util.Point{point})
	job.next++
	if job.next == len(job.points) {
		r.markLevelReady(job.level)
	}
	return true
}

// Re-evaluate the nearest not-yet-built chunk each step so panning during
// warm-up changes viewport priority without discarding completed chunks.
func (r *Render) nextViewportChunk(job *levelBuild) util.Point {
	cx, cy := float64(job.dmm.MaxX)/2, float64(job.dmm.MaxY)/2
	if r.viewportWidth > 0 && r.viewportHeight > 0 && r.Camera.Scale > 0 {
		size := float64(dmmap.WorldIconSize)
		cx = (-float64(r.Camera.ShiftX)+float64(r.viewportWidth)/(2*float64(r.Camera.Scale)))/size + 1
		cy = (-float64(r.Camera.ShiftY)+float64(r.viewportHeight)/(2*float64(r.Camera.Scale)))/size + 1
		cx = math.Max(1, math.Min(float64(job.dmm.MaxX), cx))
		cy = math.Max(1, math.Min(float64(job.dmm.MaxY), cy))
	}
	if !job.ordered || job.centerX != cx || job.centerY != cy {
		remaining := job.points[job.next:]
		sort.Slice(remaining, func(i, j int) bool {
			a, b := remaining[i], remaining[j]
			ax, ay, bx, by := float64(a.X)-cx, float64(a.Y)-cy, float64(b.X)-cx, float64(b.Y)-cy
			ad, bd := ax*ax+ay*ay, bx*bx+by*by
			return ad < bd || ad == bd && (a.Y < b.Y || a.Y == b.Y && a.X < b.X)
		})
		job.ordered, job.centerX, job.centerY = true, cx, cy
	}
	return job.points[job.next]
}

func (r *Render) nextLevelBuild() *levelBuild {
	var best *levelBuild
	bestPriority := int(^uint(0) >> 1)
	for z, job := range r.levelBuilds {
		if job == nil {
			continue
		}
		priority := r.levelBuildPriority(z)
		if best == nil || priority < bestPriority || priority == bestPriority && z < best.level {
			best, bestPriority = job, priority
		}
	}
	return best
}
func (r *Render) levelBuildPriority(z int) int {
	active := r.Camera.Level
	if z == active {
		return 0
	}
	if MultiZRendering && z < active {
		return 1 + active - z
	}
	if z == active-1 || z == active+1 {
		return 10000
	}
	if z < active {
		return 20000 + active - z
	}
	return 20000 + z - active
}
func (r *Render) markLevelReady(level int) {
	if r.levelReady == nil {
		r.levelReady = make(map[int]bool)
	}
	r.levelReady[level] = true
	delete(r.levelBuilds, level)
}
func (r *Render) LevelReady(level int) bool {
	return r.levelBuildDmm != nil && level >= 1 && level <= r.levelBuildDmm.MaxZ && r.levelReady[level]
}
func (r *Render) LevelLoading() bool { return !r.LevelReady(r.Camera.Level) }

// CancelLevelBuilds invalidates queued work as a canvas/document is replaced.
func (r *Render) CancelLevelBuilds() {
	r.releaseGeometry()
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	r.clearRetainedScene()
	// APHELION EDIT ADDITION END
	r.levelBuildGeneration++
	r.levelBuildDmm = nil
	r.levelBuildDimensions = [3]int{}
	r.levelBuilds = nil
	r.levelReady = nil
	if r.bucket != nil {
		r.bucket.Reset()
	}
}

// APHELION EDIT ADDITION END
