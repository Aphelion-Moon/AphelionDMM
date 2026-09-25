// Package envsnapshot restores the parser's complete data-only export through
// the existing environment constructor. Cached fingerprints are never accepted.
package envsnapshot

import (
	"fmt"
	"strings"

	"sdmm/third_party/sdmmparser"
)

func ValidateTree(root *sdmmparser.ObjectTreeType) error {
	if root == nil || root.Path != "" {
		return fmt.Errorf("invalid namespace root")
	}
	parents := make(map[string]string)
	variables := 0
	var visit func(*sdmmparser.ObjectTreeType, string, int) error
	visit = func(node *sdmmparser.ObjectTreeType, parent string, depth int) error {
		if depth > 512 || len(parents) >= 500000 {
			return fmt.Errorf("snapshot tree budget exceeded")
		}
		if _, duplicate := parents[node.Path]; duplicate {
			return fmt.Errorf("duplicate type path: %s", node.Path)
		}
		if depth > 0 && (!strings.HasPrefix(node.Path, parent+"/") || strings.Contains(node.Path[len(parent)+1:], "/") || len(node.Path) == len(parent)+1) {
			return fmt.Errorf("invalid namespace child: %s", node.Path)
		}
		parents[node.Path] = parent
		names := make(map[string]bool, len(node.Vars))
		variables += len(node.Vars)
		if variables > 5000000 {
			return fmt.Errorf("snapshot variable budget exceeded")
		}
		for _, variable := range node.Vars {
			if variable.Name == "" || names[variable.Name] {
				return fmt.Errorf("invalid or duplicate variable in %s", node.Path)
			}
			names[variable.Name] = true
			if variable.Name == "parent_type" {
				if variable.Value == "null" {
					parents[node.Path] = ""
				} else {
					parents[node.Path] = variable.Value
				}
			}
		}
		for i := range node.Children {
			if err := visit(&node.Children[i], node.Path, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, "", 0); err != nil {
		return err
	}
	state := make(map[string]uint8, len(parents))
	var link func(string, int) error
	link = func(path string, depth int) error {
		if path == "" || state[path] == 2 {
			return nil
		}
		if state[path] == 1 || depth > 512 {
			return fmt.Errorf("cyclic or excessive inheritance at %s", path)
		}
		parent, ok := parents[path]
		if !ok {
			return fmt.Errorf("missing inheritance target %s", path)
		}
		state[path] = 1
		if err := link(parent, depth+1); err != nil {
			return err
		}
		state[path] = 2
		return nil
	}
	for path := range parents {
		if err := link(path, 0); err != nil {
			return err
		}
	}
	return nil
}
