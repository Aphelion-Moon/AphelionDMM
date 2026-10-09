package helpers

import (
	"sort"
	"strings"
)

// Offerable drops family roots that only exist to be subtyped (the plain
// airlock helper) when the family has members; a family that is only its root,
// such as broken_machine, stays.
func Offerable(list []Helper) []Helper {
	members := map[string]int{}
	for _, h := range list {
		if h.Relative != "" {
			members[h.Family]++
		}
	}
	out := make([]Helper, 0, len(list))
	for _, h := range list {
		if h.Relative == "" && members[h.Family] > 0 {
			continue
		}
		out = append(out, h)
	}
	return out
}

// CategoryGeneral collects helpers outside any large branch.
const CategoryGeneral = "General"

// Category is one filter chip.
type Category struct {
	Name  string // CategoryGeneral, or "family/branch" such as "airlock/access"
	Count int
}

// category is the chip h belongs to: its family and first path segment when
// that branch holds at least large helpers, otherwise General.
func category(h Helper, sizes map[string]int, large int) string {
	if branch := branchOf(h); branch != "" && sizes[branch] >= large {
		return branch
	}
	return CategoryGeneral
}

func branchOf(h Helper) string {
	first, _, nested := strings.Cut(h.Relative, "/")
	if !nested {
		return ""
	}
	return strings.TrimPrefix(h.Family, Root+"/") + "/" + first
}

func branchSizes(list []Helper) map[string]int {
	sizes := map[string]int{}
	for _, h := range list {
		if b := branchOf(h); b != "" {
			sizes[b]++
		}
	}
	return sizes
}

// Categories lists the chips for list, General first, then large branches by
// name. Branches smaller than large fold into General.
func Categories(list []Helper, large int) []Category {
	sizes := branchSizes(list)
	counts := map[string]int{}
	for _, h := range list {
		counts[category(h, sizes, large)]++
	}
	var out []Category
	if n := counts[CategoryGeneral]; n > 0 {
		out = append(out, Category{Name: CategoryGeneral, Count: n})
	}
	var names []string
	for name := range counts {
		if name != CategoryGeneral {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, Category{Name: name, Count: counts[name]})
	}
	return out
}

// Filter narrows a browse.
type Filter struct {
	// Query words must all appear, in any order, in the helper's path, name
	// or description (case-insensitive).
	Query string
	// Category is a chip name; empty means every category.
	Category string
	// Large is the branch size that earns its own chip.
	Large int
}

// Node is one level of the browse tree: a family, a path segment, or a helper.
type Node struct {
	Name string
	// Key is the node's path from the root ("airlock/access/all"). It is
	// stable across rebuilds and filters, so UIs can key open state on it.
	Key      string
	Helper   *Helper
	Children []*Node
	Count    int // helpers at or below this node
}

// Browse builds the tree of helpers that pass f: families, then path
// segments. Within a level, helpers come before groups, each by name.
func Browse(list []Helper, f Filter) *Node {
	words := strings.Fields(strings.ToLower(f.Query))
	sizes := branchSizes(list)
	root := &Node{}
	for i := range list {
		h := &list[i]
		if f.Category != "" && category(*h, sizes, f.Large) != f.Category {
			continue
		}
		if !matches(*h, words) {
			continue
		}
		node := root.child(strings.TrimPrefix(h.Family, Root+"/"))
		root.Count++
		node.Count++
		if h.Relative == "" {
			leaf := node.child(node.Name)
			leaf.Helper, leaf.Count = h, 1
			continue
		}
		segments := strings.Split(h.Relative, "/")
		for _, segment := range segments[:len(segments)-1] {
			node = node.child(segment)
			node.Count++
		}
		leaf := node.child(segments[len(segments)-1])
		leaf.Helper, leaf.Count = h, 1
	}
	root.sort()
	return root
}

func matches(h Helper, words []string) bool {
	if len(words) == 0 {
		return true
	}
	text := strings.ToLower(h.Path + " " + h.Name + " " + h.Desc)
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

func (n *Node) child(name string) *Node {
	for _, c := range n.Children {
		// A helper and a group may share a name (unres and unres/north).
		if c.Name == name && c.Helper == nil {
			return c
		}
	}
	key := name
	if n.Key != "" {
		key = n.Key + "/" + name
	}
	c := &Node{Name: name, Key: key}
	n.Children = append(n.Children, c)
	return c
}

func (n *Node) sort() {
	sort.SliceStable(n.Children, func(a, b int) bool {
		x, y := n.Children[a], n.Children[b]
		if (x.Helper != nil) != (y.Helper != nil) {
			return x.Helper != nil
		}
		return x.Name < y.Name
	})
	for _, c := range n.Children {
		c.sort()
	}
}
