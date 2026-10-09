package docking

import "testing"

func vars(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { v, ok := m[name]; return v, ok }
}

func TestBoundsAllDirections(t *testing.T) {
	// Port at (10,10), width 5, height 3, dwidth 1, dheight 2.
	spec := Spec{Width: 5, Height: 3, DWidth: 1, DHeight: 2}
	cases := []struct {
		dir  int
		want Rect
	}{
		// NORTH: cos=1 sin=0 -> x:[10-1, 10+3], y:[10-2, 10+0]
		{DirNorth, Rect{9, 8, 13, 10}},
		// SOUTH: cos=-1 sin=0 -> x0=11 x1=7 ; y0=12 y1=10
		{DirSouth, Rect{7, 10, 11, 12}},
		// WEST: cos=0 sin=1 -> x0=10+2=12 x1=10-0=10 ; y0=10-1=9 y1=10+3=13
		{DirWest, Rect{10, 9, 12, 13}},
		// EAST: cos=0 sin=-1 -> x0=10-2=8 x1=10+0=10 ; y0=10+1=11 y1=10-3=7
		{DirEast, Rect{8, 7, 10, 11}},
	}
	for _, c := range cases {
		got := Bounds(10, 10, c.dir, spec)
		if got != c.want {
			t.Errorf("dir %d: got %+v want %+v", c.dir, got, c.want)
		}
		if got.Width()*got.Height() != 15 {
			t.Errorf("dir %d: area %d", c.dir, got.Width()*got.Height())
		}
	}
}

func TestBoundsDiagonalFallsBackToNorth(t *testing.T) {
	spec := Spec{Width: 2, Height: 2}
	if Bounds(5, 5, 5, spec) != Bounds(5, 5, DirNorth, spec) {
		t.Fatal("diagonal dir must behave as NORTH like the DM switch default")
	}
}

func TestParseSpec(t *testing.T) {
	spec, ok := ParseSpec(vars(map[string]string{"width": "5", "height": "3", "dwidth": "1", "dheight": "2", "dir": "WEST"}))
	if !ok || spec.Dir != DirWest || spec.Width != 5 || spec.DHeight != 2 {
		t.Fatalf("spec %+v ok=%v", spec, ok)
	}
	spec, ok = ParseSpec(vars(map[string]string{"width": "5", "height": "3", "dir": "4"}))
	if !ok || spec.Dir != DirEast {
		t.Fatalf("numeric dir: %+v %v", spec, ok)
	}
	spec, ok = ParseSpec(vars(map[string]string{"width": "5", "height": "3"}))
	if !ok || spec.Dir != DirNorth {
		t.Fatalf("missing dir defaults north: %+v %v", spec, ok)
	}
	for name, m := range map[string]map[string]string{
		"expr width":  {"width": "SHUTTLE_W", "height": "3"},
		"expr dwidth": {"width": "5", "height": "3", "dwidth": "w/2"},
		"zero size":   {"width": "0", "height": "3"},
		"no size":     {},
		"expr dir":    {"width": "5", "height": "3", "dir": "NORTH|EAST"},
		"null":        {"width": "null", "height": "3"},
	} {
		if _, ok := ParseSpec(vars(m)); ok {
			t.Errorf("%s: want unknown", name)
		}
	}
}

type testLevel struct {
	maxX, maxY int
	turfs      map[[2]int][]string
}

func (l testLevel) level() Level {
	return Level{MaxX: l.maxX, MaxY: l.maxY, TurfPaths: func(x, y int) []string { return l.turfs[[2]int{x, y}] }}
}

func port(path string, x, y int, m map[string]string) Input {
	return Input{Path: path, X: x, Y: y, Var: vars(m)}
}

func TestAnalyzeEdge(t *testing.T) {
	lv := testLevel{maxX: 20, maxY: 20}
	in := []Input{
		port("/obj/docking_port/mobile", 2, 2, map[string]string{"width": "4", "height": "4", "dwidth": "3", "dheight": "3"}),
		port("/obj/docking_port/mobile", 10, 10, map[string]string{"width": "4", "height": "4"}),
		port("/obj/docking_port/mobile", 18, 10, map[string]string{"width": "4", "height": "2"}),
	}
	out := Analyze(in, lv.level())
	if out[0].Warnings&WarnEdge == 0 {
		t.Error("expected edge warning (min below 1)")
	}
	if out[1].Warnings != 0 {
		t.Errorf("unexpected warnings %b", out[1].Warnings)
	}
	if out[2].Warnings&WarnEdge == 0 {
		t.Error("expected edge warning (max beyond MaxX)")
	}
}

func TestAnalyzeDockedShuttleIsNotOverlap(t *testing.T) {
	lv := testLevel{maxX: 30, maxY: 30}
	size := map[string]string{"width": "5", "height": "5", "dwidth": "2"}
	docked := Analyze([]Input{
		port("/obj/docking_port/stationary", 10, 10, size),
		port("/obj/docking_port/mobile", 10, 10, size),
	}, lv.level())
	if docked[0].Warnings&WarnOverlap != 0 || docked[1].Warnings&WarnOverlap != 0 {
		t.Error("shuttle docked on its stationary port reported overlap")
	}
	offset := Analyze([]Input{
		port("/obj/docking_port/stationary", 10, 10, size),
		port("/obj/docking_port/mobile", 11, 10, size),
	}, lv.level())
	if offset[0].Warnings&WarnOverlap == 0 || offset[1].Warnings&WarnOverlap == 0 {
		t.Error("offset mobile port must still overlap the dock")
	}
}

func TestAnalyzeOverlap(t *testing.T) {
	lv := testLevel{maxX: 30, maxY: 30}
	in := []Input{
		port("/obj/docking_port/stationary", 5, 5, map[string]string{"width": "5", "height": "5"}),
		port("/obj/docking_port/mobile", 9, 9, map[string]string{"width": "3", "height": "3"}),
		port("/obj/docking_port/mobile", 20, 20, map[string]string{"width": "3", "height": "3"}),
		port("/obj/docking_port/mobile", 22, 22, map[string]string{"width": "x", "height": "3"}),
	}
	out := Analyze(in, lv.level())
	if out[0].Warnings&WarnOverlap == 0 || out[1].Warnings&WarnOverlap == 0 {
		t.Error("expected mutual overlap for 0 and 1 (tile 9,9 shared)")
	}
	if out[2].Warnings&WarnOverlap != 0 {
		t.Error("unknown-size port must not participate in overlap")
	}
	if out[3].Known {
		t.Error("port 3 must be unknown")
	}
	edge := Analyze([]Input{
		port("/obj/docking_port/mobile", 1, 1, map[string]string{"width": "2", "height": "2"}),
		port("/obj/docking_port/mobile", 3, 1, map[string]string{"width": "2", "height": "2"}),
	}, lv.level())
	if edge[0].Warnings&WarnOverlap != 0 {
		t.Error("adjacent rectangles must not overlap")
	}
}

func TestAnalyzeTurfs(t *testing.T) {
	lv := testLevel{maxX: 30, maxY: 30, turfs: map[[2]int][]string{
		{5, 5}: {"/turf/open/space/basic"},
		{6, 5}: {"/turf/open/floor/plasteel"},
		{5, 6}: {"/turf/open/space"},
		{6, 6}: {"/turf/open/spacefake"}, // not a path-boundary match -> non space
	}}
	stationary := port("/obj/docking_port/stationary/transit", 5, 5, map[string]string{"width": "2", "height": "2"})
	mobile := port("/obj/docking_port/mobile", 5, 5, map[string]string{"width": "2", "height": "2"})
	out := Analyze([]Input{stationary}, lv.level())
	if out[0].Warnings&WarnNonSpace == 0 || out[0].NonSpace != 2 {
		t.Fatalf("stationary: warnings %b nonspace %d", out[0].Warnings, out[0].NonSpace)
	}
	if out[0].Kind != KindStationary {
		t.Fatal("kind")
	}
	out = Analyze([]Input{mobile}, lv.level())
	if out[0].Warnings&WarnNonSpace != 0 || out[0].Kind != KindMobile {
		t.Fatal("mobile ports are not turf-checked")
	}
	all := testLevel{maxX: 30, maxY: 30, turfs: map[[2]int][]string{
		{5, 5}: {"/turf/open/space"}, {6, 5}: {"/turf/open/space/basic"}, {5, 6}: {"/turf/open/space"}, {6, 6}: {"/turf/open/space"},
	}}
	out = Analyze([]Input{stationary}, all.level())
	if out[0].Warnings != 0 {
		t.Fatalf("all space: %b", out[0].Warnings)
	}
}

func TestLabel(t *testing.T) {
	out := Analyze([]Input{
		port("/obj/docking_port/mobile", 5, 5, map[string]string{"width": "1", "height": "1", "name": `"ferry"`, "shuttle_id": `"ferry_1"`}),
		port("/obj/docking_port/mobile", 7, 5, map[string]string{"width": "1", "height": "1", "shuttle_id": `"x"`}),
		port("/obj/docking_port/mobile", 9, 5, map[string]string{"width": "1", "height": "1"}),
	}, Level{MaxX: 30, MaxY: 30})
	if out[0].Label != "ferry [ferry_1]" || out[1].Label != "x" || out[2].Label != "" {
		t.Fatalf("labels %q %q %q", out[0].Label, out[1].Label, out[2].Label)
	}
}
