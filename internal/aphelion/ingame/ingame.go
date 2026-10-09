// Package ingame predicts how connected atoms look in game: bitmask icon
// smoothing (walls, windows, carpets, tables), power cable links and smart
// atmospherics pipes. The editor otherwise draws mapper icons, such as a
// manifold4w for every smart pipe.
//
// The rules port Meridian-Rift's DM code:
//   - smoothing: code/__HELPERS/icon_smoothing.dm (bitmask_smooth,
//     set_smoothed_icon_state) and code/__DEFINES/icon_smoothing.dm;
//   - cables: code/modules/power/cable.dm (connect_cable, get_dir_string,
//     update_icon_state) and should_have_node overrides;
//   - pipes: code/modules/atmospherics/machinery/atmosmachinery.dm
//     (connection_check, is_connectable), each family's set_init_directions
//     and pipes/smart.dm (update_pipe_icon).
//
// It is display only: overrides never enter a map, history entry or
// collaboration message. Atoms whose appearance depends on procs this package
// does not model (border-object and proc-filtered smoothing) keep their
// mapper icon.
package ingame

import (
	"strings"
	"sync"
	"sync/atomic"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// Override is the in-game appearance chosen for one instance.
type Override struct {
	Icon      string
	IconState string
	Dir       int
}

// Directions as DM numbers them.
const (
	north = 1
	south = 2
	east  = 4
	west  = 8
)

var cardinals = [4]int{north, south, east, west} // GLOB.cardinals order

func reverse(dir int) int {
	out := 0
	if dir&north != 0 {
		out |= south
	}
	if dir&south != 0 {
		out |= north
	}
	if dir&east != 0 {
		out |= west
	}
	if dir&west != 0 {
		out |= east
	}
	return out
}

func step(dir int) (dx, dy int) {
	if dir&north != 0 {
		dy++
	}
	if dir&south != 0 {
		dy--
	}
	if dir&east != 0 {
		dx++
	}
	if dir&west != 0 {
		dx--
	}
	return dx, dy
}

func isType(path, root string) bool { return path == root || strings.HasPrefix(path, root+"/") }

// ---- toggle ----

var (
	enabled atomic.Bool
	version atomic.Uint64
	persist atomic.Pointer[func(bool)]
)

// Enabled reports whether the in-game look is on.
func Enabled() bool { return enabled.Load() }

// Version changes on every toggle, so panes know to rebuild their geometry.
func Version() uint64 { return version.Load() }

// SetEnabled switches the in-game look and persists the choice.
func SetEnabled(on bool) {
	if enabled.Swap(on) == on {
		return
	}
	version.Add(1)
	resetCache()
	if p := persist.Load(); p != nil {
		(*p)(on)
	}
}

// Apply sets the state at startup without persisting it again.
func Apply(on bool) {
	if enabled.Swap(on) != on {
		version.Add(1)
		resetCache()
	}
}

// SetPersist installs the preference writer.
func SetPersist(fn func(bool)) { persist.Store(&fn) }

// ---- per-prefab facts ----

// atom is what the rules need from one prefab, read once and cached by
// pointer (prefabs are immutable and interned).
type atom struct {
	path     string
	turf     bool
	area     bool
	movable  bool
	anchored bool

	// smoothing
	smoothFlags int
	baseState   string
	groups      []string
	canSmooth   []string
	brokenFloor bool
	areaLimit   string

	// cables
	cable       bool
	cableLayer  int
	bannedLinks int
	nodeSource  bool // a type that gives a cable on its tile a node
	terminal    bool
	smes        bool
	multilayer  bool

	// atmospherics
	atmos      bool
	smartPipe  bool
	heatPipe   bool
	heJunction bool
	initDirs   int
	pipeLayer  int
	pipeColor  string
	pipeFlags  int
	dir        int

	// structure spawners: what Initialize creates on the tile
	spawned []*dmmprefab.Prefab
}

var (
	cacheMu sync.Mutex
	cache   = map[*dmmprefab.Prefab]*atom{}
)

// Reset drops cached prefab facts. Call it when the environment changes:
// prefabs are freed and their inherited values may differ.
func Reset() { resetCache() }

func resetCache() {
	cacheMu.Lock()
	cache = map[*dmmprefab.Prefab]*atom{}
	cacheMu.Unlock()
}

func factsOf(p *dmmprefab.Prefab) *atom {
	cacheMu.Lock()
	a := cache[p]
	cacheMu.Unlock()
	if a != nil {
		return a
	}
	a = readFacts(p)
	cacheMu.Lock()
	cache[p] = a
	cacheMu.Unlock()
	return a
}

func readFacts(p *dmmprefab.Prefab) *atom {
	vars := p.Vars()
	path := p.Path()
	a := &atom{path: path}
	if vars == nil {
		return a
	}
	text := func(name string) string { return vars.TextV(name, "") }
	num := func(name string) int { return vars.IntV(name, 0) }
	a.turf = isType(path, "/turf")
	a.area = isType(path, "/area")
	a.movable = isType(path, "/obj") || isType(path, "/mob")
	a.anchored = num("anchored") != 0
	a.dir = vars.IntV("dir", south)

	a.smoothFlags = num("smoothing_flags")
	a.baseState = text("base_icon_state")
	a.groups = splitGroups(text("smoothing_groups"))
	a.canSmooth = splitGroups(text("canSmoothWith"))
	// PARSE_CAN_SMOOTH_WITH: object groups are negative and sorted first, and
	// any of them turns on SMOOTH_OBJ at init.
	if len(a.canSmooth) != 0 && strings.HasPrefix(a.canSmooth[0], "-") {
		a.smoothFlags |= smoothObj
	}
	a.brokenFloor = isType(path, "/turf/open/floor") && (num("broken") != 0 || num("burnt") != 0)
	if a.area {
		if raw, ok := vars.Value("area_limited_icon_smoothing"); ok && raw != "null" {
			a.areaLimit = strings.TrimSpace(raw)
		}
	}

	a.cable = isType(path, "/obj/structure/cable")
	a.multilayer = isType(path, "/obj/structure/cable/multilayer")
	a.cableLayer = num("cable_layer")
	a.bannedLinks = num("banned_links")
	a.terminal = isType(path, "/obj/machinery/power/terminal")
	a.smes = isType(path, "/obj/machinery/power/smes")
	a.nodeSource = givesCableNode(path, num)

	a.spawned = spawnedPrefabs(path, vars)

	a.atmos = isType(path, "/obj/machinery/atmospherics")
	if a.atmos {
		a.smartPipe = isType(path, "/obj/machinery/atmospherics/pipe/smart")
		a.heatPipe = isType(path, "/obj/machinery/atmospherics/pipe/heat_exchanging")
		a.heJunction = isType(path, "/obj/machinery/atmospherics/pipe/heat_exchanging/junction")
		a.pipeLayer = vars.IntV("piping_layer", pipingLayerDefault)
		a.pipeColor = strings.ToUpper(text("pipe_color"))
		a.pipeFlags = num("pipe_flags")
		a.initDirs = initDirections(path, a.dir, vars.IntV("initialize_directions", allCardinals))
	}
	return a
}

// splitGroups reads a smoothing group string such as "-1,58,".
func splitGroups(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// givesCableNode mirrors wire_node_generating_types (typecacheof: subtypes
// included) and the /obj/machinery/power should_have_node overrides.
func givesCableNode(path string, num func(string) int) bool {
	switch {
	case isType(path, "/obj/structure/grille"), isType(path, "/obj/structure/table/reinforced"):
		return true
	case isType(path, "/obj/machinery/power/smes"), isType(path, "/obj/machinery/power/solar"), isType(path, "/obj/machinery/power/terminal"):
		return true
	case isType(path, "/obj/machinery/power/emitter"):
		return num("welded") != 0
	case isType(path, "/obj/machinery/power/energy_accumulator"):
		return num("wants_powernet") != 0 && num("anchored") != 0
	case isType(path, "/obj/machinery/power/portagrav"), isType(path, "/obj/machinery/power/shieldwallgen"), isType(path, "/obj/machinery/power/port_gen"):
		return num("anchored") != 0
	}
	return false
}
