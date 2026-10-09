package disposals

import (
	"reflect"
	"strings"
	"testing"

	"sdmm/internal/util"
)

func pt(x, y int) util.Point { return util.Point{X: x, Y: y, Z: 1} }

// initialize_dirs as Meridian-Rift declares them.
const (
	segmentInit  = dispDirFlip
	junctionInit = dispDirRight | dispDirFlip
	trunkInit    = 0
)

func TestDPDirMatchesInitialize(t *testing.T) {
	cases := []struct{ dir, init, want int }{
		{North, segmentInit, North | South},
		{North | East, segmentInit, North | East}, // bent
		{North, junctionInit, North | East | South},
		{North, dispDirLeft | dispDirFlip, North | West | South}, // junction/flip
		{North, dispDirLeft | dispDirRight, North | West | East}, // yjunction
		{East, trunkInit, East},
		{East, dispDirNone, 0},          // broken
		{0, segmentInit, North | South}, // unedited dir = NONE faces south
		{0, junctionInit, South | West | North},
	}
	for _, c := range cases {
		if got := DPDir(c.dir, c.init); got != c.want {
			t.Errorf("DPDir(%d, %d) = %d, want %d", c.dir, c.init, got, c.want)
		}
	}
}

func TestPipeForRoundTripsThroughDPDir(t *testing.T) {
	inits := map[string]int{Segment: segmentInit, Junction: junctionInit, Trunk: trunkInit}
	for mask := 1; mask < 16; mask++ {
		for _, machine := range []bool{false, true} {
			path, dir, err := PipeFor(mask, machine)
			if mask == 15 {
				if err == nil {
					t.Fatal("four-way pipe planned")
				}
				continue
			}
			if machine && countBits(mask) > 1 {
				continue
			}
			if err != nil {
				t.Fatalf("mask %d: %v", mask, err)
			}
			want := mask
			if countBits(mask) == 1 && !machine {
				want = mask | Reverse(mask) // a free end is a straight pipe, open beyond
			}
			if got := DPDir(dir, inits[path]); got != want {
				t.Fatalf("mask %d -> %s dir %d connects %d", mask, path, dir, got)
			}
		}
	}
}

func TestExtendWalksFourConnectedAndBacktracks(t *testing.T) {
	route := Extend(nil, pt(1, 1))
	route = Extend(route, pt(3, 2)) // diagonal jump: horizontal first
	if !reflect.DeepEqual(route, []util.Point{pt(1, 1), pt(2, 1), pt(3, 1), pt(3, 2)}) {
		t.Fatalf("route = %v", route)
	}
	route = Extend(route, pt(3, 1)) // back one step
	if !reflect.DeepEqual(route, []util.Point{pt(1, 1), pt(2, 1), pt(3, 1)}) {
		t.Fatalf("backtrack = %v", route)
	}
}

func TestPlanBendsJoinsAndTrunks(t *testing.T) {
	tiles := map[util.Point][]Atom{
		pt(1, 1): {{Path: "/obj/machinery/disposal/bin"}},
		// An existing straight north-south pipe at (3,1)...
		pt(3, 1): {{Path: Segment, Dir: North, InitializeDirs: segmentInit}},
	}
	read := func(p util.Point) []Atom { return tiles[p] }
	// Bin at (1,1) east to (3,1), which already runs north-south: a junction.
	edits, err := Plan([]util.Point{pt(1, 1), pt(2, 1), pt(3, 1)}, read)
	if err != nil {
		t.Fatal(err)
	}
	want := []Edit{
		{pt(1, 1), Trunk, East},
		{pt(2, 1), Segment, East},
		{pt(3, 1), Junction, South}, // N|S + W: dominant south, right of south is west
	}
	if !reflect.DeepEqual(edits, want) {
		t.Fatalf("edits = %+v", edits)
	}
	// A corner.
	edits, _ = Plan([]util.Point{pt(5, 5), pt(6, 5), pt(6, 6)}, func(util.Point) []Atom { return nil })
	if edits[1].Path != Segment || edits[1].Dir != West|North {
		t.Fatalf("corner = %+v", edits[1])
	}
	// Through a bin is refused, and nothing is planned.
	if _, err := Plan([]util.Point{pt(0, 1), pt(1, 1), pt(2, 1)}, read); err == nil || !strings.Contains(err.Error(), "only end") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckFindsOpenEndsAndOrphanTrunks(t *testing.T) {
	tiles := map[util.Point][]Atom{
		pt(1, 1): {{Path: Trunk, Dir: East}, {Path: "/obj/machinery/disposal/bin"}},
		pt(2, 1): {{Path: Segment, Dir: East, InitializeDirs: segmentInit}}, // open east
		pt(5, 5): {{Path: Trunk, Dir: North}},                               // no bin, open north
	}
	read := func(p util.Point) ([]Atom, bool) { return tiles[p], p.X >= 1 && p.Y >= 1 && p.X <= 9 && p.Y <= 9 }
	problems := Check([]util.Point{pt(1, 1), pt(2, 1), pt(5, 5)}, read)
	var msgs []string
	for _, p := range problems {
		msgs = append(msgs, p.Message)
	}
	if len(problems) != 3 || problems[0].Coord != pt(2, 1) || !strings.Contains(msgs[0], "east") || !strings.Contains(msgs[1], "no bin") {
		t.Fatalf("problems = %v", msgs)
	}
}
