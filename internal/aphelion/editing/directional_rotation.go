package editing

import (
	"strings"

	"sdmm/internal/dmapi/dmvars"
)

// PrefabLookup reads type defaults from one immutable loaded environment.
type PrefabLookup func(path string) *dmvars.Variables

// Directional mapping helpers declare an abstract family and named direction
// children. Only switch to a declared matching child; unknown types retain the
// ordinary numeric transform and never acquire a guessed path.
func directionalVariant(path string, direction int, lookups []PrefabLookup) (string, *dmvars.Variables) {
	if len(lookups) == 0 || lookups[0] == nil {
		return "", nil
	}
	lookup := lookups[0]
	slash := strings.LastIndexByte(path, '/')
	if slash <= 0 || lookup(path) == nil {
		return "", nil
	}
	family := path[:slash]
	parent := lookup(family)
	if parent == nil || parent.ValueV("abstract_type", "") != family {
		// Legacy maps can store the base type with a dir override instead of
		// its helper. Do not search unrelated semantic subtypes for variants.
		family = path + "/directional"
		parent = lookup(family)
		if parent == nil || parent.ValueV("abstract_type", "") != family {
			return "", nil
		}
	}
	name := map[int]string{1: "north", 2: "south", 4: "east", 8: "west", 5: "northeast", 9: "northwest", 6: "southeast", 10: "southwest"}[direction]
	if name == "" {
		return "", nil
	}
	target := family + "/" + name
	vars := lookup(target)
	if vars == nil {
		return "", nil
	}
	if declared, ok := parseDirection(vars.ValueV("dir", "")); !ok || declared != direction {
		return "", nil
	}
	return target, vars
}
