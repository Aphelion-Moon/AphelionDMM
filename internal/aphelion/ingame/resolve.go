package ingame

import (
	"strconv"
	"strings"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// Map reads tile contents for the resolver. Prefabs returns nil and false for
// a coordinate outside the map.
type Map interface {
	Prefabs(x, y, z int) ([]*dmmprefab.Prefab, bool)
}

// Resolve returns the in-game appearance of prefab at (x, y, z), or false
// when the mapper icon already is the in-game look or the rules do not apply.
func Resolve(m Map, x, y, z int, prefab *dmmprefab.Prefab) (Override, bool) {
	if prefab == nil || m == nil {
		return Override{}, false
	}
	a := factsOf(prefab)
	switch {
	case a.smartPipe:
		return smartPipe(m, x, y, z, a)
	case a.cable && !a.multilayer:
		return cable(m, x, y, z, a)
	case a.smoothFlags&usesSmoothing != 0 && a.smoothFlags&(smoothBorderObject|smoothProcFilter) == 0 && a.baseState != "":
		return smooth(m, x, y, z, a)
	}
	return Override{}, false
}

// Affects reports whether Resolve can change prefab's look, so callers can
// keep reusing appearances for everything else.
func Affects(prefab *dmmprefab.Prefab) bool {
	if prefab == nil {
		return false
	}
	a := factsOf(prefab)
	return len(a.spawned) != 0 || a.smartPipe || (a.cable && !a.multilayer) ||
		(a.smoothFlags&usesSmoothing != 0 && a.smoothFlags&(smoothBorderObject|smoothProcFilter) == 0 && a.baseState != "")
}

func facts(m Map, x, y, z int) ([]*atom, bool) {
	prefabs, ok := m.Prefabs(x, y, z)
	if !ok {
		return nil, false
	}
	out := make([]*atom, 0, len(prefabs))
	for _, p := range prefabs {
		a := factsOf(p)
		out = append(out, a)
		// Spawned atoms exist in game; neighbours connect to them.
		for _, s := range a.spawned {
			out = append(out, factsOf(s))
		}
	}
	return out, true
}

// ---- icon smoothing (icon_smoothing.dm) ----

const (
	smoothBitmask         = 1 << 0
	smoothBitmaskCardinal = 1 << 1
	smoothDiagonalCorners = 1 << 2
	smoothBorder          = 1 << 3
	smoothObj             = 1 << 5
	smoothBorderObject    = 1 << 6
	smoothProcFilter      = 1 << 7
	usesSmoothing         = smoothBitmask | smoothBitmaskCardinal

	northEastJunction = 1 << 4
	southEastJunction = 1 << 5
	southWestJunction = 1 << 6
	northWestJunction = 1 << 7
)

func smooth(m Map, x, y, z int, a *atom) (Override, bool) {
	if a.brokenFloor {
		return Override{}, false // set_smoothed_icon_state returns early
	}
	here, _ := facts(m, x, y, z)
	limit := ""
	for _, h := range here {
		if h.area {
			limit = h.areaLimit
		}
	}
	found := func(dir int) bool {
		dx, dy := step(dir)
		neighbor, ok := facts(m, x+dx, y+dy, z)
		if !ok {
			return a.smoothFlags&smoothBorder != 0
		}
		if limit != "" && !areaWithin(neighbor, limit) {
			return a.smoothFlags&smoothBorder != 0
		}
		for _, n := range neighbor {
			if n.turf && n == lastTurf(neighbor) && n.groups != nil && intersects(a.canSmooth, n.groups) {
				return true
			}
		}
		if a.smoothFlags&smoothObj != 0 {
			for _, n := range neighbor {
				if n.movable && n.anchored && n.groups != nil && intersects(a.canSmooth, n.groups) {
					return true
				}
			}
		}
		return false
	}
	junction := 0
	for _, d := range cardinals {
		if found(d) {
			junction |= d
		}
	}
	if a.smoothFlags&smoothBitmaskCardinal == 0 && junction&(north|south) != 0 && junction&(east|west) != 0 {
		if junction&north != 0 {
			if junction&west != 0 && found(north|west) {
				junction |= northWestJunction
			}
			if junction&east != 0 && found(north|east) {
				junction |= northEastJunction
			}
		}
		if junction&south != 0 {
			if junction&west != 0 && found(south|west) {
				junction |= southWestJunction
			}
			if junction&east != 0 && found(south|east) {
				junction |= southEastJunction
			}
		}
	}
	state := a.baseState + "-" + strconv.Itoa(junction)
	if a.turf && isType(a.path, "/turf/closed") && a.smoothFlags&smoothDiagonalCorners != 0 && diagonalJunction(junction) {
		state += "-diagonal" // the underlay a real wall copies is not drawn
	}
	return Override{IconState: state, Dir: a.dir}, true
}

// lastTurf is the tile's turf; with stacked turfs DM keeps the last one.
func lastTurf(tile []*atom) *atom {
	for i := len(tile) - 1; i >= 0; i-- {
		if tile[i].turf {
			return tile[i]
		}
	}
	return nil
}

func areaWithin(tile []*atom, limit string) bool {
	for _, t := range tile {
		if t.area {
			return isType(t.path, limit)
		}
	}
	return false
}

func diagonalJunction(j int) bool {
	switch j {
	case north | west, north | east, south | west, south | east,
		north | west | northWestJunction, north | east | northEastJunction,
		south | west | southWestJunction, south | east | southEastJunction:
		return true
	}
	return false
}

// ---- power cables (cable.dm) ----

func cable(m Map, x, y, z int, a *atom) (Override, bool) {
	here, _ := facts(m, x, y, z)
	underTerminal, underSmes, node := false, false, a.bannedLinks != 0
	for _, h := range here {
		underTerminal = underTerminal || h.terminal
		underSmes = underSmes || h.smes
		node = node || h.nodeSource
	}
	if underTerminal {
		underSmes = false // connect_cable checks for a terminal first
	}
	links := 0
	for _, d := range cardinals {
		if d&a.bannedLinks != 0 {
			continue
		}
		dx, dy := step(d)
		neighbor, ok := facts(m, x+dx, y+dy, z)
		if !ok {
			continue
		}
		skip := false
		for _, n := range neighbor {
			// Never link an SMES to its own terminal.
			if (underSmes && n.terminal) || (underTerminal && n.smes) {
				skip = true
			}
		}
		if skip {
			continue
		}
		for _, n := range neighbor {
			if n.cable && n.cableLayer&a.cableLayer != 0 && n.bannedLinks&reverse(d) == 0 {
				links |= d
				break
			}
		}
	}
	layer := strconv.Itoa(a.cableLayer)
	if links == 0 {
		return Override{IconState: "l" + layer + "-noconnection", Dir: south}, true
	}
	var parts []string
	for _, d := range cardinals {
		if links&d != 0 {
			parts = append(parts, strconv.Itoa(d))
		}
	}
	state := "l" + layer + "-" + strings.Join(parts, "-")
	if len(parts) > 1 && node {
		state += "-node"
	}
	return Override{IconState: state, Dir: south}, true
}
