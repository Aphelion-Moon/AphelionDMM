package maplight

import (
	"sort"
	"time"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// SkipGroup is one aggregated row of the skipped-atom report.
type SkipGroup struct {
	Path       string
	Reason     string
	Unparsable bool
	Count      int
}

// Report summarises a computed level for the status line.
type Report struct {
	Sources   int  // explicit emitters used
	Starlight int  // derived starlight emitters
	Skipped   int  // atoms that look like lights but were not used
	Capped    bool // explicit sources were truncated to SourceCap
	Groups    []SkipGroup
}

const maxReportRows = 200

// levelState is the UI-thread master copy of one level's lighting inputs. The
// worker never sees it; it receives snapshot().
type levelState struct {
	w, h        int
	level       *lighting.Level
	tileSources map[int32][]lighting.Source
	tileSkips   map[int32][]skipKey
	skipCounts  map[skipKey]int
	skipTotal   int
	sourceCount int
	maxSources  int
}

func newLevelState(w, h int) *levelState {
	lvl := lighting.NewLevel(w, h)
	lvl.Ambient = make([]lighting.RGB, w*h)
	lvl.Unlit = make([]bool, w*h)
	lvl.Space = make([]bool, w*h)
	return &levelState{
		w: w, h: h, level: lvl, maxSources: SourceCap,
		tileSources: make(map[int32][]lighting.Source),
		tileSkips:   make(map[int32][]skipKey),
		skipCounts:  make(map[skipKey]int),
	}
}

// captureTile reads one zero-based tile of dmm level z into the master level.
func (s *levelState) captureTile(d *dmmap.Dmm, z int, cls *Classifier, x, y int) {
	idx := y*s.w + x
	key := int32(idx)
	if old, ok := s.tileSources[key]; ok {
		s.sourceCount -= len(old)
		delete(s.tileSources, key)
	}
	if old, ok := s.tileSkips[key]; ok {
		for _, k := range old {
			if s.skipCounts[k]--; s.skipCounts[k] <= 0 {
				delete(s.skipCounts, k)
			}
		}
		s.skipTotal -= len(old)
		delete(s.tileSkips, key)
	}

	var opaque, space, hasArea bool
	base, static := lighting.AreaBase{Color: lighting.RGB{R: 1, G: 1, B: 1}}, true
	var sources []lighting.Source
	var skips []skipKey
	if tile := d.GetTile(util.Point{X: x + 1, Y: y + 1, Z: z}); tile != nil {
		for _, in := range tile.Instances() {
			p := in.Prefab()
			if p == nil {
				continue
			}
			info := cls.info(p)
			if info.area && !hasArea {
				hasArea, base, static = true, info.base, info.static
			}
			opaque = opaque || info.opaque
			space = space || info.space
			if info.emits {
				src := info.source
				src.X, src.Y = x, y
				sources = append(sources, src)
			}
			skips = append(skips, info.skips...)
		}
	}
	s.level.SetOpaque(x, y, opaque)
	s.level.Space[idx] = space
	s.level.Unlit[idx] = !static
	s.level.Ambient[idx] = lighting.ResolveAmbient(base, space, cls.set.starlight())
	if len(sources) > 0 {
		s.tileSources[key] = sources
		s.sourceCount += len(sources)
	}
	if len(skips) > 0 {
		s.tileSkips[key] = skips
		for _, k := range skips {
			s.skipCounts[k]++
		}
		s.skipTotal += len(skips)
	}
}

// recapture re-reads the given one-based points and returns the zero-based
// bounding rectangle of the tiles that were read.
func (s *levelState) recapture(d *dmmap.Dmm, z int, cls *Classifier, points []util.Point) lighting.Rect {
	var dirty lighting.Rect
	for _, p := range points {
		x, y := p.X-1, p.Y-1
		if x < 0 || y < 0 || x >= s.w || y >= s.h {
			continue
		}
		s.captureTile(d, z, cls, x, y)
		dirty = dirty.Union(lighting.RectAround(x, y))
	}
	return dirty
}

// flatSources lists sources in row-major tile order, deterministic so that an
// incremental update equals a fresh compute. It truncates at maxSources.
func (s *levelState) flatSources() []lighting.Source {
	keys := make([]int32, 0, len(s.tileSources))
	for k := range s.tileSources {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]lighting.Source, 0, min(s.sourceCount, s.maxSources))
	for _, k := range keys {
		for _, src := range s.tileSources[k] {
			if len(out) >= s.maxSources {
				return out
			}
			out = append(out, src)
		}
	}
	return out
}

// snapshot returns an independent Level safe to hand to a worker.
func (s *levelState) snapshot() *lighting.Level {
	lvl := s.level.Clone()
	lvl.Sources = s.flatSources()
	return lvl
}

func (s *levelState) report() Report {
	rep := Report{Sources: min(s.sourceCount, s.maxSources), Skipped: s.skipTotal, Capped: s.sourceCount > s.maxSources}
	for k, n := range s.skipCounts {
		rep.Groups = append(rep.Groups, SkipGroup{Path: k.Path, Reason: k.Reason, Unparsable: k.Unparsable, Count: n})
	}
	sort.Slice(rep.Groups, func(i, j int) bool {
		a, b := rep.Groups[i], rep.Groups[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Reason < b.Reason
	})
	if len(rep.Groups) > maxReportRows {
		rep.Groups = rep.Groups[:maxReportRows]
	}
	return rep
}

// Builder captures a whole level in bounded steps on the UI thread.
type Builder struct {
	dmm           *dmmap.Dmm
	z             int
	cls           *Classifier
	st            *levelState
	next          int
	tilesPerCheck int
	// maxPerStep, when positive, caps the tiles captured by one Step call
	// regardless of the clock; tests use it to pin multi-frame behaviour.
	maxPerStep int
}

func newBuilder(d *dmmap.Dmm, z int, cls *Classifier, st *levelState) *Builder {
	st.level.Starlight = cls.set.starlight()
	return &Builder{dmm: d, z: z, cls: cls, st: st, tilesPerCheck: 256}
}

// Step captures tiles until the budget is spent (budget <= 0 means unbounded)
// and reports whether the level is complete.
func (b *Builder) Step(budget time.Duration) bool {
	total := b.st.w * b.st.h
	var deadline time.Time
	if budget > 0 {
		deadline = time.Now().Add(budget)
	}
	start := b.next
	for b.next < total {
		if b.maxPerStep > 0 && b.next-start >= b.maxPerStep {
			return false
		}
		for n := 0; n < b.tilesPerCheck && b.next < total; n++ {
			b.st.captureTile(b.dmm, b.z, b.cls, b.next%b.st.w, b.next/b.st.w)
			b.next++
		}
		if budget > 0 && b.next < total && !time.Now().Before(deadline) {
			return false
		}
	}
	return true
}
