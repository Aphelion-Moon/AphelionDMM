package ingame

import (
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

type fakeMap struct {
	w, h  int
	tiles map[[2]int][]*dmmprefab.Prefab
}

func newMap(w, h int) *fakeMap { return &fakeMap{w: w, h: h, tiles: map[[2]int][]*dmmprefab.Prefab{}} }

func (m *fakeMap) Prefabs(x, y, z int) ([]*dmmprefab.Prefab, bool) {
	if x < 1 || y < 1 || x > m.w || y > m.h || z != 1 {
		return nil, false
	}
	return m.tiles[[2]int{x, y}], true
}

func (m *fakeMap) put(x, y int, p *dmmprefab.Prefab) *dmmprefab.Prefab {
	m.tiles[[2]int{x, y}] = append(m.tiles[[2]int{x, y}], p)
	return p
}

func prefab(path string, kv ...string) *dmmprefab.Prefab {
	v := &dmvars.MutableVariables{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Put(kv[i], kv[i+1])
	}
	return dmmprefab.New(dmmprefab.IdNone, path, v.ToImmutable())
}

func resolve(t *testing.T, m Map, x, y int, p *dmmprefab.Prefab) Override {
	t.Helper()
	o, ok := Resolve(m, x, y, 1, p)
	if !ok {
		t.Fatalf("%s at %d,%d not resolved", p.Path(), x, y)
	}
	return o
}

func wall() *dmmprefab.Prefab {
	return prefab("/turf/closed/wall", "smoothing_flags", "1", "base_icon_state", `"wall"`, "smoothing_groups", `"-1,58,"`, "canSmoothWith", `"-41,-22,-1,"`)
}

func TestWallsSmoothWithWallsAndWindowsButNotFloors(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(3, 3)
	floor := prefab("/turf/open/floor", "smoothing_groups", `"0,"`)
	window := prefab("/obj/structure/window/reinforced/fulltile", "anchored", "1", "smoothing_flags", "1", "base_icon_state", `"reinforced_window"`, "smoothing_groups", `"-22,"`, "canSmoothWith", `"-41,-22,-1,"`)
	for x := 1; x <= 3; x++ {
		for y := 1; y <= 3; y++ {
			m.put(x, y, floor)
		}
	}
	center := wall()
	m.tiles[[2]int{2, 2}] = []*dmmprefab.Prefab{center}
	m.tiles[[2]int{2, 3}] = []*dmmprefab.Prefab{wall()}                  // north
	m.tiles[[2]int{1, 2}] = []*dmmprefab.Prefab{wall()}                  // west
	m.tiles[[2]int{1, 3}] = []*dmmprefab.Prefab{wall()}                  // north-west
	if got := resolve(t, m, 2, 2, center).IconState; got != "wall-137" { // N|W|NW = 1+8+128
		t.Fatalf("state = %q, want wall-137", got)
	}
	// canSmoothWith starting with an object group turns on SMOOTH_OBJ, so the
	// wall also joins the anchored window to its east: N|E|W|NW.
	m.put(3, 2, window)
	if got := resolve(t, m, 2, 2, center).IconState; got != "wall-141" {
		t.Fatalf("wall did not smooth with the window: %q", got)
	}
	// Without an object group in canSmoothWith, the window east is ignored and
	// only the walls (group 58) count: N|W|NW.
	turfOnly := prefab("/turf/closed/wall", "smoothing_flags", "1", "base_icon_state", `"wall"`, "smoothing_groups", `"58,"`, "canSmoothWith", `"58,"`)
	m.tiles[[2]int{2, 2}] = []*dmmprefab.Prefab{turfOnly}
	if got := resolve(t, m, 2, 2, turfOnly).IconState; got != "wall-137" {
		t.Fatalf("turf-only smoothing joined objects: %q", got)
	}
	m.tiles[[2]int{2, 2}] = []*dmmprefab.Prefab{center}
	if got := resolve(t, m, 3, 2, window).IconState; got != "reinforced_window-8" { // west wall
		t.Fatalf("window state = %q", got)
	}
	// SMOOTH_BORDER counts the map edge.
	bordered := prefab("/turf/closed/wall", "smoothing_flags", "9", "base_icon_state", `"wall"`, "smoothing_groups", `"-1,"`, "canSmoothWith", `"-1,"`)
	m.tiles[[2]int{1, 1}] = []*dmmprefab.Prefab{bordered}
	// North is a wall; south, west and the SW/NW diagonals are off the map;
	// east is floor: N|S|W|SW|NW = 1+2+8+64+128.
	if got := resolve(t, m, 1, 1, bordered).IconState; got != "wall-203" {
		t.Fatalf("bordered state = %q, want wall-203", got)
	}
}

func TestBrokenFloorsAndProcFilteredAtomsKeepTheirIcon(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(1, 1)
	broken := prefab("/turf/open/floor/carpet", "smoothing_flags", "1", "base_icon_state", `"carpet"`, "broken", "1")
	if _, ok := Resolve(m, 1, 1, 1, broken); ok {
		t.Fatal("broken floor re-smoothed")
	}
	filtered := prefab("/obj/structure/railing", "smoothing_flags", "65", "base_icon_state", `"railing"`)
	if Affects(filtered) {
		t.Fatal("border-object smoothing is not modelled and must be left alone")
	}
}

func cableAt(layer string) *dmmprefab.Prefab {
	return prefab("/obj/structure/cable", "cable_layer", layer)
}

func TestCablesLinkPerLayerAndShowNodes(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(3, 3)
	center := m.put(2, 2, cableAt("2"))
	m.put(2, 3, cableAt("2"))
	m.put(3, 2, cableAt("2"))
	m.put(1, 2, cableAt("1")) // other layer: no link
	if got := resolve(t, m, 2, 2, center).IconState; got != "l2-1-4" {
		t.Fatalf("state = %q", got)
	}
	m.put(2, 2, prefab("/obj/structure/grille"))
	if got := resolve(t, m, 2, 2, center).IconState; got != "l2-1-4-node" {
		t.Fatalf("grille node missing: %q", got)
	}
	lone := m.put(1, 1, cableAt("4"))
	if got := resolve(t, m, 1, 1, lone).IconState; got != "l4-noconnection" {
		t.Fatalf("lone = %q", got)
	}
	// An SMES never links to the terminal next to it.
	n := newMap(2, 1)
	underSmes := n.put(1, 1, cableAt("2"))
	n.put(1, 1, prefab("/obj/machinery/power/smes"))
	n.put(2, 1, cableAt("2"))
	n.put(2, 1, prefab("/obj/machinery/power/terminal"))
	if got := resolve(t, n, 1, 1, underSmes).IconState; got != "l2-noconnection" {
		t.Fatalf("smes linked to its terminal: %q", got)
	}
}

func pipe(path, layer, color string, kv ...string) *dmmprefab.Prefab {
	return prefab(path, append([]string{"piping_layer", layer, "pipe_color", `"` + color + `"`, "initialize_directions", "15"}, kv...)...)
}

const smart = "/obj/machinery/atmospherics/pipe/smart/manifold4w/supply/hidden"

func TestSmartPipesConnectByLayerColourAndPorts(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(3, 3)
	center := m.put(2, 2, pipe(smart, "3", "#0000FF"))
	m.put(1, 2, pipe(smart, "3", "#0000FF")) // west: same net
	m.put(3, 2, pipe(smart, "3", "#0000FF")) // east: same net
	m.put(2, 3, pipe(smart, "2", "#0000FF")) // north: other layer
	m.put(2, 1, pipe(smart, "3", "#FF0000")) // south: other colour
	o := resolve(t, m, 2, 2, center)
	if o.IconState != "12_3" || o.Dir != east || o.Icon != pipeBitmaskIcon {
		t.Fatalf("straight pipe = %+v", o)
	}
	// A vent facing up into the pipe from below joins; omni colour connects to any.
	m.tiles[[2]int{2, 1}] = []*dmmprefab.Prefab{prefab("/obj/machinery/atmospherics/components/unary/vent_pump", "dir", "1", "piping_layer", "3", "pipe_color", `"#EEEEEE"`, "pipe_flags", "2")}
	if got := resolve(t, m, 2, 2, center).IconState; got != "14_3" { // S|E|W
		t.Fatalf("with vent = %q", got)
	}
	// A vent facing away does not.
	m.tiles[[2]int{2, 1}] = []*dmmprefab.Prefab{prefab("/obj/machinery/atmospherics/components/unary/vent_pump", "dir", "2", "piping_layer", "3", "pipe_color", `"#EEEEEE"`)}
	if got := resolve(t, m, 2, 2, center).IconState; got != "12_3" {
		t.Fatalf("away-facing vent connected: %q", got)
	}
}

func TestLoneSmartPipeGetsShortStubs(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(3, 3)
	lone := m.put(2, 2, pipe(smart, "3", "#0000FF"))
	// No connections: ISSTUB adds init dirs from the lowest bit until two exist.
	if got := resolve(t, m, 2, 2, lone).IconState; got != "48_3" { // (N|S)<<4
		t.Fatalf("lone = %q", got)
	}
	m.put(3, 2, pipe(smart, "3", "#0000FF"))
	// One connection east: add the opposite side as a short pipe.
	if got := resolve(t, m, 2, 2, lone).IconState; got != "132_3" { // E | W<<4
		t.Fatalf("one-sided = %q", got)
	}
}

func TestHeatExchangePipesOnlyJoinThroughJunctions(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(2, 1)
	p := m.put(1, 1, pipe(smart, "3", "#0000FF"))
	m.put(2, 1, prefab(heatExchangeRoot+"/simple", "dir", "4", "piping_layer", "3", "pipe_color", `"#EEEEEE"`))
	if got := resolve(t, m, 1, 1, p).IconState; got != "48_3" { // unconnected: N|S stubs
		t.Fatalf("pipe joined a heat exchange pipe: %q", got)
	}
	// junction.dm accepts a normal pipe on the side where its dir equals the
	// direction from that pipe to the junction.
	m.tiles[[2]int{2, 1}] = []*dmmprefab.Prefab{prefab(heatExchangeRoot+"/junction", "dir", "4", "piping_layer", "3", "pipe_color", `"#EEEEEE"`)}
	if got := resolve(t, m, 1, 1, p).IconState; got != "132_3" { // E | W<<4
		t.Fatalf("pipe did not join the junction: %q", got)
	}
	m.tiles[[2]int{2, 1}] = []*dmmprefab.Prefab{prefab(heatExchangeRoot+"/junction", "dir", "8", "piping_layer", "3", "pipe_color", `"#EEEEEE"`)}
	if got := resolve(t, m, 1, 1, p).IconState; got != "48_3" {
		t.Fatalf("pipe joined the junction's heat-exchange side: %q", got)
	}
}

// Window spawners are drawn as what they spawn, and spawned windows smooth with
// their neighbours like mapped ones.
func TestStructureSpawnersExpandAndSmooth(t *testing.T) {
	t.Cleanup(resetCache)
	types := map[string]*dmvars.Variables{}
	define := func(path string, kv ...string) {
		v := &dmvars.MutableVariables{}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Put(kv[i], kv[i+1])
		}
		types[path] = v.ToImmutable()
	}
	define("/obj/structure/grille", "icon", "'icons/obj/grille.dmi'", "icon_state", `"grille"`, "anchored", "1")
	define("/obj/structure/window/reinforced/fulltile", "icon", "'icons/obj/r_window.dmi'", "anchored", "1",
		"smoothing_flags", "1", "base_icon_state", `"reinforced_window"`, "smoothing_groups", `"-22,"`, "canSmoothWith", `"-41,-22,-1,"`)
	SetTypes(func(path string) *dmvars.Variables { return types[path] })
	t.Cleanup(func() { SetTypes(nil) })

	spawner := func() *dmmprefab.Prefab {
		return prefab("/obj/effect/spawner/structure/window/reinforced", "spawn_list", "list(/obj/structure/grille,/obj/structure/window/reinforced/fulltile)")
	}
	m := newMap(3, 1)
	left, right := m.put(1, 1, spawner()), m.put(2, 1, spawner())
	m.put(3, 1, wall())
	if !Affects(left) {
		t.Fatal("spawner not handled")
	}
	parts := Expand(m, 2, 1, 1, right)
	if len(parts) != 2 || parts[0].Prefab.Path() != "/obj/structure/grille" || parts[1].Prefab.Path() != "/obj/structure/window/reinforced/fulltile" {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[0].Override.IconState != "grille" {
		t.Fatalf("grille = %+v", parts[0].Override)
	}
	// West: the other spawner's window. East: a wall (group -1).
	if got := parts[1].Override.IconState; got != "reinforced_window-12" {
		t.Fatalf("window = %q", got)
	}
	// The wall joins the spawned window to its west.
	if got := resolve(t, m, 3, 1, m.tiles[[2]int{3, 1}][0]).IconState; got != "wall-8" {
		t.Fatalf("wall = %q", got)
	}
	// Dir-dependent spawners keep their mapper icon.
	end := prefab("/obj/effect/spawner/structure/window/hollow/reinforced/end", "spawn_list", "list(/obj/structure/grille)")
	if Affects(end) || Expand(m, 1, 1, 1, end) != nil {
		t.Fatal("hollow end spawner expanded from its static list")
	}
}

func TestToggleChangesVersionAndPersists(t *testing.T) {
	var saved []bool
	SetPersist(func(on bool) { saved = append(saved, on) })
	t.Cleanup(func() { SetPersist(func(bool) {}); Apply(false) })
	Apply(false)
	v := Version()
	SetEnabled(true)
	SetEnabled(true)
	if !Enabled() || Version() != v+1 || len(saved) != 1 || !saved[0] {
		t.Fatalf("enabled=%v version %d->%d saved %v", Enabled(), v, Version(), saved)
	}
}
