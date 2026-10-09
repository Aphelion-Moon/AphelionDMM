// Package repath proposes and applies type path migrations for map content
// whose paths are unknown to the loaded environment. It is pure and
// deterministic: environment access goes through TypeSource, and map changes
// are produced as ordinary model.TileChange values for the operation engine.
package repath

import "strings"

// Base returns the root type of a path ("/obj" for "/obj/item/x"), or "" for
// a path that does not start with a non-empty segment.
func Base(path string) string {
	if len(path) < 2 || path[0] != '/' {
		return ""
	}
	if end := strings.IndexByte(path[1:], '/'); end >= 0 {
		if end == 0 {
			return ""
		}
		return path[:end+1]
	}
	return path
}

func SameBase(a, b string) bool {
	base := Base(a)
	return base != "" && base == Base(b)
}

// Parent returns the path without its last segment, or "" at a root type.
func Parent(path string) string {
	slash := strings.LastIndexByte(path, '/')
	if slash <= 0 {
		return ""
	}
	return path[:slash]
}

func Leaf(path string) string {
	return path[strings.LastIndexByte(path, '/')+1:]
}

func segments(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// singular reports the composition channels that must keep exactly their
// count of instances on every tile.
func singular(path string) (area, turf bool) {
	switch Base(path) {
	case "/area":
		return true, false
	case "/turf":
		return false, true
	}
	return false, false
}
