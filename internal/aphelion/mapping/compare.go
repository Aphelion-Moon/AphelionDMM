package mapping

import (
	"maps"
	"strings"

	"sdmm/internal/util"
)

type ChannelDiff struct{ Turf, Area, Objects bool }
type Transform struct{ Offset util.Point }

func (t Transform) Apply(p util.Point) util.Point {
	return util.Point{X: p.X + t.Offset.X, Y: p.Y + t.Offset.Y, Z: p.Z + t.Offset.Z}
}
func (t Transform) Inverse(p util.Point) util.Point {
	return util.Point{X: p.X - t.Offset.X, Y: p.Y - t.Offset.Y, Z: p.Z - t.Offset.Z}
}
func channel(a Atom) string {
	if a.Path == "/turf" || strings.HasPrefix(a.Path, "/turf/") {
		return "turf"
	}
	if a.Path == "/area" || strings.HasPrefix(a.Path, "/area/") {
		return "area"
	}
	return "objects"
}
func atomsFor(atoms []Atom, c string) []Atom {
	var result []Atom
	for _, a := range atoms {
		if channel(a) == c {
			result = append(result, a)
		}
	}
	return result
}
func CompareCell(a, b []Atom) ChannelDiff {
	equal := func(a, b []Atom) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i].Path != b[i].Path || !maps.Equal(a[i].Vars, b[i].Vars) {
				return false
			}
		}
		return true
	}
	return ChannelDiff{Turf: !equal(atomsFor(a, "turf"), atomsFor(b, "turf")), Area: !equal(atomsFor(a, "area"), atomsFor(b, "area")), Objects: !equal(atomsFor(a, "objects"), atomsFor(b, "objects"))}
}
