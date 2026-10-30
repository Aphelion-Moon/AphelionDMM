package maplight

import (
	"testing"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/util"
)

// planet is a minimal Lavaland/Icebox-style environment: no space, no
// /area/space, outdoor areas and lava rivers. tg lava declares its light with
// light_on = FALSE and switches it on at runtime on border turfs
// (lava.dm refresh_light), and draws an emissive light mask (mask_state).
type planet struct {
	*synthetic
	basalt, rock, lava, plasma, outdoors, lit *dmmprefab.Prefab
}

func newPlanet() *planet {
	s := newSynthetic()
	p := &planet{synthetic: s}
	p.basalt = prefab("/turf/open/misc/asteroid/basalt/lava_land_surface", typ(s.root, "planetary_atmos", "1"))
	p.rock = prefab("/turf/closed/mineral/volcanic/lava_land_surface", typ(s.root, "opacity", "1"))
	lava := typ(s.root, "light_range", "2", "light_power", "0.75", "light_color", `"#C48A18"`, "light_on", "FALSE",
		"mask_icon", "'icons/turf/floors.dmi'", "mask_state", `"lava-lightmask"`)
	p.lava = prefab("/turf/open/lava/smooth/lava_land_surface", lava)
	p.plasma = prefab("/turf/open/lava/plasma/ice_moon", typ(lava, "light_color", `"#8A3DE0"`))
	p.outdoors = prefab("/area/lavaland/surface/outdoors", typ(s.root, "outdoors", "1"))
	// An icemoon-style surface: no lighting objects, full base lighting.
	p.lit = prefab("/area/icemoon/surface/outdoors", typ(s.root, "static_lighting", "FALSE", "base_lighting_alpha", "255", "base_lighting_color", `"#FFFFFF"`))
	return p
}

// lavaland returns a w by h basalt plain in the outdoor area with a 4x3 lava pool
// whose south-west corner is at (px, py), one-based.
func (p *planet) lavaland(w, h, px, py int) *dmmap.Dmm {
	d := &dmmap.Dmm{MaxX: w, MaxY: h, MaxZ: 1, Tiles: make([]*dmmap.Tile, w*h)}
	for y := 1; y <= h; y++ {
		for x := 1; x <= w; x++ {
			d.Tiles[(y-1)*w+x-1] = &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}}
			p.set(d, x, y, p.basalt, p.outdoors)
		}
	}
	for y := py; y < py+3; y++ {
		for x := px; x < px+4; x++ {
			p.set(d, x, y, p.lava, p.outdoors)
		}
	}
	return d
}

func TestLavaLightsAPlanetMapAtItsBorders(t *testing.T) {
	p := newPlanet()
	d := p.lavaland(16, 9, 6, 4)
	st, _ := buildAll(t, d, fullSettings())
	lvl := st.snapshot()
	rep := st.report()
	if rep.Skipped != 0 {
		t.Fatalf("lava must not be skipped as switched off: %+v", rep.Groups)
	}
	if got := len(lvl.EdgeSources()); got != 10 { // a 4x3 pool: all but the two middle tiles border basalt
		t.Fatalf("want 10 lava border lights, got %d", got)
	}

	c := NewController()
	defer c.Close()
	f := settle(t, c, input(d, &env{}, 1))
	if rep := c.Report(); rep.EdgeLights != 10 {
		t.Fatalf("report must count lava lights: %+v", rep)
	}
	// Basalt next to the pool is lit orange; basalt far away stays dark.
	near, far := tileColor(f, 4, 4, 1), tileColor(f, 0, 0, 0)
	if near[0] <= 0.10 || near[0] <= near[2] {
		t.Fatalf("basalt beside lava %v: want an orange light", near)
	}
	if far != ([3]float32{0.10, 0.10, 0.10}) {
		t.Fatalf("far basalt %v must stay dark", far)
	}
	// The lava itself glows through its emissive mask.
	if lava := tileColor(f, 6, 4, 0); lava != ([3]float32{1, 1, 1}) {
		t.Fatalf("lava tile %v must be fully lit", lava)
	}
}

func TestLavaPoolInteriorAndPlasmaShareTheFamily(t *testing.T) {
	p := newPlanet()
	d := p.lavaland(12, 9, 3, 3)
	for y := 3; y <= 7; y++ { // a 5x5 pool, plasma in the middle
		for x := 3; x <= 7; x++ {
			p.set(d, x, y, p.lava, p.outdoors)
		}
	}
	p.set(d, 5, 5, p.plasma, p.outdoors)
	st, cls := buildAll(t, d, fullSettings())
	if got := len(st.snapshot().EdgeSources()); got != 16 {
		t.Fatalf("only the ring of a 5x5 pool borders basalt: %d lights", got)
	}
	// Draining a ring tile exposes the interior tile next to it.
	p.set(d, 3, 5, p.basalt, p.outdoors)
	st.recapture(d, 1, cls, []util.Point{{X: 3, Y: 5, Z: 1}})
	lights := st.snapshot().EdgeSources()
	found := false
	for _, s := range lights {
		if s.X == 3 && s.Y == 4 { // zero-based (4, 5)
			found = true
		}
	}
	if !found || len(lights) != 18 { // 15 ring tiles plus the three interior tiles beside the gap
		t.Fatalf("edge lights after the edit: %d %+v", len(lights), lights)
	}
}

func TestPlanetAreasWithoutStaticLightingUseTheirBaseLighting(t *testing.T) {
	p := newPlanet()
	d := p.lavaland(6, 4, 2, 1)
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 6; x++ {
			p.set(d, x, y, p.basalt, p.lit)
		}
	}
	p.set(d, 3, 3, p.rock, p.lit)
	c := NewController()
	defer c.Close()
	f := settle(t, c, input(d, &env{}, 1))
	if got := tileColor(f, 0, 0, 0); got != ([3]float32{1, 1, 1}) {
		t.Fatalf("fully base-lit outdoor tile %v", got)
	}
	if rep := c.Report(); rep.Sources != 0 || rep.Starlight != 0 {
		t.Fatalf("a planet surface derives no starlight: %+v", rep)
	}
}
