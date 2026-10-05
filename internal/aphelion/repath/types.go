package repath

import (
	"context"
	"slices"
	"strings"
)

// TypeSource is the read-only view of a loaded environment that repath needs.
// Value reports an inherited variable; ok is false when no type in the chain
// declares it.
type TypeSource interface {
	Paths() []string
	Value(path, name string) (string, bool)
}

// Index answers structural and metadata queries over one TypeSource. It is
// immutable after construction and safe for concurrent readers.
type Index struct {
	source   TypeSource
	paths    []string
	exists   map[string]struct{}
	children map[string][]string
	byLeaf   map[string][]string
	bySuffix map[string][]string // last two and three segments
	byToken  map[string][]string
	byIcon   map[string][]string // icon + "\x00" + icon_state
	byName   map[string][]string
}

func NewIndex(ctx context.Context, source TypeSource) (*Index, error) {
	paths := slices.Clone(source.Paths())
	slices.Sort(paths)
	paths = slices.Compact(paths)
	index := &Index{
		source:   source,
		paths:    paths,
		exists:   make(map[string]struct{}, len(paths)),
		children: make(map[string][]string),
		byLeaf:   make(map[string][]string),
		bySuffix: make(map[string][]string),
		byToken:  make(map[string][]string),
		byIcon:   make(map[string][]string),
		byName:   make(map[string][]string),
	}
	for n, path := range paths {
		if n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if Base(path) == "" {
			continue
		}
		index.exists[path] = struct{}{}
		if parent := Parent(path); parent != "" {
			index.children[parent] = append(index.children[parent], path)
		}
		parts := segments(path)
		index.byLeaf[parts[len(parts)-1]] = append(index.byLeaf[parts[len(parts)-1]], path)
		for _, length := range []int{2, 3} {
			if len(parts) > length {
				key := strings.Join(parts[len(parts)-length:], "/")
				index.bySuffix[key] = append(index.bySuffix[key], path)
			}
		}
		for _, token := range tokens(parts[len(parts)-1]) {
			index.byToken[token] = append(index.byToken[token], path)
		}
		if key, ok := index.iconKey(path); ok {
			index.byIcon[key] = append(index.byIcon[key], path)
		}
		if name, ok := index.text(path, "name"); ok && name != "" {
			index.byName[strings.ToLower(name)] = append(index.byName[strings.ToLower(name)], path)
		}
	}
	return index, nil
}

func (x *Index) Exists(path string) bool {
	if x == nil {
		return false
	}
	_, ok := x.exists[path]
	return ok
}

func (x *Index) Value(path, name string) (string, bool) {
	if !x.Exists(path) {
		return "", false
	}
	return x.source.Value(path, name)
}

func (x *Index) Declares(path, name string) bool {
	_, ok := x.Value(path, name)
	return ok
}

func (x *Index) Children(path string) []string { return x.children[path] }
func (x *Index) Paths() []string               { return x.paths }
func (x *Index) Len() int                      { return len(x.paths) }

// WithPrefix returns up to limit known paths beginning with prefix, in order.
func (x *Index) WithPrefix(prefix string, limit int) []string {
	if x == nil {
		return nil
	}
	start, _ := slices.BinarySearch(x.paths, prefix)
	var result []string
	for n := start; n < len(x.paths) && len(result) < limit && strings.HasPrefix(x.paths[n], prefix); n++ {
		result = append(result, x.paths[n])
	}
	return result
}

// NearestAncestor returns the closest known proper ancestor of path.
func (x *Index) NearestAncestor(path string) string {
	for parent := Parent(path); parent != ""; parent = Parent(parent) {
		if x.Exists(parent) {
			return parent
		}
	}
	return ""
}

// text returns a variable as unquoted text when it is a string literal.
func (x *Index) text(path, name string) (string, bool) {
	value, ok := x.source.Value(path, name)
	if !ok {
		return "", false
	}
	return unquote(value), true
}

func (x *Index) iconKey(path string) (string, bool) {
	icon, ok := x.source.Value(path, "icon")
	if !ok || icon == "" || icon == "null" {
		return "", false
	}
	state, _ := x.source.Value(path, "icon_state")
	return unquote(icon) + "\x00" + unquote(state), true
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// tokens splits an identifier on underscores and digit boundaries.
func tokens(leaf string) []string {
	var result []string
	for _, part := range strings.FieldsFunc(strings.ToLower(leaf), func(r rune) bool {
		return r == '_' || r == '-' || r >= '0' && r <= '9'
	}) {
		if len(part) > 1 && !slices.Contains(result, part) {
			result = append(result, part)
		}
	}
	return result
}

// MapSource is an in-memory TypeSource. Vars hold each type's own declared or
// overridden variables; inheritance follows path parents.
type MapSource map[string]map[string]string

func (m MapSource) Paths() []string {
	paths := make([]string, 0, len(m))
	for path := range m {
		paths = append(paths, path)
	}
	return paths
}

func (m MapSource) Value(path, name string) (string, bool) {
	for ; path != ""; path = Parent(path) {
		if vars, ok := m[path]; ok {
			if value, ok := vars[name]; ok {
				return value, true
			}
		}
	}
	return "", false
}
