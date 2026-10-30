package lighting

import "strings"

// EdgeLight is a turf light the game switches on at runtime only where the
// turf borders something that is not of its own family. The tg case is lava
// (and plasma lava on ice moons): lava.dm declares light_on = FALSE and
// refresh_light() enables the light only when a turf in RANGE_TURFS(1) is not
// islava(). A pool interior is lit by its emissive mask instead (see
// Level.Emissive). Like starlight, edge lights are derived from the level
// during computation, so they follow neighbour edits and are not subject to
// the explicit source cap.
type EdgeLight struct {
	// Family groups turf types that do not make each other borders (every
	// /turf/open/lava subtype is islava). It must be positive.
	Family int
	// Emits is false for a family member without a usable light; it still
	// counts as the family for its neighbours.
	Emits bool
	// Source is the light; X and Y are filled per tile.
	Source Source
}

// EdgeSources returns the edge lights that are switched on, in row-major order.
func (l *Level) EdgeSources() []Source { return l.edgeSourcesIn(Rect{0, 0, l.W, l.H}) }

func (l *Level) edgeAt(i int) *EdgeLight {
	k := int(l.Edge[i])
	if k == 0 || k > len(l.EdgeLights) {
		return nil
	}
	return &l.EdgeLights[k-1]
}

func (l *Level) edgeSourcesIn(scan Rect) []Source {
	if l.Edge == nil || len(l.EdgeLights) == 0 {
		return nil
	}
	scan = clipRect(scan, Rect{0, 0, l.W, l.H})
	var out []Source
	for y := scan.Y0; y < scan.Y1; y++ {
		for x := scan.X0; x < scan.X1; x++ {
			e := l.edgeAt(y*l.W + x)
			if e == nil || !e.Emits || !l.bordersOtherFamily(x, y, e.Family) {
				continue
			}
			s := e.Source
			s.X, s.Y = x, y
			out = append(out, s)
		}
	}
	return out
}

// bordersOtherFamily reports whether one of the eight neighbours on the level
// is not of family. Off-map is not a turf (RANGE_TURFS clamps to the world);
// the turfs above and below a multi-z stack are not modelled.
func (l *Level) bordersOtherFamily(x, y, family int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= l.W || ny >= l.H {
				continue
			}
			if e := l.edgeAt(ny*l.W + nx); e == nil || e.Family != family {
				return true
			}
		}
	}
	return false
}

// EdgeProfile names a family of runtime edge lights. Every type under
// PathPrefix belongs to the family (islava is istype(A, /turf/open/lava)).
type EdgeProfile struct {
	PathPrefix string
	// EmissiveVar names an icon state variable. When it is set and not null
	// the turf draws an emissive mask over the tile (lava.dm update_overlays:
	// emissive_appearance(mask_icon, mask_state)).
	EmissiveVar string
}

// DefaultEdgeProfiles returns the built-in families: lava, including plasma
// lava on ice moons.
func DefaultEdgeProfiles() []EdgeProfile {
	return []EdgeProfile{{PathPrefix: "/turf/open/lava", EmissiveVar: "mask_state"}}
}

// EdgeExtraction is the outcome of reading one turf against the edge profiles.
type EdgeExtraction struct {
	Light    EdgeLight
	Emissive bool
	// Skip explains why a family member has no light (Light.Emits false).
	// Unparsable is set when a variable could not be understood.
	Skip       string
	Unparsable bool
}

// ExtractEdge reads a turf that belongs to an edge family. Its light comes
// from its own light variables like any atom; only light_on is ignored when
// it is false, because the game switches it on at the family border. A type
// whose light_on is already true emits everywhere and is left to
// ExtractSource (ok false).
func ExtractEdge(a Atom, profiles []EdgeProfile) (EdgeExtraction, bool) {
	family := 0
	var prof *EdgeProfile
	for i := range profiles {
		if pathHasPrefix(a.Path, profiles[i].PathPrefix) {
			family, prof = i+1, &profiles[i]
			break
		}
	}
	if prof == nil {
		return EdgeExtraction{}, false
	}
	get := a.Var
	if get == nil {
		get = func(string) (string, bool) { return "", false }
	}
	on, e := boolVar(get, "light_on", true)
	if e == "" && on {
		return EdgeExtraction{}, false
	}
	out := EdgeExtraction{Light: EdgeLight{Family: family}}
	if prof.EmissiveVar != "" {
		if raw, ok := get(prof.EmissiveVar); ok && !isNull(raw) && strings.Trim(strings.TrimSpace(raw), `"'`) != "" {
			out.Emissive = true
		}
	}
	forced := func(name string) (string, bool) {
		if name == "light_on" {
			return "TRUE", true
		}
		return get(name)
	}
	ex := ExtractSource(Atom{Path: a.Path, X: a.X, Y: a.Y, Var: forced}, nil)
	if !ex.Emits {
		out.Skip, out.Unparsable = ex.Reason, ex.Unparsable
		return out, true
	}
	out.Light.Emits = true
	out.Light.Source = ex.Source
	out.Light.Source.X, out.Light.Source.Y = 0, 0
	return out, true
}
