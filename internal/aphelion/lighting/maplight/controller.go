package maplight

import (
	"sync"
	"time"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

const (
	// maxDirtyPoints bounds the tiles replayed on the UI thread; a larger burst
	// becomes a full rebuild that is captured in budgeted steps.
	maxDirtyPoints = 4096
	// fullRecomputeFraction: a dirty rectangle covering more than this share of
	// the level is cheaper to compute from scratch.
	fullRecomputeNum, fullRecomputeDen = 1, 2
)

// Input is everything a Tick needs from the pane.
type Input struct {
	Dmm   *dmmap.Dmm
	Level int
	// Env identifies the loaded environment. It is compared, never dereferenced.
	Env      any
	Settings Settings
	// ViewVersion is Editor.MapViewVersion. A change without an observed dirty
	// region forces a full recompute.
	ViewVersion uint64
	// Budget bounds the UI-thread capture work done by this call (<=0: unbounded).
	Budget time.Duration
}

// Stats counts scheduling decisions; used by tests and diagnostics.
type Stats struct {
	Full, Incremental, Dropped, InFlightMax int
}

type buildKey struct {
	dmm   *dmmap.Dmm
	level int
	env   any
	w, h  int
}

type job struct {
	epoch, seq uint64
	level      *lighting.Level
	opts       lighting.Options
	prev       *result
	dirty      lighting.Rect
	full       bool
	z          int
}

type result struct {
	epoch, seq uint64
	z, w, h    int
	res        *lighting.Result
	tiles      []float32
	rows       rowSpan
	sources    []lighting.Source
	starlight  int
}

// Controller owns the preview of one level of one map view. Every method except
// the worker body is UI-thread only.
type Controller struct {
	run func(*job) *result

	mu       sync.Mutex
	done     *result
	inFlight bool
	closed   bool

	haveKey     bool
	key         buildKey
	compute     computeKey
	set         Settings
	epoch       uint64
	cls         *Classifier
	state       *levelState
	builder     *Builder
	dirty       lighting.Rect
	dirtyPoints []util.Point
	dirtyAll    bool
	needFull    bool
	seenVersion uint64
	jobSeq      uint64
	appliedSeq  uint64
	shown       *result
	frame       *Frame
	report      Report
	stats       Stats
}

// NewController returns an idle controller.
func NewController() *Controller { return &Controller{run: runJob} }

// Close fences any in-flight result and releases state.
func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	c.done = nil
	c.mu.Unlock()
	c.release()
}

// Stats returns scheduling counters.
func (c *Controller) Stats() Stats { return c.stats }

// Report returns the current source/skip summary.
func (c *Controller) Report() Report { return c.report }

// Sources returns the explicit emitters of the shown frame (immutable).
func (c *Controller) Sources() []lighting.Source {
	if c.shown == nil {
		return nil
	}
	return c.shown.sources
}

// Busy reports pending capture, computation or dirty regions.
func (c *Controller) Busy() bool {
	c.mu.Lock()
	pending := c.inFlight || c.done != nil
	c.mu.Unlock()
	return pending || c.builder != nil || c.needFull || !c.dirty.Empty() || c.hasDirtyPoints()
}

func (c *Controller) hasDirtyPoints() bool { return c.dirtyAll || len(c.dirtyPoints) > 0 }

// MarkDirty records changed one-based tiles of a level (0 = any level). Empty
// points mean the whole level changed. Callers pass display-tile coordinates, as
// render.UpdateBucketV does.
func (c *Controller) MarkDirty(level int, points []util.Point) {
	if !c.haveKey || (level != 0 && level != c.key.level) {
		return
	}
	if len(points) == 0 {
		c.dirtyAll = true
		return
	}
	for _, p := range points {
		if p.X < 1 || p.Y < 1 || p.X > c.key.w || p.Y > c.key.h {
			continue
		}
		if len(c.dirtyPoints) >= maxDirtyPoints {
			c.dirtyAll, c.dirtyPoints = true, nil
			return
		}
		c.dirtyPoints = append(c.dirtyPoints, p)
	}
}

func (c *Controller) release() {
	c.haveKey = false
	c.epoch++
	c.cls, c.state, c.builder = nil, nil, nil
	c.dirty, c.dirtyPoints, c.dirtyAll, c.needFull = lighting.Rect{}, nil, false, false
	c.shown, c.frame, c.report = nil, nil, Report{}
}

func (c *Controller) startRebuild(in Input) {
	c.builder = newBuilder(in.Dmm, in.Level, c.cls, newLevelState(in.Dmm.MaxX, in.Dmm.MaxY))
	c.dirty, c.dirtyPoints, c.dirtyAll = lighting.Rect{}, nil, false
	c.needFull = true
}

// Tick advances capture, publishes a finished result and schedules work. It
// returns the frame to draw, or nil when nothing valid is available.
func (c *Controller) Tick(in Input) *Frame {
	if !in.Settings.Enabled || in.Dmm == nil || in.Level < 1 || in.Level > in.Dmm.MaxZ || in.Dmm.MaxX < 1 || in.Dmm.MaxY < 1 {
		if c.haveKey {
			c.release()
		}
		return nil
	}
	key := buildKey{in.Dmm, in.Level, in.Env, in.Dmm.MaxX, in.Dmm.MaxY}
	ck := in.Settings.computeKey()
	switch {
	case !c.haveKey || key != c.key:
		c.release()
		c.haveKey, c.key, c.compute, c.set = true, key, ck, in.Settings
		c.cls = NewClassifier(in.Settings)
		c.startRebuild(in)
	case ck != c.compute:
		// Same level, different options: keep the old frame on screen until the
		// replacement lands, but fence anything still running.
		c.compute, c.set = ck, in.Settings
		c.epoch++
		c.cls = NewClassifier(in.Settings)
		c.startRebuild(in)
	}
	c.poll()

	if c.builder != nil {
		if c.dirtyAll {
			c.startRebuild(in)
		}
		if !c.builder.Step(in.Budget) {
			c.seenVersion = in.ViewVersion
			return c.frame
		}
		c.state, c.builder = c.builder.st, nil
		// Edits that arrived while capturing may precede or follow the tile
		// reads, so re-read them now; this also settles the version fence.
		if len(c.dirtyPoints) > 0 {
			c.state.recapture(in.Dmm, in.Level, c.cls, c.dirtyPoints)
			c.dirtyPoints = nil
		}
		c.needFull = true
	} else if c.dirtyAll || (in.ViewVersion != c.seenVersion && len(c.dirtyPoints) == 0) {
		c.startRebuild(in)
		c.seenVersion = in.ViewVersion
		if !c.builder.Step(in.Budget) {
			return c.frame
		}
		c.state, c.builder = c.builder.st, nil
	} else if len(c.dirtyPoints) > 0 {
		rect := c.state.recapture(in.Dmm, in.Level, c.cls, c.dirtyPoints)
		c.dirty = c.dirty.Union(rect)
		c.dirtyPoints = nil
	}
	c.seenVersion = in.ViewVersion
	c.launch()
	return c.frame
}

// poll applies a finished result when it still matches the current fence.
func (c *Controller) poll() {
	c.mu.Lock()
	res := c.done
	c.done = nil
	c.mu.Unlock()
	if res == nil {
		return
	}
	if res.epoch != c.epoch || res.seq <= c.appliedSeq || !c.haveKey || res.z != c.key.level || res.w != c.key.w || res.h != c.key.h {
		c.stats.Dropped++
		return
	}
	var prev *Frame
	if c.shown != nil && c.shown.epoch == res.epoch {
		prev = c.frame
	}
	c.appliedSeq = res.seq
	c.shown = res
	c.frame = newFrame(res.z, res.w, res.h, res.tiles, prev, res.rows)
	if c.state != nil {
		c.report = c.state.report()
		c.report.Starlight = res.starlight
	}
}

// launch starts a worker job when one is needed and none is running. At most
// one job runs; edits that arrive meanwhile accumulate in dirty and become the
// single pending job.
func (c *Controller) launch() {
	if c.builder != nil || c.state == nil || (!c.needFull && c.dirty.Empty()) {
		return
	}
	c.mu.Lock()
	if c.inFlight || c.closed {
		c.mu.Unlock()
		return
	}
	c.inFlight = true
	c.mu.Unlock()
	c.stats.InFlightMax = max(c.stats.InFlightMax, 1)

	w, h := c.key.w, c.key.h
	var prev *result
	if c.shown != nil && c.shown.epoch == c.epoch && c.shown.w == w && c.shown.h == h {
		prev = c.shown
	}
	full := c.needFull || prev == nil ||
		(c.dirty.X1-c.dirty.X0)*(c.dirty.Y1-c.dirty.Y0)*fullRecomputeDen > w*h*fullRecomputeNum
	c.jobSeq++
	j := &job{
		epoch: c.epoch, seq: c.jobSeq, z: c.key.level,
		level: c.state.snapshot(), opts: c.set.options(),
		prev: prev, dirty: c.dirty, full: full,
	}
	if full {
		c.stats.Full++
	} else {
		c.stats.Incremental++
	}
	c.dirty, c.needFull = lighting.Rect{}, false
	run := c.run
	go func() {
		res := run(j)
		c.mu.Lock()
		c.inFlight = false
		if !c.closed {
			c.done = res
		}
		c.mu.Unlock()
	}()
}

// runJob is the worker body. It reads only the job's immutable snapshot and
// clones the previous result before updating it.
func runJob(j *job) *result {
	lvl := j.level
	w, h := lvl.W, lvl.H
	out := &result{epoch: j.epoch, seq: j.seq, z: j.z, w: w, h: h, sources: lvl.Sources}
	if lvl.Starlight.Enabled {
		out.starlight = len(lvl.StarlightSources())
	}
	if j.full || j.prev == nil {
		out.res = lighting.Compute(lvl, j.opts)
		out.tiles = make([]float32, w*h*12)
		fillTiles(out.res, lvl, out.tiles, lighting.Rect{X0: 0, Y0: 0, X1: w, Y1: h})
		out.rows = rowSpan{y0: 0, y1: h, full: true}
		return out
	}
	out.res = j.prev.res.Clone()
	out.res.Update(lvl, j.dirty)
	out.tiles = append([]float32(nil), j.prev.tiles...)
	aff := j.opts.AffectedTiles(j.dirty, w, h)
	fillTiles(out.res, lvl, out.tiles, aff)
	out.rows = rowSpan{y0: aff.Y0, y1: aff.Y1}
	return out
}

// fillTiles writes the displayed corner colours of every tile in rect.
func fillTiles(res *lighting.Result, lvl *lighting.Level, tiles []float32, rect lighting.Rect) {
	for y := rect.Y0; y < rect.Y1; y++ {
		for x := rect.X0; x < rect.X1; x++ {
			t := res.Tile(lvl, x, y)
			base := (y*lvl.W + x) * 12
			for k := 0; k < 4; k++ {
				tiles[base+k*3+0] = float32(t[k].R)
				tiles[base+k*3+1] = float32(t[k].G)
				tiles[base+k*3+2] = float32(t[k].B)
			}
		}
	}
}
