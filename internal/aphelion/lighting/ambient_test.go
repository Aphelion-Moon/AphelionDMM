package lighting

import "testing"

func spaceLevel(w, h int) (*Level, []bool) {
	lvl := NewLevel(w, h)
	lvl.Space = make([]bool, w*h)
	for i := range lvl.Space {
		lvl.Space[i] = true
	}
	return lvl, lvl.Space
}

func TestStarlightOnlyOnSpaceTilesTouchingNonSpace(t *testing.T) {
	// space.dm:106-113: a space tile enables starlight only if one of the
	// 8 neighbours is neither space nor cordon.
	lvl, space := spaceLevel(7, 7)
	space[3*7+3] = false
	lvl.Starlight = DefaultStarlight()
	got := lvl.StarlightSources()
	if len(got) != 8 {
		t.Fatalf("want 8 starlight sources, got %d", len(got))
	}
	for _, s := range got {
		if s.Range != 2 || s.Power != 1 || s.Height != -0.5 || s.System != SystemComplex {
			t.Fatalf("unexpected starlight source %+v", s)
		}
		if s.Color.B <= s.Color.R {
			t.Fatalf("COLOR_STARLIGHT #8589fa should be blue-ish: %+v", s.Color)
		}
	}
	lvl.Starlight.Enabled = false
	if n := len(lvl.StarlightSources()); n != 0 {
		t.Fatalf("starlight off must emit nothing, got %d", n)
	}
}

func TestStarlightLightsAdjacentFloorCorners(t *testing.T) {
	lvl, space := spaceLevel(7, 7)
	space[3*7+3] = false
	lvl.Starlight = DefaultStarlight()
	res := Compute(lvl, DefaultOptions())
	c := res.Corner(3, 3) // SW corner of the floor tile
	if c.B <= 0 || c.B < c.R {
		t.Fatalf("expected starlit blue corner, got %v", c)
	}
	lvl.Starlight.Enabled = false
	res = Compute(lvl, DefaultOptions())
	if res.Corner(3, 3) != (RGB{}) {
		t.Fatalf("no starlight, no light")
	}
}

func TestResolveAmbient(t *testing.T) {
	cfg := DefaultStarlight()
	// lighting_area.dm:74 alpha/255 additive colour.
	got := ResolveAmbient(AreaBase{Color: RGB{1, 1, 1}, Alpha: 255 * 0.5}, false, cfg)
	near(t, "half", got.R, 0.5, 1e-12)
	// Alpha 0 -> no ambient.
	if ResolveAmbient(AreaBase{Color: RGB{1, 1, 1}}, false, cfg) != (RGB{}) {
		t.Fatalf("alpha 0 must be dark")
	}
	// turf.dm:175-176: space_lit turf with no area base lighting shows starlight.
	got = ResolveAmbient(AreaBase{}, true, cfg)
	if got != cfg.Color {
		t.Fatalf("space_lit fallback should be starlight colour: %v", got)
	}
	// ...but not when starlight is disabled, nor when the area has base lighting.
	cfg2 := cfg
	cfg2.Enabled = false
	if ResolveAmbient(AreaBase{}, true, cfg2) != (RGB{}) {
		t.Fatalf("starlight disabled")
	}
	got = ResolveAmbient(AreaBase{Color: RGB{1, 0, 0}, Alpha: 255}, true, cfg)
	if got != (RGB{1, 0, 0}) {
		t.Fatalf("area base lighting wins: %v", got)
	}
}
