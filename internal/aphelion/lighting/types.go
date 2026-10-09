// Package lighting is a pure, editor-only approximation of the tg-derived SS13
// lighting model (code/modules/lighting in Meridian-Rift). It never touches map
// data, collaboration protocol, or hashes: it reads atom variables and an
// opacity grid and produces per-tile-corner RGB luminance for a preview overlay.
//
// Citations of the form "lighting_source.dm:276" refer to files under
// code/modules/lighting (or the named path) in the Meridian-Rift tree.
package lighting

import "math"

// RGB is a linear colour triple. Light colours are in 0..1 per channel;
// accumulated corner sums may exceed 1 before normalisation.
type RGB struct{ R, G, B float64 }

// Dir is a BYOND direction bitmask (NORTH 1, SOUTH 2, EAST 4, WEST 8). North is
// +Y in map coordinates.
type Dir uint8

// BYOND direction flags.
const (
	DirNone  Dir = 0
	DirNorth Dir = 1
	DirSouth Dir = 2
	DirEast  Dir = 4
	DirWest  Dir = 8
)

// Angle mirrors dir2angle() (type2type.dm:81): north-zero, clockwise degrees.
// Unknown directions return 0, matching BYOND's null-as-zero arithmetic.
func (d Dir) Angle() float64 {
	switch d {
	case DirNorth:
		return 0
	case DirSouth:
		return 180
	case DirEast:
		return 90
	case DirWest:
		return 270
	case DirNorth | DirEast:
		return 45
	case DirSouth | DirEast:
		return 135
	case DirNorth | DirWest:
		return 315
	case DirSouth | DirWest:
		return 225
	}
	return 0
}

// vec returns the unit step (dx, dy) of the direction, north being +Y.
func (d Dir) vec() (int, int) {
	dx, dy := 0, 0
	if d&DirNorth != 0 {
		dy++
	}
	if d&DirSouth != 0 {
		dy--
	}
	if d&DirEast != 0 {
		dx++
	}
	if d&DirWest != 0 {
		dx--
	}
	return dx, dy
}

// Reverse mirrors REVERSE_DIR for the eight BYOND directions.
func (d Dir) Reverse() Dir {
	var r Dir
	if d&DirNorth != 0 {
		r |= DirSouth
	}
	if d&DirSouth != 0 {
		r |= DirNorth
	}
	if d&DirEast != 0 {
		r |= DirWest
	}
	if d&DirWest != 0 {
		r |= DirEast
	}
	return r
}

// System is the atom light_system value (__DEFINES/lighting.dm:1-10).
type System uint8

// Light systems.
const (
	SystemNone               System = 0 // NO_LIGHT_SUPPORT
	SystemComplex            System = 1 // COMPLEX_LIGHT
	SystemOverlay            System = 2 // OVERLAY_LIGHT
	SystemOverlayDirectional System = 3 // OVERLAY_LIGHT_DIRECTIONAL
	SystemOverlayBeam        System = 4 // OVERLAY_LIGHT_BEAM
)

// Source is one light emitter on a level. Coordinates are 0-based tile
// indices; X grows east and Y grows north.
type Source struct {
	X, Y int
	// ShiftX/ShiftY move the emitter off the tile centre, in tiles. This is the
	// negation of the tg sheet offset_x/offset_y (lighting_source.dm:250,
	// lighting_atom.dm:218-230).
	ShiftX, ShiftY float64
	Range, Power   float64
	Color          RGB // per-channel multipliers, 0..1 (PARSE_LIGHT_COLOR)
	Dir            Dir
	// Angle is the cone width in degrees; <=0 or >=360 means omnidirectional.
	Angle  float64
	Height float64
	System System
}

// OcclusionGrid reports tiles that block light (opaque turfs and opaque
// objects). Coordinates outside Width/Height are never queried.
type OcclusionGrid interface {
	Width() int
	Height() int
	Opaque(x, y int) bool
}

// BoolGrid is a simple OcclusionGrid backed by a slice.
type BoolGrid struct {
	W, H int
	Bits []bool
}

// NewBoolGrid returns an all-transparent grid.
func NewBoolGrid(w, h int) *BoolGrid { return &BoolGrid{W: w, H: h, Bits: make([]bool, w*h)} }

// Width implements OcclusionGrid.
func (g *BoolGrid) Width() int { return g.W }

// Height implements OcclusionGrid.
func (g *BoolGrid) Height() int { return g.H }

// Opaque implements OcclusionGrid.
func (g *BoolGrid) Opaque(x, y int) bool { return g.Bits[y*g.W+x] }

// Set marks a tile opaque or transparent.
func (g *BoolGrid) Set(x, y int, opaque bool) { g.Bits[y*g.W+x] = opaque }

// Starlight configures space starlight (space.dm:1-19,104-122).
type Starlight struct {
	Enabled      bool
	Range, Power float64
	Color        RGB
}

// DefaultStarlight mirrors /turf/open/space defaults (space.dm:64-67) with
// COLOR_STARLIGHT #8589fa (__DEFINES/colors.dm:247).
func DefaultStarlight() Starlight {
	return Starlight{Enabled: true, Range: 2, Power: 1, Color: RGB{0x85 / 255.0, 0x89 / 255.0, 0xfa / 255.0}}
}

// starlightHeight is LIGHTING_HEIGHT_SPACE (__DEFINES/lighting.dm:29).
const starlightHeight = -0.5

// Level is everything the model needs for one z-level. All per-tile slices are
// row-major, y*W+x, and optional (nil means all-false / all-zero).
type Level struct {
	W, H int
	Occ  OcclusionGrid
	// Ambient is the additive base lighting per tile (see ResolveAmbient).
	Ambient []RGB
	// Unlit marks tiles without a lighting object (area static_lighting off).
	Unlit []bool
	// Space marks /turf/open/space tiles, which derive starlight sources.
	Space     []bool
	Starlight Starlight
	Sources   []Source
}

// NewLevel returns an empty level with a transparent BoolGrid and starlight off.
func NewLevel(w, h int) *Level {
	return &Level{W: w, H: h, Occ: NewBoolGrid(w, h)}
}

// SetOpaque edits the occlusion grid when it is a *BoolGrid.
func (l *Level) SetOpaque(x, y int, opaque bool) {
	if g, ok := l.Occ.(*BoolGrid); ok {
		g.Set(x, y, opaque)
	}
}

// Options tunes a computation. Preview-only knobs such as darkness strength
// belong to the UI layer, not here.
type Options struct {
	// MaxRange bounds light_range (and so the cost of a source). Sources beyond
	// it are clamped, which is a documented deviation.
	MaxRange float64
	// Cutoff is the lighting plane floor in 0..1 (render_plate.dm:296: 10/100).
	Cutoff float64
	// IncludeOverlayLights approximates OVERLAY_LIGHT* sources.
	IncludeOverlayLights bool
}

// DefaultOptions returns the recommended defaults.
func DefaultOptions() Options {
	return Options{MaxRange: 16, Cutoff: 0.10, IncludeOverlayLights: true}
}

func (o Options) maxRange() float64 {
	if o.MaxRange <= 0 || math.IsNaN(o.MaxRange) {
		return DefaultOptions().MaxRange
	}
	return o.MaxRange
}

// Rect is a half-open tile rectangle [X0,X1) x [Y0,Y1). The zero value is empty.
type Rect struct{ X0, Y0, X1, Y1 int }

// RectAround returns the one-tile rectangle at x, y.
func RectAround(x, y int) Rect { return Rect{x, y, x + 1, y + 1} }

// Empty reports whether the rectangle contains no tiles.
func (r Rect) Empty() bool { return r.X1 <= r.X0 || r.Y1 <= r.Y0 }

// Union returns the bounding rectangle of r and o.
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	return Rect{min(r.X0, o.X0), min(r.Y0, o.Y0), max(r.X1, o.X1), max(r.Y1, o.Y1)}
}

func (r Rect) expand(n int) Rect { return Rect{r.X0 - n, r.Y0 - n, r.X1 + n, r.Y1 + n} }

func (r Rect) intersects(o Rect) bool {
	return !r.Empty() && !o.Empty() && r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}
