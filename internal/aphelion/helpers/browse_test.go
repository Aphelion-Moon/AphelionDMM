package helpers

import (
	"fmt"
	"reflect"
	"testing"
)

// airlockFixture mirrors the real shape: one family root, a few toggles and
// a large access/{all,any}/{department}/{role} branch.
func airlockFixture() []Helper {
	family := Root + "/airlock"
	h := func(rel, desc string) Helper {
		path := family
		if rel != "" {
			path += "/" + rel
		}
		name := rel
		if i := lastSlash(rel); i >= 0 {
			name = rel[i+1:]
		}
		if rel == "" {
			name = "airlock"
		}
		return Helper{Type: Type{Path: path, Name: name, Desc: desc}, Family: family, Relative: rel}
	}
	list := []Helper{h("", ""), h("locked", "Bolts the door."), h("welded", ""), h("unres/north", "")}
	for _, mode := range []string{"all", "any"} {
		for _, dept := range []string{"engineering", "medical"} {
			for r := 0; r < 3; r++ {
				list = append(list, h(fmt.Sprintf("access/%s/%s/role%d", mode, dept, r), ""))
			}
		}
	}
	list = append(list, Helper{Type: Type{Path: Root + "/broken_machine", Name: "broken_machine"}, Family: Root + "/broken_machine"})
	return list
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func TestOfferableDropsFamilyRootsWithMembers(t *testing.T) {
	got := Offerable(airlockFixture())
	for _, h := range got {
		if h.Path == Root+"/airlock" {
			t.Fatal("the abstract airlock family root is offered")
		}
	}
	if got[len(got)-1].Path != Root+"/broken_machine" {
		t.Fatal("a family that is only its root must stay offered")
	}
}

func TestCategoriesSplitLargeBranchesFromGeneral(t *testing.T) {
	cats := Categories(Offerable(airlockFixture()), 8)
	want := []Category{{Name: CategoryGeneral, Count: 4}, {Name: "airlock/access", Count: 12}}
	if !reflect.DeepEqual(cats, want) {
		t.Fatalf("categories = %+v, want %+v", cats, want)
	}
	if one := Categories(Offerable(airlockFixture())[:3], 8); len(one) != 1 {
		t.Fatalf("a single category = %+v", one)
	}
}

// The tree is rebuilt every frame; UI open state is keyed on Node.Key, so a
// group's key must not change between rebuilds or with filtering.
func TestBrowseKeysAreStableAcrossRebuilds(t *testing.T) {
	list := Offerable(airlockFixture())
	keys := func(root *Node) map[string]bool {
		out := map[string]bool{}
		var walk func(*Node)
		walk = func(n *Node) {
			for _, c := range n.Children {
				if c.Helper == nil {
					out[c.Key] = true
				}
				walk(c)
			}
		}
		walk(root)
		return out
	}
	first, second := keys(Browse(list, Filter{Large: 8})), keys(Browse(list, Filter{Large: 8}))
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("group keys = %v then %v", first, second)
	}
	for k := range first {
		if !second[k] {
			t.Fatalf("group key %q changed between rebuilds", k)
		}
	}
	if !first["airlock/access/all/engineering"] {
		t.Fatalf("keys are not paths: %v", first)
	}
	for k := range keys(Browse(list, Filter{Query: "medical", Large: 8})) {
		if !first[k] {
			t.Fatalf("filtering produced a new key %q", k)
		}
	}
}

func TestBrowseTreeFiltersAndOrdersTogglesFirst(t *testing.T) {
	list := Offerable(airlockFixture())
	root := Browse(list, Filter{Large: 8})
	if root.Count != 16 || len(root.Children) != 2 {
		t.Fatalf("root count %d, families %d", root.Count, len(root.Children))
	}
	airlock := root.Children[0]
	var names []string
	for _, c := range airlock.Children {
		names = append(names, c.Name)
	}
	// Leaves first, then groups, each alphabetical.
	if want := []string{"locked", "welded", "access", "unres"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("airlock children = %v, want %v", names, want)
	}

	// Every word must match path, name or description, in any order.
	found := Browse(list, Filter{Query: "medical any", Large: 8})
	if found.Count != 3 {
		t.Fatalf("'medical any' matched %d, want 3", found.Count)
	}
	if found := Browse(list, Filter{Query: "bolts", Large: 8}); found.Count != 1 {
		t.Fatalf("description search matched %d", found.Count)
	}

	general := Browse(list, Filter{Category: CategoryGeneral, Large: 8})
	if general.Count != 4 {
		t.Fatalf("general category = %d", general.Count)
	}
	access := Browse(list, Filter{Category: "airlock/access", Query: "role1", Large: 8})
	if access.Count != 4 {
		t.Fatalf("access + role1 = %d", access.Count)
	}
}
