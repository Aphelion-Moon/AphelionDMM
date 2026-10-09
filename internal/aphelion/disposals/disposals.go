// Package disposals plans disposal pipe runs and checks disposal networks.
//
// The connection rules port Meridian-Rift's
// code/modules/recycling/disposal/pipe.dm: a pipe's connections (dpdir) are
// its dir plus the sides its type's initialize_dirs adds (DISP_DIR_LEFT,
// DISP_DIR_RIGHT, DISP_DIR_FLIP, or none for DISP_DIR_NONE); a diagonal dir is
// a bent segment that already names both sides. Trunks connect only toward dir
// and sit under a disposal bin, chute or outlet on their own tile.
package disposals

import (
	"fmt"
	"strings"

	"sdmm/internal/util"
)

const (
	North = 1
	South = 2
	East  = 4
	West  = 8

	dispDirLeft  = 1
	dispDirRight = 2
	dispDirFlip  = 4
	dispDirNone  = 8

	PipeRoot     = "/obj/structure/disposalpipe"
	Segment      = PipeRoot + "/segment"
	Junction     = PipeRoot + "/junction"
	JunctionFlip = PipeRoot + "/junction/flip"
	Trunk        = PipeRoot + "/trunk"
)

var cardinals = [4]int{North, South, East, West}

// Reverse is REVERSE_DIR for cardinal combinations.
func Reverse(dir int) int {
	out := 0
	for _, pair := range [][2]int{{North, South}, {South, North}, {East, West}, {West, East}} {
		if dir&pair[0] != 0 {
			out |= pair[1]
		}
	}
	return out
}

// turnLeft is turn(dir, 90); turnRight is turn(dir, -90).
func turnLeft(dir int) int {
	switch dir {
	case North:
		return West
	case West:
		return South
	case South:
		return East
	case East:
		return North
	}
	return 0
}

func turnRight(dir int) int { return Reverse(turnLeft(dir)) }

func isCardinal(dir int) bool { return dir == North || dir == South || dir == East || dir == West }

// DPDir is the connection mask a pipe gets at Initialize.
func DPDir(dir, initializeDirs int) int {
	if dir == 0 {
		// disposalpipe declares dir = NONE, but an atom's dir is never 0 in
		// BYOND: unedited pipes face SOUTH (MiniStation's unedited vertical
		// segments only connect that way).
		dir = South
	}
	if !isCardinal(dir) {
		return dir // bent segment
	}
	if initializeDirs == dispDirNone {
		return 0
	}
	mask := dir
	if initializeDirs&dispDirLeft != 0 {
		mask |= turnLeft(dir)
	}
	if initializeDirs&dispDirRight != 0 {
		mask |= turnRight(dir)
	}
	if initializeDirs&dispDirFlip != 0 {
		mask |= Reverse(dir)
	}
	return mask
}

// Atom is what the planner needs to know about one atom on a tile.
type Atom struct {
	Path string
	Dir  int
	// InitializeDirs is the type's initialize_dirs; only read for pipes.
	InitializeDirs int
}

// IsPipe reports a disposal pipe.
func IsPipe(path string) bool { return isType(path, PipeRoot) }

// IsMachine reports what a trunk links to on its own tile.
func IsMachine(path string) bool {
	return isType(path, "/obj/machinery/disposal") || isType(path, "/obj/structure/disposaloutlet")
}

func isType(path, root string) bool { return path == root || strings.HasPrefix(path, root+"/") }

// Mask is the union of every pipe connection on a tile.
func Mask(tile []Atom) int {
	mask := 0
	for _, a := range tile {
		if IsPipe(a.Path) {
			mask |= DPDir(a.Dir, a.InitializeDirs)
		}
	}
	return mask
}

func hasMachine(tile []Atom) bool {
	for _, a := range tile {
		if IsMachine(a.Path) {
			return true
		}
	}
	return false
}

// PipeFor chooses the pipe type and dir that connect exactly mask. A single
// connection on a tile with a bin, chute or outlet is a trunk; on an empty
// tile it is a straight dead end that Check will report.
func PipeFor(mask int, machine bool) (path string, dir int, err error) {
	switch bits := countBits(mask); {
	case bits == 1 && machine:
		return Trunk, mask, nil
	case bits == 1:
		return Segment, mask, nil
	case bits == 2 && (mask == North|South || mask == East|West):
		if mask == North|South {
			return Segment, North, nil
		}
		return Segment, East, nil
	case bits == 2:
		return Segment, mask, nil // bend: the diagonal dir
	case bits == 3:
		for _, d := range cardinals {
			if mask&d == 0 || mask&Reverse(d) == 0 {
				continue
			}
			if side := mask &^ (d | Reverse(d)); side == turnRight(d) {
				return Junction, d, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no disposal pipe connects %s", DescribeMask(mask))
}

func countBits(mask int) int {
	n := 0
	for _, d := range cardinals {
		if mask&d != 0 {
			n++
		}
	}
	return n
}

// DescribeMask names the directions in a mask.
func DescribeMask(mask int) string {
	var parts []string
	for _, pair := range []struct {
		d    int
		name string
	}{{North, "north"}, {South, "south"}, {East, "east"}, {West, "west"}} {
		if mask&pair.d != 0 {
			parts = append(parts, pair.name)
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// Step is the direction from a to an adjacent tile b, or 0.
func Step(a, b util.Point) int {
	switch {
	case b.X == a.X && b.Y == a.Y+1:
		return North
	case b.X == a.X && b.Y == a.Y-1:
		return South
	case b.X == a.X+1 && b.Y == a.Y:
		return East
	case b.X == a.X-1 && b.Y == a.Y:
		return West
	}
	return 0
}

// Extend appends to to route as a 4-connected walk, stepping horizontally
// first, and retracts the route when the walk doubles back on itself.
func Extend(route []util.Point, to util.Point) []util.Point {
	if len(route) == 0 {
		return []util.Point{to}
	}
	for cur := route[len(route)-1]; cur != to; cur = route[len(route)-1] {
		next := cur
		switch {
		case to.X > cur.X:
			next.X++
		case to.X < cur.X:
			next.X--
		case to.Y > cur.Y:
			next.Y++
		default:
			next.Y--
		}
		if len(route) >= 2 && route[len(route)-2] == next {
			route = route[:len(route)-1] // walking back undoes the last step
			continue
		}
		route = append(route, next)
	}
	return route
}

// Edit replaces a tile's disposal pipes with one pipe.
type Edit struct {
	Coord util.Point
	Path  string
	Dir   int
}

// Plan lays a pipe along route. Each tile keeps the connections its existing
// pipes already have and gains the run's, so starting or crossing on an
// existing pipe makes a junction. read returns a tile's atoms. It fails without
// planning anything when a tile would need a four-way pipe, which disposals
// lack, or when the run passes through a bin.
func Plan(route []util.Point, read func(util.Point) []Atom) ([]Edit, error) {
	if len(route) < 2 {
		return nil, fmt.Errorf("drag across at least two tiles")
	}
	edits := make([]Edit, 0, len(route))
	seen := map[util.Point]bool{}
	for i, p := range route {
		if seen[p] {
			return nil, fmt.Errorf("the run crosses itself at %d,%d", p.X, p.Y)
		}
		seen[p] = true
		tile := read(p)
		mask := Mask(tile)
		if i > 0 {
			mask |= Step(p, route[i-1])
		}
		if i < len(route)-1 {
			mask |= Step(p, route[i+1])
		}
		machine := hasMachine(tile)
		if machine && countBits(mask) > 1 {
			return nil, fmt.Errorf("a disposal bin or outlet at %d,%d can only end a run", p.X, p.Y)
		}
		path, dir, err := PipeFor(mask, machine)
		if err != nil {
			return nil, fmt.Errorf("at %d,%d: %w", p.X, p.Y, err)
		}
		edits = append(edits, Edit{Coord: p, Path: path, Dir: dir})
	}
	return edits, nil
}

// Problem is one broken disposal connection.
type Problem struct {
	Coord   util.Point
	Message string
}

// Check reports every pipe connection that leads nowhere: no pipe on the next
// tile connects back, or a trunk without a bin, chute or outlet. Multi-z trunks
// are skipped; their partner is on another level.
func Check(points []util.Point, read func(util.Point) ([]Atom, bool)) []Problem {
	var out []Problem
	for _, p := range points {
		tile, _ := read(p)
		for _, a := range tile {
			if !IsPipe(a.Path) || isType(a.Path, Trunk+"/multiz") || isType(a.Path, PipeRoot+"/broken") {
				continue
			}
			mask := DPDir(a.Dir, a.InitializeDirs)
			if isType(a.Path, Trunk) && !hasMachine(tile) {
				out = append(out, Problem{p, "Disposal trunk has no bin, chute or outlet on its tile"})
			}
			for _, d := range cardinals {
				if mask&d == 0 {
					continue
				}
				dx, dy := 0, 0
				switch d {
				case North:
					dy = 1
				case South:
					dy = -1
				case East:
					dx = 1
				case West:
					dx = -1
				}
				neighbor, ok := read(util.Point{X: p.X + dx, Y: p.Y + dy, Z: p.Z})
				if !ok || Mask(neighbor)&Reverse(d) == 0 {
					out = append(out, Problem{p, "Disposal pipe " + a.Path + " is open to the " + DescribeMask(d)})
				}
			}
		}
	}
	return out
}
