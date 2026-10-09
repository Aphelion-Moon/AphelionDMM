package lighting

import (
	"math"
	"testing"
)

func white(x, y int, rng, power float64) Source {
	return Source{X: x, Y: y, Range: rng, Power: power, Color: RGB{1, 1, 1}, Dir: DirNorth, Angle: 360, Height: 1, System: SystemComplex}
}

func roundTo64(v float64) float64 { return math.Floor(v*64+0.5) / 64 }

func TestSingleSourceCornersMatchHandComputedTG(t *testing.T) {
	lvl := NewLevel(11, 11)
	lvl.Sources = []Source{white(5, 5, 5, 1)}
	res := Compute(lvl, DefaultOptions())

	// Tile (5,5) NE corner is corner index (6,6), offset (0.5,0.5).
	// falloff 0.7550510257 -> rounded to 1/64 (lighting_corner.dm:130) = 0.75.
	c := res.Corner(6, 6)
	near(t, "NE r", c.R, 0.75, 1e-12)
	near(t, "NE g", c.G, 0.75, 1e-12)
	near(t, "NE b", c.B, 0.75, 1e-12)

	// Corner (8,6): offset (2.5,0.5), falloff 0.4522774 -> 29/64.
	near(t, "(8,6)", res.Corner(8, 6).R, 29.0/64.0, 1e-12)
	// Corner (10,6): offset (4.5,0.5), falloff 0.0726 -> 5/64.
	near(t, "(10,6)", res.Corner(10, 6).R, 5.0/64.0, 1e-12)
	// Corner (11,6): offset (5.5,0.5) beyond range -> 0.
	if res.Corner(11, 6).R != 0 {
		t.Fatalf("beyond range must be 0")
	}
}

func TestPowerScalesBeforeNormalisation(t *testing.T) {
	lvl := NewLevel(11, 11)
	lvl.Sources = []Source{white(5, 5, 5, 0.5)}
	res := Compute(lvl, DefaultOptions())
	// 0.755051 * 0.5 = 0.3775 -> 24.16/64 -> 24/64
	near(t, "half power", res.Corner(6, 6).R, 24.0/64.0, 1e-12)
}

func TestOpacityShadowsCornersBehindWall(t *testing.T) {
	lvl := NewLevel(21, 11)
	for y := 0; y < 11; y++ {
		lvl.SetOpaque(7, y, true)
	}
	lvl.Sources = []Source{white(5, 5, 8, 1)}
	res := Compute(lvl, DefaultOptions())

	// Corner index 7 sits at x=6.5, between tiles 6 and 7: the wall face is lit.
	if res.Corner(7, 5).R <= 0 {
		t.Fatalf("wall face corner should be lit")
	}
	// Corner index 8 sits at x=7.5, between the wall (7) and the hidden tile
	// (8): both neighbours are opaque or unseen, so it must stay dark
	// (lighting_source.dm:476-479 skips opaque turfs).
	if res.Corner(8, 5).R != 0 {
		t.Fatalf("corner behind wall must be dark, got %v", res.Corner(8, 5))
	}
	for i := 9; i <= 14; i++ {
		if res.Corner(i, 5).R != 0 {
			t.Fatalf("corner %d behind wall must be dark", i)
		}
	}
}

func TestOpenDoorwayLetsLightThrough(t *testing.T) {
	lvl := NewLevel(21, 11)
	for y := 0; y < 11; y++ {
		if y != 5 {
			lvl.SetOpaque(7, y, true)
		}
	}
	lvl.Sources = []Source{white(5, 5, 8, 1)}
	res := Compute(lvl, DefaultOptions())
	if res.Corner(10, 5).R <= 0 {
		t.Fatalf("light should pass through gap")
	}
	lvl.SetOpaque(7, 5, true)
	res = Compute(lvl, DefaultOptions())
	if res.Corner(10, 5).R != 0 {
		t.Fatalf("closed gap must block light")
	}
}

func TestOpaqueSourceTileDoesNotCrash(t *testing.T) {
	lvl := NewLevel(5, 5)
	lvl.SetOpaque(2, 2, true)
	lvl.Sources = []Source{white(2, 2, 3, 1)}
	_ = Compute(lvl, DefaultOptions())
}

func TestSourcesOutsideGridAreIgnored(t *testing.T) {
	lvl := NewLevel(5, 5)
	lvl.Sources = []Source{white(-1, 2, 3, 1), white(5, 2, 3, 1), white(2, 9, 3, 1)}
	res := Compute(lvl, DefaultOptions())
	for j := 0; j <= 5; j++ {
		for i := 0; i <= 5; i++ {
			if res.Corner(i, j) != (RGB{}) {
				t.Fatalf("expected darkness")
			}
		}
	}
}

func TestDirectionalConeLightsFacingSideOnly(t *testing.T) {
	lvl := NewLevel(21, 21)
	s := white(10, 10, 6, 1)
	s.Dir = DirEast
	s.Angle = 90
	lvl.Sources = []Source{s}
	res := Compute(lvl, DefaultOptions())
	east := res.Corner(13, 11) // offset (+2.5, +0.5)
	west := res.Corner(8, 11)  // offset (-2.5, +0.5)
	if east.R <= 0 {
		t.Fatalf("east should be lit")
	}
	if west.R != 0 {
		t.Fatalf("west should be dark, got %v", west)
	}
}

func TestColorMixingNormalisesByLargestChannel(t *testing.T) {
	lvl := NewLevel(11, 11)
	red := white(5, 5, 5, 1)
	red.Color = RGB{1, 0, 0}
	lvl.Sources = []Source{red, white(5, 5, 5, 1)}
	res := Compute(lvl, DefaultOptions())
	// raw = (1.5102, 0.7551, 0.7551); divided by max (lighting_corner.dm:113-116)
	// -> (1, 0.5, 0.5).
	c := res.Corner(6, 6)
	near(t, "r", c.R, 1, 1e-12)
	near(t, "g", c.G, 0.5, 1.0/64)
	near(t, "b", c.B, 0.5, 1.0/64)
}

func TestColorsAddPerChannelWithoutClampBelowOne(t *testing.T) {
	lvl := NewLevel(11, 11)
	red := white(5, 5, 5, 0.4)
	red.Color = RGB{1, 0, 0}
	blue := white(5, 5, 5, 0.4)
	blue.Color = RGB{0, 0, 1}
	lvl.Sources = []Source{red, blue}
	res := Compute(lvl, DefaultOptions())
	c := res.Corner(6, 6)
	want := roundTo64(0.7550510257 * 0.4)
	near(t, "r", c.R, want, 1e-12)
	near(t, "g", c.G, 0, 1e-12)
	near(t, "b", c.B, want, 1e-12)
}

func TestNegativePowerSubtractsAndOutputNeverNegative(t *testing.T) {
	lvl := NewLevel(11, 11)
	neg := white(5, 5, 5, -1)
	lvl.Sources = []Source{white(5, 5, 5, 0.5), neg}
	res := Compute(lvl, DefaultOptions())
	c := res.Corner(6, 6)
	if c.R < 0 || c.G < 0 || c.B < 0 {
		t.Fatalf("negative output %v", c)
	}
	if c.R != 0 {
		t.Fatalf("net negative light should clamp to 0, got %v", c.R)
	}
}

func TestClampKeepsTileCornersInUnitRangeAndAppliesCutoff(t *testing.T) {
	lvl := NewLevel(5, 5)
	opts := DefaultOptions()
	res := Compute(lvl, opts)
	// Dark tile: raw 0, cutoff floor 0.10 (render_plate.dm:296,352-353).
	tc := res.Tile(lvl, 2, 2)
	for _, c := range tc {
		near(t, "cutoff", c.R, 0.10, 1e-12)
	}
	// Ambient above 1 clamps to 1.
	lvl.Ambient = make([]RGB, 25)
	lvl.Ambient[2*5+2] = RGB{3, 0.5, 0}
	tc = res.Tile(lvl, 2, 2)
	for _, c := range tc {
		near(t, "amb r", c.R, 1, 1e-12)
		near(t, "amb g", c.G, 0.5, 1e-12)
		near(t, "amb b", c.B, 0.10, 1e-12)
	}
}

func TestUnlitTileIgnoresCornerLightButKeepsAmbient(t *testing.T) {
	// Areas with static_lighting = FALSE have no lighting object
	// (static_lighting_area.dm:39,46-51): only base lighting shows.
	lvl := NewLevel(11, 11)
	lvl.Sources = []Source{white(5, 5, 5, 1)}
	lvl.Unlit = make([]bool, 121)
	lvl.Unlit[5*11+5] = true
	lvl.Ambient = make([]RGB, 121)
	lvl.Ambient[5*11+5] = RGB{0.3, 0.3, 0.3}
	res := Compute(lvl, DefaultOptions())
	for _, c := range res.Tile(lvl, 5, 5) {
		near(t, "unlit", c.R, 0.3, 1e-12)
	}
	// A lit neighbour still receives the light that corners share.
	if res.Tile(lvl, 6, 5)[0].R <= 0.3 {
		t.Fatalf("neighbour should be lit")
	}
}

func TestOverlayLightIsApproximatedWithClampedRange(t *testing.T) {
	lvl := NewLevel(31, 31)
	s := white(15, 15, 20, 1)
	s.System = SystemOverlay
	lvl.Sources = []Source{s}
	res := Compute(lvl, DefaultOptions())
	// overlay_lighting.dm:401 clamps range to 1..6.
	if res.Corner(15+8, 16).R != 0 {
		t.Fatalf("overlay light must not reach beyond 6 tiles")
	}
	if res.Corner(16, 16).R <= 0 {
		t.Fatalf("overlay light should light its own tile")
	}
	opts := DefaultOptions()
	opts.IncludeOverlayLights = false
	res = Compute(lvl, opts)
	if res.Corner(16, 16).R != 0 {
		t.Fatalf("overlay lights disabled")
	}
}

func TestMaxRangeBoundsReach(t *testing.T) {
	lvl := NewLevel(101, 11)
	lvl.Sources = []Source{white(50, 5, 60, 1)}
	opts := DefaultOptions()
	opts.MaxRange = 8
	res := Compute(lvl, opts)
	if res.Corner(50+20, 5).R != 0 {
		t.Fatalf("MaxRange must bound reach")
	}
}

func TestSourceShiftMovesEmitter(t *testing.T) {
	lvl := NewLevel(11, 11)
	s := white(5, 5, 5, 1)
	s.ShiftX = 0.5 // emitter halfway to tile 6 -> corner 6 is now directly beside it
	lvl.Sources = []Source{s}
	res := Compute(lvl, DefaultOptions())
	a, b := res.Corner(6, 5), res.Corner(5, 5)
	// corner (6,5) pos (5.5,4.5); emitter (5.5,5): offset (0,-0.5).
	if a.R <= b.R {
		t.Fatalf("shift ignored: %v %v", a, b)
	}
}
