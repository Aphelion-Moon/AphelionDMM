package chunk

import (
	"runtime"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"

	"sdmm/internal/aphelion/ingame"
	"sdmm/internal/aphelion/spritedirs"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func inGamePrefab(path string, kv ...string) *dmmprefab.Prefab {
	v := &dmvars.MutableVariables{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Put(kv[i], kv[i+1])
	}
	return dmmprefab.New(dmmprefab.IdNone, path, v.ToImmutable())
}

// The chunk asks the in-game rules about connected atoms on the live map and
// keeps the mapper icon for everything else.
func TestInGameAppearanceUsesNeighboursOnTheLiveMap(t *testing.T) {
	dmm := newChunkTestMap(3, 1, 1)
	pipe := inGamePrefab("/obj/machinery/atmospherics/pipe/smart/manifold4w/supply/hidden",
		"icon", "'icons/obj/pipes_n_cables/manifold.dmi'", "piping_layer", "3", "pipe_color", `"#0000FF"`, "initialize_directions", "15")
	for x := 1; x <= 3; x++ {
		dmm.GetTile(util.Point{X: x, Y: 1, Z: 1}).InstancesAdd(pipe)
	}
	dmm.GetTile(util.Point{X: 2, Y: 1, Z: 1}).InstancesAdd(inGamePrefab("/obj/item/wrench"))
	m := ingame.DmmMap(dmm)
	middle := dmm.GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()
	icon, state, dir, ok := inGameAppearance(m, 2, 1, 1, middle[0])
	if !ok || icon != "icons/obj/pipes_n_cables/!pipes_bitmask.dmi" || state != "12_3" || dir != 4 {
		t.Fatalf("pipe = %q %q %d %v", icon, state, dir, ok)
	}
	if _, _, _, ok := inGameAppearance(m, 2, 1, 1, middle[1]); ok {
		t.Fatal("an unconnected item was overridden")
	}
}

func TestInGameAppearanceKeepsMapperIconForMissingStates(t *testing.T) {
	dmm := newChunkTestMap(1, 1, 1)
	wall := inGamePrefab("/turf/closed/mineral", "icon", "'icons/turf/mining.dmi'", "smoothing_flags", "1", "base_icon_state", `"smoothrocks"`, "smoothing_groups", `"1,"`, "canSmoothWith", `"1,"`)
	dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).InstancesAdd(wall)
	instance := dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	// No icons on disk: the index reads nothing, so every state is unknown.
	spritedirs.Activate(spritedirs.New(t.TempDir()))
	t.Cleanup(func() { spritedirs.Activate(nil) })
	if _, _, _, ok := inGameAppearance(ingame.DmmMap(dmm), 1, 1, 1, instance); ok {
		t.Fatal("overrode with a state the icon does not have")
	}
	spritedirs.Activate(nil)
	if _, state, _, ok := inGameAppearance(ingame.DmmMap(dmm), 1, 1, 1, instance); !ok || state != "smoothrocks-0" {
		t.Fatalf("without an index the prediction applies: %q %v", state, ok)
	}
}

// A window spawner draws its grille and window; both units still pick the
// spawner instance.
func TestSpawnerDrawsItsPartsAndKeepsTheSpawnerSelectable(t *testing.T) {
	if chunkTestWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1: units need a GL context for sprites")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	chunkTestWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)
	types := map[string]*dmvars.Variables{
		"/obj/structure/grille":                     inGamePrefab("x", "icon", "'g.dmi'", "icon_state", `"grille"`, "layer", "2.9").Vars(),
		"/obj/structure/window/reinforced/fulltile": inGamePrefab("x", "icon", "'w.dmi'", "icon_state", `"w"`, "layer", "3").Vars(),
	}
	ingame.SetTypes(func(path string) *dmvars.Variables { return types[path] })
	t.Cleanup(func() { ingame.SetTypes(nil) })
	dmm := newChunkTestMap(1, 1, 1)
	dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).InstancesAdd(inGamePrefab("/obj/effect/spawner/structure/window/reinforced",
		"icon", "'spawners.dmi'", "spawn_list", "list(/obj/structure/grille,/obj/structure/window/reinforced/fulltile)"))
	spawner := dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	units := inGameUnits(ingame.DmmMap(dmm), 1, 1, 1, spawner, nil)
	if len(units) != 2 || units[0].Instance() != spawner || units[1].Instance() != spawner {
		t.Fatalf("units = %d", len(units))
	}
	if units[0].Layer() >= units[1].Layer() {
		t.Fatal("parts did not take their own layers (grille below window)")
	}
	// Retained draws are invalidated by the icon a unit draws; the spawner's
	// own icon would leave the windows as placeholders after their DMI loads.
	if units[0].Icon() != "g.dmi" || units[1].Icon() != "w.dmi" {
		t.Fatalf("drawn icons = %q, %q; want the parts' icons", units[0].Icon(), units[1].Icon())
	}
}

func TestNeighborhoodCoversEdgesWithoutDuplicates(t *testing.T) {
	dmm := &dmmap.Dmm{MaxX: 3, MaxY: 3, MaxZ: 1}
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			dmm.Tiles = append(dmm.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}})
		}
	}
	got := ingame.Neighborhood(dmm, []util.Point{{X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}})
	if len(got) != 6 { // columns 1..3, rows 1..2
		t.Fatalf("neighbourhood = %v", got)
	}
}
