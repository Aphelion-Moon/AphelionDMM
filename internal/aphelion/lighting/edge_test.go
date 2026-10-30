package lighting

import (
	"math/rand"
	"testing"
)

func lavaLight() EdgeLight {
	return EdgeLight{Family: 1, Emits: true, Source: Source{
		Range: 2, Power: 0.75, Color: RGB{0xc4 / 255.0, 0x8a / 255.0, 0x18 / 255.0},
		Dir: DirNorth, Angle: 360, Height: 1, System: SystemComplex,
	}}
}

// lava.dm refresh_light: a lava turf switches its light on only when one of
// the turfs around it is not lava. A pool interior stays dark (it glows
// through its emissive mask instead) and the map edge is not a border.
func TestEdgeLightsOnlyAtTheBorderOfTheirFamily(t *testing.T) {
	const w, h = 7, 7
	lvl := NewLevel(w, h)
	lvl.Edge = make([]uint16, w*h)
	lvl.EdgeLights = []EdgeLight{lavaLight()}
	for y := 1; y <= 5; y++ {
		for x := 1; x <= 5; x++ {
			lvl.Edge[y*w+x] = 1
		}
	}
	got := lvl.EdgeSources()
	if len(got) != 16 { // the ring of a 5x5 pool
		t.Fatalf("want 16 border lava lights, got %d", len(got))
	}
	for _, s := range got {
		if s.X >= 2 && s.X <= 4 && s.Y >= 2 && s.Y <= 4 {
			t.Fatalf("interior lava at %d,%d emits", s.X, s.Y)
		}
		if s.Range != 2 || s.Power != 0.75 {
			t.Fatalf("edge light lost its own vars: %+v", s)
		}
	}

	// A pool filling the whole level touches only the map edge: no light.
	full := NewLevel(3, 3)
	full.Edge = []uint16{1, 1, 1, 1, 1, 1, 1, 1, 1}
	full.EdgeLights = []EdgeLight{lavaLight()}
	if n := len(full.EdgeSources()); n != 0 {
		t.Fatalf("map edge treated as a border: %d lights", n)
	}

	// Another lava type (plasma) is still lava: no border between them.
	plasma := lavaLight()
	plasma.Source.Color = RGB{0.5, 0, 1}
	lvl.EdgeLights = append(lvl.EdgeLights, plasma)
	lvl.Edge[3*w+3] = 2
	if n := len(lvl.EdgeSources()); n != 16 {
		t.Fatalf("a different lava type made a border: %d", n)
	}
	// A family member that does not emit still counts as the family.
	dark := lavaLight()
	dark.Emits = false
	lvl.EdgeLights = append(lvl.EdgeLights, dark)
	lvl.Edge[1*w+1] = 3
	if n := len(lvl.EdgeSources()); n != 15 {
		t.Fatalf("non-emitting lava: want 15 lights, got %d", n)
	}
}

func TestEdgeLightsLightTheirSurroundings(t *testing.T) {
	const w, h = 9, 9
	lvl := NewLevel(w, h)
	lvl.Edge = make([]uint16, w*h)
	lvl.EdgeLights = []EdgeLight{lavaLight()}
	lvl.Edge[4*w+4] = 1
	res := Compute(lvl, DefaultOptions())
	if c := res.Corner(4, 4); c.R <= 0 || c.R <= c.B {
		t.Fatalf("lava did not light its corner orange: %+v", c)
	}
	far := res.Tile(lvl, 0, 0)
	if far[0].R != DefaultOptions().Cutoff {
		t.Fatalf("lava light reached the far corner: %+v", far[0])
	}
}

// Emissive overlays (emissive_appearance) are drawn at full light whatever the
// lighting around them.
func TestEmissiveTilesAreFullyLit(t *testing.T) {
	lvl := NewLevel(3, 3)
	lvl.Emissive = make([]bool, 9)
	lvl.Emissive[4] = true
	lvl.Unlit = make([]bool, 9)
	lvl.Unlit[4] = true
	res := Compute(lvl, DefaultOptions())
	for k, c := range res.Tile(lvl, 1, 1) {
		if c != (RGB{1, 1, 1}) {
			t.Fatalf("emissive corner %d = %+v", k, c)
		}
	}
	if c := res.Tile(lvl, 0, 0)[0]; c.R != DefaultOptions().Cutoff {
		t.Fatalf("emissive leaked to its neighbour: %+v", c)
	}
	if c := lvl.Clone(); !c.Emissive[4] || &c.Emissive[0] == &lvl.Emissive[0] {
		t.Fatal("Clone must copy emissive flags")
	}
}

func TestIncrementalEqualsFullWithEdgeLights(t *testing.T) {
	const w, h = 40, 32
	r := rand.New(rand.NewSource(20261030))
	lvl := NewLevel(w, h)
	lvl.Edge = make([]uint16, w*h)
	lvl.EdgeLights = []EdgeLight{lavaLight()}
	for i := range lvl.Edge {
		if r.Intn(3) == 0 {
			lvl.Edge[i] = 1
		}
	}
	opts := DefaultOptions()
	res := Compute(lvl, opts)
	for step := 0; step < 120; step++ {
		x, y := r.Intn(w), r.Intn(h)
		if lvl.Edge[y*w+x] == 0 {
			lvl.Edge[y*w+x] = 1
		} else {
			lvl.Edge[y*w+x] = 0
		}
		res.Update(lvl, RectAround(x, y))
		assertSameResult(t, step, res, Compute(lvl, opts))
	}
	clone := lvl.Clone()
	if len(clone.Edge) != len(lvl.Edge) || len(clone.EdgeLights) != 1 || &clone.Edge[0] == &lvl.Edge[0] {
		t.Fatal("Clone must copy edge lights")
	}
}

func TestExtractEdgeReadsTheTurfsOwnLightAndIgnoresCompiledLightOn(t *testing.T) {
	vars := map[string]string{
		"light_range": "2", "light_power": "0.75", "light_color": `"#C48A18"`, "light_on": "FALSE",
		"mask_state": `"lava-lightmask"`,
	}
	get := func(name string) (string, bool) { v, ok := vars[name]; return v, ok }
	ex, ok := ExtractEdge(Atom{Path: "/turf/open/lava/plasma/ice_moon", Var: get}, DefaultEdgeProfiles())
	if !ok || !ex.Light.Emits || ex.Light.Family != 1 || !ex.Emissive {
		t.Fatalf("plasma lava: %+v %v", ex, ok)
	}
	if want, _ := ParseColor(`"#C48A18"`); ex.Light.Source.Color != want || ex.Light.Source.Range != 2 || ex.Light.Source.Power != 0.75 {
		t.Fatalf("light not read from the type's vars: %+v", ex.Light.Source)
	}

	// Without a mask variable there is no emissive overlay to predict.
	delete(vars, "mask_state")
	if ex, _ := ExtractEdge(Atom{Path: "/turf/open/lava", Var: get}, DefaultEdgeProfiles()); ex.Emissive {
		t.Fatal("emissive without a mask state")
	}
	// A lava type with no range is still lava for its neighbours.
	vars["light_range"] = "0"
	if ex, ok := ExtractEdge(Atom{Path: "/turf/open/lava", Var: get}, DefaultEdgeProfiles()); !ok || ex.Light.Emits || ex.Skip == "" {
		t.Fatalf("dark lava: %+v %v", ex, ok)
	}
	// Code where every lava turf is lit (light_on TRUE) uses ordinary sources.
	vars["light_on"], vars["light_range"] = "TRUE", "2"
	if _, ok := ExtractEdge(Atom{Path: "/turf/open/lava", Var: get}, DefaultEdgeProfiles()); ok {
		t.Fatal("an always-on lava light must stay an ordinary source")
	}
	if _, ok := ExtractEdge(Atom{Path: "/turf/open/floor", Var: get}, DefaultEdgeProfiles()); ok {
		t.Fatal("a floor is not lava")
	}
}
