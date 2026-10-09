// Package docking computes the read-only footprint of /obj/docking_port
// instances for the map overlay. It is pure geometry and validation; it never
// reads or changes map data.
//
// Semantics are copied from the game code (Meridian-Rift,
// code/modules/shuttle/shuttle.dm):
//   - shuttle.dm:18-25: width is the size perpendicular to dir, height the size
//     parallel to dir, dwidth/dheight the port's offset inside that rectangle.
//   - shuttle.dm:81-108 (return_coords): the rectangle corners are
//     (x - dw*cos + dh*sin, y - dw*sin - dh*cos) and
//     (x + (w-dw-1)*cos - (h-dh-1)*sin, y + (w-dw-1)*sin + (h-dh-1)*cos) with
//     NORTH=(cos 1, sin 0), WEST=(0, 1), SOUTH=(-1, 0), EAST=(0, -1). Any other
//     dir (diagonals) falls through the switch and behaves as NORTH.
//   - shuttle.dm:119-143 (return_ordered_turfs) iterates the same rectangle.
package docking

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// DM direction constants (dir values).
const (
	DirNorth = 1
	DirSouth = 2
	DirEast  = 4
	DirWest  = 8
)

// SpacePathPrefixes lists the turf paths treated as "space" for the stationary
// port non-space warning. A turf is space when its path equals a prefix or is
// below it ("/turf/open/space" matches "/turf/open/space/basic" but not
// "/turf/open/spacefake").
var SpacePathPrefixes = []string{"/turf/open/space"}

// Path roots used to classify ports.
const (
	PortRoot       = "/obj/docking_port"
	StationaryRoot = PortRoot + "/stationary"
	MobileRoot     = PortRoot + "/mobile"
)

var enabled atomic.Bool

// Enabled reports whether the overlay is shown (View menu toggle).
func Enabled() bool { return enabled.Load() }

// SetEnabled sets the overlay visibility.
func SetEnabled(v bool) { enabled.Store(v) }

// Spec holds the numeric docking-port variables.
type Spec struct {
	Width, Height, DWidth, DHeight int
	Dir                            int
}

// Rect is an inclusive tile rectangle with Min <= Max.
type Rect struct{ MinX, MinY, MaxX, MaxY int }

func (r Rect) Width() int  { return r.MaxX - r.MinX + 1 }
func (r Rect) Height() int { return r.MaxY - r.MinY + 1 }

func (r Rect) intersects(o Rect) bool {
	return r.MinX <= o.MaxX && o.MinX <= r.MaxX && r.MinY <= o.MaxY && o.MinY <= r.MaxY
}

var dirNames = map[string]int{"NORTH": DirNorth, "SOUTH": DirSouth, "EAST": DirEast, "WEST": DirWest}

func parseInt(get func(string) (string, bool), name string, def int, required bool) (int, bool) {
	value, ok := get(name)
	if !ok {
		return def, !required
	}
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// ParseSpec reads width/height/dwidth/dheight/dir through get, which should
// return inherited values. ok is false when any value is a non-numeric
// expression or the size is not positive.
func ParseSpec(get func(string) (string, bool)) (Spec, bool) {
	var s Spec
	var ok bool
	if s.Width, ok = parseInt(get, "width", 0, true); !ok {
		return Spec{}, false
	}
	if s.Height, ok = parseInt(get, "height", 0, true); !ok {
		return Spec{}, false
	}
	if s.DWidth, ok = parseInt(get, "dwidth", 0, false); !ok {
		return Spec{}, false
	}
	if s.DHeight, ok = parseInt(get, "dheight", 0, false); !ok {
		return Spec{}, false
	}
	s.Dir = DirNorth // shuttle.dm:10
	if value, has := get("dir"); has {
		value = strings.TrimSpace(value)
		if d, named := dirNames[value]; named {
			s.Dir = d
		} else if n, err := strconv.ParseInt(value, 10, 32); err == nil {
			s.Dir = int(n)
		} else {
			return Spec{}, false
		}
	}
	if s.Width <= 0 || s.Height <= 0 {
		return Spec{}, false
	}
	return s, true
}

// Bounds implements return_coords (shuttle.dm:81-108) for a port at (x, y).
func Bounds(x, y, dir int, s Spec) Rect {
	cos, sin := 1, 0
	switch dir {
	case DirWest:
		cos, sin = 0, 1
	case DirSouth:
		cos, sin = -1, 0
	case DirEast:
		cos, sin = 0, -1
	}
	x0 := x + (-s.DWidth * cos) - (-s.DHeight * sin)
	y0 := y + (-s.DWidth * sin) + (-s.DHeight * cos)
	x1 := x + (-s.DWidth+s.Width-1)*cos - (-s.DHeight+s.Height-1)*sin
	y1 := y + (-s.DWidth+s.Width-1)*sin + (-s.DHeight+s.Height-1)*cos
	r := Rect{x0, y0, x1, y1}
	if r.MinX > r.MaxX {
		r.MinX, r.MaxX = r.MaxX, r.MinX
	}
	if r.MinY > r.MaxY {
		r.MinY, r.MaxY = r.MaxY, r.MinY
	}
	return r
}

// Kind classifies a port by path.
type Kind uint8

const (
	KindOther Kind = iota
	KindStationary
	KindMobile
)

// Warning is a bit set of footprint problems.
type Warning uint8

const (
	WarnEdge Warning = 1 << iota
	WarnOverlap
	WarnNonSpace
)

// Input describes one docking-port instance on one level.
type Input struct {
	Path string
	X, Y int
	// Var returns an instance variable, including values inherited from the
	// environment.
	Var func(string) (string, bool)
}

// Level describes the level the ports sit on.
type Level struct {
	MaxX, MaxY int
	// TurfPaths returns the turf paths at a tile; nil means no information.
	TurfPaths func(x, y int) []string
}

// Entry is the analysis of one input, in input order.
type Entry struct {
	Kind     Kind
	X, Y     int
	Label    string
	Known    bool // false: size could not be determined, Rect is invalid
	Rect     Rect
	Warnings Warning
	NonSpace int // count of non-space footprint tiles (stationary only)
}

func underPath(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func classify(path string) Kind {
	switch {
	case underPath(path, StationaryRoot):
		return KindStationary
	case underPath(path, MobileRoot):
		return KindMobile
	}
	return KindOther
}

// IsPort reports whether a prefab path is /obj/docking_port or a subtype.
func IsPort(path string) bool { return underPath(path, PortRoot) }

// IsSpace reports whether a turf path counts as space.
func IsSpace(path string) bool {
	for _, prefix := range SpacePathPrefixes {
		if underPath(path, prefix) {
			return true
		}
	}
	return false
}

func text(get func(string) (string, bool), name string) string {
	value, ok := get(name)
	if !ok || value == "null" {
		return ""
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return ""
}

func label(get func(string) (string, bool)) string {
	name, id := text(get, "name"), text(get, "shuttle_id")
	switch {
	case name != "" && id != "" && name != id:
		return name + " [" + id + "]"
	case name != "":
		return name
	}
	return id
}

// Analyze computes footprints and warnings for ports on one level.
func Analyze(inputs []Input, level Level) []Entry {
	out := make([]Entry, len(inputs))
	for i, in := range inputs {
		e := Entry{Kind: classify(in.Path), X: in.X, Y: in.Y}
		get := in.Var
		if get == nil {
			get = func(string) (string, bool) { return "", false }
		}
		e.Label = label(get)
		if spec, ok := ParseSpec(get); ok {
			e.Known = true
			e.Rect = Bounds(in.X, in.Y, spec.Dir, spec)
			if e.Rect.MinX < 1 || e.Rect.MinY < 1 || e.Rect.MaxX > level.MaxX || e.Rect.MaxY > level.MaxY {
				e.Warnings |= WarnEdge
			}
			if e.Kind == KindStationary && level.TurfPaths != nil {
				e.NonSpace = countNonSpace(e.Rect, level)
				if e.NonSpace > 0 {
					e.Warnings |= WarnNonSpace
				}
			}
		}
		out[i] = e
	}
	for i := range out {
		if !out[i].Known {
			continue
		}
		for j := i + 1; j < len(out); j++ {
			if out[j].Known && out[i].Rect.intersects(out[j].Rect) && !docked(out[i], out[j]) {
				out[i].Warnings |= WarnOverlap
				out[j].Warnings |= WarnOverlap
			}
		}
	}
	return out
}

// docked reports a mobile port resting on a stationary port's turf, which is
// the normal mapped state of a shuttle at its home dock rather than a clash.
func docked(a, b Entry) bool {
	if a.X != b.X || a.Y != b.Y {
		return false
	}
	return a.Kind == KindMobile && b.Kind == KindStationary || a.Kind == KindStationary && b.Kind == KindMobile
}

func countNonSpace(r Rect, level Level) int {
	count := 0
	for y := r.MinY; y <= r.MaxY; y++ {
		for x := r.MinX; x <= r.MaxX; x++ {
			if x < 1 || y < 1 || x > level.MaxX || y > level.MaxY {
				continue
			}
			for _, turf := range level.TurfPaths(x, y) {
				if !IsSpace(turf) {
					count++
					break
				}
			}
		}
	}
	return count
}
