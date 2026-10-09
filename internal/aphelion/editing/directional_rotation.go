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
	family, ok := directionalFamily(path, lookups)
	if !ok {
		return "", nil
	}
	lookup := lookups[0]
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

// directionalFamily finds the declared helper family a path belongs to: either
// the path's own parent family, or the family under a legacy base type.
func directionalFamily(path string, lookups []PrefabLookup) (string, bool) {
	if len(lookups) == 0 || lookups[0] == nil {
		return "", false
	}
	lookup := lookups[0]
	slash := strings.LastIndexByte(path, '/')
	if slash <= 0 || lookup(path) == nil {
		return "", false
	}
	family := path[:slash]
	if isDeclaredFamily(lookup, family) {
		return family, true
	}
	// Legacy maps can store the base type with a dir override instead of
	// its helper. Do not search unrelated semantic subtypes for variants.
	family = path + "/directional"
	if isDeclaredFamily(lookup, family) {
		return family, true
	}
	return "", false
}

func isDeclaredFamily(lookup PrefabLookup, family string) bool {
	vars := lookup(family)
	if vars == nil {
		return false
	}
	declared, ok := vars.Value("abstract_type")
	return ok && normalizeTypepath(declared) == family
}

// normalizeTypepath tolerates the spellings a parsed typepath value can take.
func normalizeTypepath(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	if len(raw) > 1 {
		raw = strings.TrimRight(raw, "/")
	}
	return raw
}
