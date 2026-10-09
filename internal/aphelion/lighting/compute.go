package lighting

import "math"

// Result holds raw per-corner light sums for one Level. Corner (i, j) with
// 0<=i<=W, 0<=j<=H lies at the south-west corner of tile (i, j), that is at
// (i-0.5, j-0.5) in tile-centre coordinates. This is the tg corner lattice
// (lighting_corner.dm:37-39) shifted to a zero-based grid.
type Result struct {
	W, H int
	sum  []RGB
	opts Options
}

// effSource is a normalised emitter plus its working range.
type effSource struct {
	Source
	wr int // ceil(range + visual_offset), lighting_source.dm:470
}

// reach is the corner distance, in tiles, beyond which a source can never
// contribute for the given options.
func reach(opts Options) int {
	return int(math.Ceil(opts.maxRange())) + maxShift + 1
}

const maxShift = 3

// Compute lights a whole level.
func Compute(lvl *Level, opts Options) *Result {
	r := &Result{W: lvl.W, H: lvl.H, sum: make([]RGB, (lvl.W+1)*(lvl.H+1)), opts: opts}
	r.render(lvl, Rect{0, 0, lvl.W + 1, lvl.H + 1})
	return r
}

// Update recomputes the corners that a change inside dirty can affect and
// returns how many corners it recomputed. The caller must include in dirty
// every tile whose opacity, space flag or source set changed, including both
// the old and new positions of moved sources. The result is bit-identical to
// a fresh Compute.
func (r *Result) Update(lvl *Level, dirty Rect) int {
	if lvl.W != r.W || lvl.H != r.H {
		*r = *Compute(lvl, r.opts)
		return len(r.sum)
	}
	if dirty.Empty() {
		return 0
	}
	// A source within reach of a dirty tile can light a corner within reach of
	// that source, so the affected corner region is twice the reach.
	region := dirty.expand(2 * reach(r.opts))
	region.X1++ // tile -> corner index (inclusive upper corner)
	region.Y1++
	region = clipRect(region, Rect{0, 0, lvl.W + 1, lvl.H + 1})
	if region.Empty() {
		return 0
	}
	r.render(lvl, region)
	return (region.X1 - region.X0) * (region.Y1 - region.Y0)
}

func clipRect(r, to Rect) Rect {
	return Rect{max(r.X0, to.X0), max(r.Y0, to.Y0), min(r.X1, to.X1), min(r.Y1, to.Y1)}
}

// render zeroes the corner region [X0,X1)x[Y0,Y1) and re-adds every source
// that can reach it, in source order so sums are deterministic.
func (r *Result) render(lvl *Level, region Rect) {
	cw := lvl.W + 1
	for j := region.Y0; j < region.Y1; j++ {
		row := r.sum[j*cw+region.X0 : j*cw+region.X1]
		for i := range row {
			row[i] = RGB{}
		}
	}
	// Starlight and overlay sources can only matter near the region.
	scan := region.expand(reach(r.opts) + 1)
	srcs := effectiveSources(lvl, r.opts, scan)
	sc := newScratch(lvl)
	for k := range srcs {
		s := &srcs[k]
		box := Rect{s.X - s.wr - 1, s.Y - s.wr - 1, s.X + s.wr + 3, s.Y + s.wr + 3}
		if !box.intersects(region) {
			continue
		}
		sc.addSource(r, s, region)
	}
}

// effectiveSources normalises explicit sources and appends derived starlight
// sources (tiles inside scan only). Order: explicit sources in slice order,
// then starlight in row-major tile order.
func effectiveSources(lvl *Level, opts Options, scan Rect) []effSource {
	out := make([]effSource, 0, len(lvl.Sources))
	for _, s := range lvl.Sources {
		if e, ok := normalize(lvl, s, opts); ok {
			out = append(out, e)
		}
	}
	for _, s := range lvl.starlightSourcesIn(scan) {
		if e, ok := normalize(lvl, s, opts); ok {
			out = append(out, e)
		}
	}
	return out
}

// normalize applies light-system conversion, clamps and validity checks.
func normalize(lvl *Level, s Source, opts Options) (effSource, bool) {
	if s.X < 0 || s.Y < 0 || s.X >= lvl.W || s.Y >= lvl.H {
		return effSource{}, false
	}
	if s.System == SystemNone || s.Range <= 0 || s.Power == 0 ||
		math.IsNaN(s.Range) || math.IsNaN(s.Power) || math.IsInf(s.Range, 0) || math.IsInf(s.Power, 0) {
		return effSource{}, false
	}
	rawRange := s.Range
	switch s.System {
	case SystemOverlay, SystemOverlayDirectional, SystemOverlayBeam:
		if !opts.IncludeOverlayLights {
			return effSource{}, false
		}
		// overlay_lighting.dm:401 range clamp, :428-429 alpha as strength.
		s.Range = clampF(math.Ceil(s.Range*2)/2, 1, 6)
		strength := math.Min(230, math.Abs(s.Power)*120+30) / 255
		if s.Power < 0 {
			strength = -strength
		}
		s.Power = strength
		s.Angle = 360
		if s.System == SystemOverlayBeam {
			s.Angle = 45 // narrow beam approximation
		}
		if s.System != SystemOverlay {
			dx, dy := castDirectional(lvl, s, rawRange)
			s.ShiftX += dx
			s.ShiftY += dy
		}
	}
	s.Range = math.Min(s.Range, opts.maxRange())
	s.ShiftX = clampF(s.ShiftX, -maxShift, maxShift)
	s.ShiftY = clampF(s.ShiftY, -maxShift, maxShift)
	visual := math.Max(math.Ceil(math.Abs(s.ShiftX)), math.Ceil(math.Abs(s.ShiftY))) // :449
	return effSource{Source: s, wr: int(math.Ceil(s.Range + visual))}, true
}

// castDirectional mirrors the cast distance of directional overlay lights
// (overlay_lighting.dm:416-420,511-525): the light centre moves forward until
// an opaque tile.
func castDirectional(lvl *Level, s Source, rawRange float64) (float64, float64) {
	cast := clampF(math.Round(rawRange*0.5), 1, 3)
	if s.System == SystemOverlayBeam {
		cast = math.Max(math.Round(rawRange*0.5), 1)
	}
	dist := int(cast)
	if dist > 2 && s.Dir&(DirNorth|DirSouth|DirEast|DirWest) != 0 && !isCardinal(s.Dir) {
		dist--
	}
	dx, dy := s.Dir.vec()
	if dx == 0 && dy == 0 {
		return 0, 0
	}
	x, y := s.X, s.Y
	steps := 0
	for i := 1; i <= dist; i++ {
		nx, ny := x+dx, y+dy
		if nx < 0 || ny < 0 || nx >= lvl.W || ny >= lvl.H || lvl.Occ.Opaque(nx, ny) {
			break
		}
		x, y = nx, ny
		steps = i
	}
	return float64(steps * dx), float64(steps * dy)
}

func isCardinal(d Dir) bool { return d == DirNorth || d == DirSouth || d == DirEast || d == DirWest }

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// scratch holds reusable buffers for visibility and corner de-duplication.
type scratch struct {
	lvl     *Level
	cw      int
	stamp   []uint32
	gen     uint32
	corners []int32
}

func newScratch(lvl *Level) *scratch {
	return &scratch{lvl: lvl, cw: lvl.W + 1, stamp: make([]uint32, (lvl.W+1)*(lvl.H+1))}
}

func (sc *scratch) addCorner(i, j int) {
	idx := j*sc.cw + i
	if sc.stamp[idx] != sc.gen {
		sc.stamp[idx] = sc.gen
		sc.corners = append(sc.corners, int32(idx))
	}
}

// addSource adds one source's contribution to corners inside region.
func (sc *scratch) addSource(r *Result, s *effSource, region Rect) {
	sc.gen++
	sc.corners = sc.corners[:0]
	sc.visibleCorners(s.X, s.Y, s.wr)

	ex := float64(s.X) + s.ShiftX
	ey := float64(s.Y) + s.ShiftY
	for _, ci := range sc.corners {
		i, j := int(ci)%sc.cw, int(ci)/sc.cw
		if i < region.X0 || i >= region.X1 || j < region.Y0 || j >= region.Y1 {
			continue
		}
		f := Falloff(float64(i)-0.5-ex, float64(j)-0.5-ey, 0, s.Height, s.Range, s.Dir, s.Angle)
		f *= s.Power // APPLY_CORNER, lighting_source.dm:205
		if f == 0 {
			continue
		}
		c := &r.sum[ci]
		c.R += f * s.Color.R // :210-212
		c.G += f * s.Color.G
		c.B += f * s.Color.B
	}
}

// visibleCorners collects the corners of every transparent tile in the
// source's square view (lighting_source.dm:469-479). Opaque tiles are skipped
// themselves; their corners are reached only through transparent neighbours.
func (sc *scratch) visibleCorners(sx, sy, radius int) {
	l := sc.lvl
	mark := func(x, y int) {
		if l.Occ.Opaque(x, y) {
			return
		}
		sc.addCorner(x, y)
		sc.addCorner(x+1, y)
		sc.addCorner(x, y+1)
		sc.addCorner(x+1, y+1)
	}
	mark(sx, sy)
	for oct := 0; oct < 8; oct++ {
		sc.cast(sx, sy, 1, 1.0, 0.0, radius, octXX[oct], octXY[oct], octYX[oct], octYY[oct], mark)
	}
}

var (
	octXX = [8]int{1, 0, 0, -1, -1, 0, 0, 1}
	octXY = [8]int{0, 1, -1, 0, 0, -1, 1, 0}
	octYX = [8]int{0, 1, 1, 0, 0, -1, -1, 0}
	octYY = [8]int{1, 0, 0, 1, -1, 0, 0, -1}
)

// cast is recursive shadowcasting over one octant. Tiles outside the level
// block sight and are never marked. The view is a square of the given radius,
// like BYOND view().
func (sc *scratch) cast(cx, cy, row int, start, end float64, radius, xx, xy, yx, yy int, mark func(x, y int)) {
	if start < end {
		return
	}
	l := sc.lvl
	newStart := 0.0
	for j := row; j <= radius; j++ {
		dx, dy := -j-1, -j
		blocked := false
		for dx <= 0 {
			dx++
			x := cx + dx*xx + dy*xy
			y := cy + dx*yx + dy*yy
			lSlope := (float64(dx) - 0.5) / (float64(dy) + 0.5)
			rSlope := (float64(dx) + 0.5) / (float64(dy) - 0.5)
			if start < rSlope {
				continue
			} else if end > lSlope {
				break
			}
			inside := x >= 0 && y >= 0 && x < l.W && y < l.H
			if inside {
				mark(x, y)
			}
			opaque := !inside || l.Occ.Opaque(x, y)
			if blocked {
				if opaque {
					newStart = rSlope
					continue
				}
				blocked = false
				start = newStart
			} else if opaque && j < radius {
				blocked = true
				sc.cast(cx, cy, j+1, start, lSlope, radius, xx, xy, yx, yy, mark)
				newStart = rSlope
			}
		}
		if blocked {
			break
		}
	}
}

// Corner returns the normalised, 1/64-rounded light at corner (i, j) before
// ambient and cutoff, never negative. It is the tg cache_r/g/b value
// (lighting_corner.dm:108-135).
func (r *Result) Corner(i, j int) RGB {
	return normaliseCorner(r.sum[j*(r.W+1)+i])
}

func normaliseCorner(c RGB) RGB {
	largest := math.Max(c.R, math.Max(c.G, c.B))
	factor := 1.0
	if largest > 1 { // :115-116
		factor = 1 / largest
	}
	return RGB{
		R: math.Max(0, roundLight(c.R*factor)), // :130-132
		G: math.Max(0, roundLight(c.G*factor)),
		B: math.Max(0, roundLight(c.B*factor)),
	}
}

// Tile returns the final displayed brightness at the four corners of tile
// (x, y) in lighting_object.dm:89-92 order: SW, SE, NW, NE. Each value is
// corner light (zero for tiles without a lighting object) plus the tile's
// ambient, clamped to 0..1, then floored at Options.Cutoff.
func (r *Result) Tile(lvl *Level, x, y int) [4]RGB {
	cw := r.W + 1
	idx := [4]int{y*cw + x, y*cw + x + 1, (y+1)*cw + x, (y+1)*cw + x + 1}
	t := y*lvl.W + x
	var amb RGB
	if lvl.Ambient != nil {
		amb = lvl.Ambient[t]
	}
	unlit := lvl.Unlit != nil && lvl.Unlit[t]
	var out [4]RGB
	for k, ci := range idx {
		var c RGB
		if !unlit {
			c = normaliseCorner(r.sum[ci])
		}
		out[k] = RGB{
			R: math.Max(clamp01(c.R+amb.R), r.opts.Cutoff),
			G: math.Max(clamp01(c.G+amb.G), r.opts.Cutoff),
			B: math.Max(clamp01(c.B+amb.B), r.opts.Cutoff),
		}
	}
	return out
}
