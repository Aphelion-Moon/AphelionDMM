package ingame

import (
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// neonType mirrors a tg simple neon carpet (fancy_floor.dm): a dark smoothed
// base whose glowing pattern is a decal added at Initialize from neon_icon,
// neon_icon_state, neon_color and the turf's alpha, at smoothing_junction.
func neonType() *dmvars.Variables {
	v := &dmvars.MutableVariables{}
	for _, kv := range [][2]string{
		{"icon", "'icons/turf/floors/carpet_neon_base.dmi'"},
		{"icon_state", `"base-255"`},
		{"base_icon_state", `"base"`},
		{"smoothing_flags", "1"},
		{"smoothing_groups", `"0,47,"`},
		{"canSmoothWith", `"47,"`},
		{"neon_icon", "'icons/turf/floors/carpet_neon_light.dmi'"},
		{"neon_icon_state", `"light"`},
		{"neon_color", `"#FF00FF"`},
		{"alpha", "255"},
		{"layer", "2"},
	} {
		v.Put(kv[0], kv[1])
	}
	return v.ToImmutable()
}

func neonCarpet(parent *dmvars.Variables, kv ...string) *dmmprefab.Prefab {
	v := dmvars.FromParent(parent)
	for i := 0; i+1 < len(kv); i += 2 {
		v = dmvars.Set(v, kv[i], kv[i+1])
	}
	return dmmprefab.New(dmmprefab.IdNone, "/turf/open/floor/carpet/neon/simple/pink", v)
}

func TestNeonCarpetDrawsItsPatternFromItsOwnVars(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(3, 1)
	carpet := neonCarpet(neonType())
	for x := 1; x <= 3; x++ {
		m.put(x, 1, carpet)
	}
	parts := Overlays(m, 2, 1, 1, carpet)
	if len(parts) != 1 {
		t.Fatalf("overlays = %d, want the neon pattern", len(parts))
	}
	o, vars := parts[0].Override, parts[0].Prefab.Vars()
	// East and west carpet: junction 4|8 = 12, the same one the base smooths to.
	if o.Icon != "icons/turf/floors/carpet_neon_light.dmi" || o.IconState != "light-12" {
		t.Fatalf("pattern = %q %q", o.Icon, o.IconState)
	}
	if base := resolve(t, m, 2, 1, carpet); base.IconState != "base-12" {
		t.Fatalf("base = %q", base.IconState)
	}
	if got := vars.TextV("color", ""); got != "#FF00FF" {
		t.Fatalf("pattern colour %q, want neon_color", got)
	}
	if got := vars.FloatV("alpha", 0); got != 255 {
		t.Fatalf("pattern alpha %v", got)
	}
	if got := vars.FloatV("layer", 0); got != 2 {
		t.Fatalf("pattern must share the turf's layer, got %v", got)
	}

	// A map-edited colour is the instance's own neon_color.
	edited := neonCarpet(neonType(), "neon_color", `"#00FFFF"`, "alpha", "128")
	m.put(1, 1, edited)
	parts = Overlays(m, 1, 1, 1, edited)
	if len(parts) != 1 || parts[0].Prefab.Vars().TextV("color", "") != "#00FFFF" || parts[0].Prefab.Vars().FloatV("alpha", 0) != 128 {
		t.Fatalf("edited neon carpet = %+v", parts)
	}
}

// neon_icon || icon and neon_icon_state || base_icon_state; without smoothing
// the decal uses the state as is (smoothing_junction is null).
func TestNeonCarpetFallsBackToItsOwnIconAndBaseState(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(1, 1)
	carpet := neonCarpet(neonType(), "neon_icon", "null", "neon_icon_state", "null", "smoothing_flags", "0")
	m.put(1, 1, carpet)
	parts := Overlays(m, 1, 1, 1, carpet)
	if len(parts) != 1 {
		t.Fatalf("overlays = %d", len(parts))
	}
	if o := parts[0].Override; o.Icon != "icons/turf/floors/carpet_neon_base.dmi" || o.IconState != "base" {
		t.Fatalf("fallback pattern = %q %q", o.Icon, o.IconState)
	}
}

func TestOrdinaryTurfsHaveNoNeonOverlay(t *testing.T) {
	t.Cleanup(resetCache)
	m := newMap(1, 1)
	carpet := prefab("/turf/open/floor/carpet", "smoothing_flags", "1", "base_icon_state", `"carpet"`, "smoothing_groups", `"47,"`, "canSmoothWith", `"47,"`)
	m.put(1, 1, carpet)
	if parts := Overlays(m, 1, 1, 1, carpet); parts != nil {
		t.Fatalf("plain carpet got %+v", parts)
	}
	if parts := Overlays(m, 1, 1, 1, nil); parts != nil {
		t.Fatal("nil prefab")
	}
}
