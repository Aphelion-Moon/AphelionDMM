// Package helpers finds the mapping helpers that apply to an object.
//
// Mapping helpers (/obj/effect/mapping_helpers) act on an atom on their own
// tile at Initialize, but nothing declares which atom: the DM code looks it up,
// as in `locate(/obj/machinery/power/apc) in loc` or
// `for(var/obj/machinery/door/window/windoor in loc)`. The index reads the
// helpers' own DM source for those lookups. A helper applies to an object when
// it, or a helper type it inherits from, looks up a type the object is.
package helpers

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
	"strings"
)

const Root = "/obj/effect/mapping_helpers"

var (
	// A top-level proc definition owned by a helper type, e.g.
	// "/obj/effect/mapping_helpers/airlock/Initialize(mapload)".
	procHeader = regexp.MustCompile(`^(` + regexp.QuoteMeta(Root) + `[\w/]*?)(?:/proc|/verb)?/\w+\(`)
	// Lookups on the helper's own tile.
	ownTile   = `(?:loc|src\.loc|get_turf\(src\))`
	locateRe  = regexp.MustCompile(`locate\((/[\w/]+)\)\s+in\s+` + ownTile + `\b`)
	forLoopRe = regexp.MustCompile(`for\s*\(\s*var(/[\w/]+)/\w+\s+in\s+` + ownTile + `\s*\)`)
)

// Type is what the index needs to know about a helper type.
type Type struct {
	Path     string
	Name     string
	Desc     string
	Abstract bool
}

// Helper is one helper that applies to an object.
type Helper struct {
	Type
	// Family is the helper type whose code looks the target up; Relative is
	// Path below it, for grouping.
	Family   string
	Relative string
	Target   string
}

// Index maps helper families to the types they act on.
type Index struct {
	types   []Type
	targets map[string][]string // helper type path -> looked-up types
}

// Build indexes helper types from their DM sources. sources are the files
// that define them; read returns a file's contents.
func Build(types []Type, sources []string, read func(string) ([]byte, error)) *Index {
	x := &Index{types: types, targets: map[string][]string{}}
	seen := map[string]bool{}
	for _, file := range sources {
		if seen[file] {
			continue
		}
		seen[file] = true
		data, err := read(file)
		if err != nil {
			continue
		}
		x.scan(data)
	}
	sort.Slice(x.types, func(i, j int) bool { return x.types[i].Path < x.types[j].Path })
	return x
}

func (x *Index) scan(data []byte) {
	owner := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && line[0] != '\t' && line[0] != ' ' {
			owner = ""
			if m := procHeader.FindStringSubmatch(line); m != nil {
				owner = m[1]
			}
			continue
		}
		if owner == "" {
			continue
		}
		code := line
		if i := strings.Index(code, "//"); i >= 0 {
			code = code[:i]
		}
		for _, re := range []*regexp.Regexp{locateRe, forLoopRe} {
			for _, m := range re.FindAllStringSubmatch(code, -1) {
				x.add(owner, m[1])
			}
		}
	}
}

func (x *Index) add(owner, target string) {
	for _, t := range x.targets[owner] {
		if t == target {
			return
		}
	}
	x.targets[owner] = append(x.targets[owner], target)
}

func isType(path, root string) bool { return path == root || strings.HasPrefix(path, root+"/") }

// For lists the helpers that act on an object of path, nearest family first
// within each path order.
func (x *Index) For(path string) []Helper {
	if x == nil {
		return nil
	}
	var out []Helper
	for _, t := range x.types {
		if t.Abstract {
			continue
		}
		for family := t.Path; isType(family, Root); family = family[:strings.LastIndex(family, "/")] {
			target := ""
			for _, candidate := range x.targets[family] {
				if isType(path, candidate) {
					target = candidate
					break
				}
			}
			if target != "" {
				out = append(out, Helper{Type: t, Family: family, Relative: strings.TrimPrefix(strings.TrimPrefix(t.Path, family), "/"), Target: target})
				break
			}
			if len(x.targets[family]) != 0 {
				break // the nearest family with lookups decides
			}
		}
	}
	return out
}
