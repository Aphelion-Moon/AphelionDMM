package lighting

// AreaBase is an area's additive base lighting (lighting_area.dm:6-11).
type AreaBase struct {
	Color RGB     // base_lighting_color as 0..1 channels
	Alpha float64 // base_lighting_alpha, 0..255
}

// ResolveAmbient returns the additive ambient for one tile.
//
// Area base lighting is a BLEND_ADD overlay of base_lighting_color at
// base_lighting_alpha (lighting_area.dm:62-76). A space_lit turf in an area
// with no base lighting adds the starlight overlay itself
// (turf.dm:175-176); that fallback is dropped when starlight is disabled.
func ResolveAmbient(base AreaBase, spaceLit bool, star Starlight) RGB {
	if base.Alpha > 0 {
		a := clamp01(base.Alpha / 255)
		return RGB{base.Color.R * a, base.Color.G * a, base.Color.B * a}
	}
	if spaceLit && star.Enabled {
		return star.Color
	}
	return RGB{}
}

// StarlightSources returns the starlight emitters derived from Level.Space.
// A space tile emits only when one of its eight neighbours is not space
// (space.dm:106-113); cordons are not modelled.
func (l *Level) StarlightSources() []Source {
	return l.starlightSourcesIn(Rect{0, 0, l.W, l.H})
}

func (l *Level) starlightSourcesIn(scan Rect) []Source {
	if !l.Starlight.Enabled || l.Space == nil {
		return nil
	}
	scan = clipRect(scan, Rect{0, 0, l.W, l.H})
	var out []Source
	for y := scan.Y0; y < scan.Y1; y++ {
		for x := scan.X0; x < scan.X1; x++ {
			if !l.Space[y*l.W+x] || !l.hasNonSpaceNeighbour(x, y) {
				continue
			}
			out = append(out, Source{
				X: x, Y: y,
				Range: l.Starlight.Range, Power: l.Starlight.Power,
				Color: l.Starlight.Color, Dir: DirNorth, Angle: 360,
				Height: starlightHeight, System: SystemComplex,
			})
		}
	}
	return out
}

func (l *Level) hasNonSpaceNeighbour(x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= l.W || ny >= l.H {
				continue // off-map is not a turf
			}
			if !l.Space[ny*l.W+nx] {
				return true
			}
		}
	}
	return false
}
