package tilemenu

import (
	"testing"

	"sdmm/internal/aphelion/helpers"
)

func TestHelperTreeNestsByFamilyAndPath(t *testing.T) {
	family := helpers.Root + "/airlock"
	list := []helpers.Helper{
		{Type: helpers.Type{Path: family + "/access/all/engineering"}, Family: family, Relative: "access/all/engineering"},
		{Type: helpers.Type{Path: family + "/access/all/medical"}, Family: family, Relative: "access/all/medical"},
		{Type: helpers.Type{Path: family + "/locked"}, Family: family, Relative: "locked"},
		{Type: helpers.Type{Path: helpers.Root + "/apc"}, Family: helpers.Root + "/apc", Relative: ""},
	}
	root := helperTree(list)
	if len(root.children) != 2 {
		t.Fatalf("families = %d", len(root.children))
	}
	airlock := root.children[0]
	if airlock.name != "airlock" || len(airlock.children) != 2 {
		t.Fatalf("airlock node = %+v", airlock)
	}
	all := airlock.children[0].children[0]
	if all.name != "all" || len(all.children) != 2 || all.children[0].helper == nil {
		t.Fatalf("access/all = %+v", all)
	}
	// A family that is itself a helper is a leaf at its own name.
	if apc := root.children[1]; apc.name != "apc" || apc.helper == nil {
		t.Fatalf("apc = %+v", apc)
	}
}
