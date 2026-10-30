package ingame

import (
	"strconv"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// Neon carpets (/turf/open/floor/carpet/neon, fancy_floor.dm) map with a
// bare base icon. Their glowing pattern is added at Initialize as a decal
// element:
//
//	AddElement(/datum/element/decal, neon_icon || icon,
//		neon_icon_state || base_icon_state, dir, null, null, alpha,
//		neon_color, smoothing_junction)
//
// and re-added whenever the junction changes. The decal draws
// "[state]-[smoothing_junction]", or the plain state while the junction is
// null (datums/elements/decals/_decal.dm). A second, emissive copy makes the
// pattern glow in the dark; the in-game look draws the coloured copy only.
//
// The rule keys on the type declaring neon_icon or neon_icon_state, so every
// subtype and downstream variant is read from its own variables.

// neonDecal is what a neon turf's pattern needs, read once per prefab.
type neonDecal struct {
	icon, state string
	junction    string // explicit smoothing_junction, "" when null
	part        *dmmprefab.Prefab
}

func readNeon(path string, vars *dmvars.Variables) *neonDecal {
	if !isType(path, "/turf") {
		return nil
	}
	_, hasIcon := vars.Value("neon_icon")
	_, hasState := vars.Value("neon_icon_state")
	if !hasIcon && !hasState {
		return nil
	}
	d := &neonDecal{
		icon:  vars.TextV("neon_icon", vars.TextV("icon", "")),
		state: vars.TextV("neon_icon_state", vars.TextV("base_icon_state", "")),
	}
	if raw, ok := vars.Value("smoothing_junction"); ok && raw != dmvars.NullValue {
		d.junction = raw
	}
	if d.icon == "" || d.state == "" {
		return nil
	}
	// The decal is drawn with the turf's placement and layer, coloured by
	// neon_color at the turf's alpha (neon_alpha where a codebase declares it).
	alpha := vars.ValueV("alpha", "255")
	if raw, ok := vars.Value("neon_alpha"); ok && raw != dmvars.NullValue {
		alpha = raw
	}
	part := dmvars.Set(dmvars.FromParent(vars), "color", vars.ValueV("neon_color", dmvars.NullValue))
	part = dmvars.Set(part, "alpha", alpha)
	d.part = dmmprefab.New(dmmprefab.IdNone, path, part)
	return d
}

// Overlays returns what prefab at (x, y, z) draws over its own appearance in
// game, in drawing order, or nil. Callers draw prefab first (its Resolve
// appearance or mapper icon), then these parts.
func Overlays(m Map, x, y, z int, prefab *dmmprefab.Prefab) []Part {
	if prefab == nil || m == nil {
		return nil
	}
	a := factsOf(prefab)
	if a.neon == nil {
		return nil
	}
	state := a.neon.state
	switch {
	case a.smooths() && !a.brokenFloor:
		state += "-" + strconv.Itoa(smoothingJunction(m, x, y, z, a))
	case a.neon.junction != "":
		state += "-" + a.neon.junction
	}
	return []Part{{Prefab: a.neon.part, Override: Override{Icon: a.neon.icon, IconState: state, Dir: a.dir}}}
}
