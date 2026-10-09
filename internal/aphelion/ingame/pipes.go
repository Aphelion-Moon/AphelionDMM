package ingame

import "strconv"

// atmos_piping.dm
const (
	pipingLayerDefault   = 3
	allCardinals         = north | south | east | west
	pipingAllLayer       = 1 << 0
	pipingAllColors      = 1 << 4
	pipingDistroAndWaste = 1 << 6
	atmosColorOmni       = "#EEEEEE" // ATMOS_COLOR_OMNI = COLOR_VERY_LIGHT_GRAY
	pipeBitmaskIcon      = "icons/obj/pipes_n_cables/!pipes_bitmask.dmi"
	heatExchangeRoot     = "/obj/machinery/atmospherics/pipe/heat_exchanging"
	smartPipeDeviceNodes = 4
	atmosMachineryRoot   = "/obj/machinery/atmospherics"
	componentsRoot       = atmosMachineryRoot + "/components"
	pipesRoot            = atmosMachineryRoot + "/pipe"
)

// initDirections mirrors each family's set_init_directions for a mapped atom.
// fallback is the type's initialize_directions var.
func initDirections(path string, dir, fallback int) int {
	straight := func() int {
		if dir == north || dir == south {
			return north | south
		}
		return east | west
	}
	switch {
	case isType(path, pipesRoot+"/smart"):
		if fallback == 0 {
			return allCardinals
		}
		return fallback
	case isType(path, heatExchangeRoot+"/simple"):
		if dir != north && dir != south && dir != east && dir != west {
			return dir // diagonal bends
		}
		return straight()
	case isType(path, heatExchangeRoot+"/manifold4w"):
		return fallback
	case isType(path, heatExchangeRoot+"/manifold"):
		return allCardinals &^ dir
	case isType(path, heatExchangeRoot+"/junction"),
		isType(path, pipesRoot+"/bridge_pipe"),
		isType(path, pipesRoot+"/color_adapter"),
		isType(path, pipesRoot+"/layer_manifold"):
		return straight()
	case isType(path, pipesRoot+"/multiz"):
		return dir
	case isType(path, componentsRoot+"/unary"):
		return dir
	case isType(path, componentsRoot+"/binary/circulator"):
		if dir == north || dir == south {
			return east | west
		}
		return north | south
	case isType(path, componentsRoot+"/binary"):
		return straight()
	case isType(path, componentsRoot+"/trinary"):
		switch dir {
		case north:
			return east | north | south
		case south:
			return south | west | north
		case east:
			return east | west | south
		case west:
			return west | north | east
		}
	}
	return fallback
}

// connectable mirrors connection_check(target, piping_layer) from a smart
// pipe toward target, which lies in direction d from it.
func connectable(pipe, target *atom, d int) bool {
	if !target.atmos {
		return false
	}
	multiz := isType(target.path, pipesRoot+"/multiz")
	if !multiz && (pipe.initDirs&d == 0 || target.initDirs&reverse(d) == 0) {
		return false
	}
	// Heat exchange pipes only take heat exchange pipes; a junction takes a
	// normal pipe on the side its dir points at (he_pipes.dm, junction.dm).
	// junction.dm: dir == get_dir(target, src), the direction from us to it.
	if target.heatPipe && (!target.heJunction || target.dir != d) {
		return false
	}
	layer := pipe.pipeLayer
	if target.pipeFlags&pipingDistroAndWaste != 0 {
		if layer%2 == 1 {
			return false
		}
	} else if target.pipeLayer != layer && target.pipeFlags&pipingAllLayer == 0 {
		return false
	}
	if target.pipeColor != pipe.pipeColor && (target.pipeFlags|pipe.pipeFlags)&pipingAllColors == 0 &&
		target.pipeColor != atmosColorOmni && pipe.pipeColor != atmosColorOmni {
		return false
	}
	return true
}

// smartPipe mirrors /obj/machinery/atmospherics/pipe/smart/update_pipe_icon.
func smartPipe(m Map, x, y, z int, a *atom) (Override, bool) {
	connections, nodes := 0, 0
	for _, d := range cardinals {
		if a.initDirs&d == 0 || nodes == smartPipeDeviceNodes {
			continue
		}
		dx, dy := step(d)
		neighbor, ok := facts(m, x+dx, y+dy, z)
		if !ok {
			continue
		}
		for _, n := range neighbor {
			if connectable(a, n, d) {
				connections |= d
				nodes++
				break
			}
		}
	}
	dir := connections
	switch connections {
	case east | west:
		dir = east
	case north | south:
		dir = south
	}
	bitfield := connections
	if isStub(connections) {
		add := 0
		if connections != 0 {
			add |= reverse(connections) & a.initDirs
		}
		for shift := 0; isStub(connections|add) && a.initDirs>>shift != 0; shift++ {
			if candidate := a.initDirs & (1 << shift); candidate&connections == 0 {
				add |= candidate
			}
		}
		bitfield |= add << 4 // CARDINAL_TO_SHORTPIPES
	}
	if dir == 0 {
		dir = south
	}
	return Override{Icon: pipeBitmaskIcon, IconState: strconv.Itoa(bitfield) + "_" + strconv.Itoa(a.pipeLayer), Dir: dir}, true
}

// isStub is ISSTUB: zero or one direction bit.
func isStub(bits int) bool { return bits&(bits-1) == 0 }
