package app

import (
	"testing"

	"sdmm/internal/dmapi/dmenv"
)

// The environment-backed filter is the one the map renderer and editing tools
// consult, so an unknown area must be hidden by the /area visibility toggle.
func TestNewPathsFilterHidesUnknownTypesWithKnownAncestor(t *testing.T) {
	env := &dmenv.Dme{Objects: map[string]*dmenv.Object{
		"/area":        {Path: "/area", DirectChildren: []string{"/area/engine"}},
		"/area/engine": {Path: "/area/engine"},
		"/turf":        {Path: "/turf"},
	}}
	filter := newPathsFilter(env)
	filter.TogglePath("/area")

	if !filter.IsHiddenPath("/area/engine/break_room") {
		t.Fatal("an unknown /area type should be hidden with /area")
	}
	if filter.IsHiddenPath("/turf/removed") {
		t.Fatal("an unknown /turf type should follow /turf, which is shown")
	}
}
